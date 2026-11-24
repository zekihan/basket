import { ApiError, downloadExport, getSnapshot, getSimilar, postLogout, postOps } from "./api.ts";
import { clearLocal, openBasketDB, readLocal, writeLocal } from "./db.ts";
import { applyOp, batchOps, emptySnapshot, filterAckedOps, ulid } from "./logic.ts";
import type { BasketDB } from "./db.ts";
import type { Item, Op, Snapshot } from "../types.ts";

export interface Toast {
  id: string;
  message: string;
}

export interface StoreState {
  snapshot: Snapshot | null;
  queue: Op[];
  online: boolean;
  signedOut: boolean;
  sending: boolean;
  toasts: Toast[];
  lastMutationAt: number | null;
  retryIn: number | null;
}

const BATCH = 200;

export class Store {
  private dbPromise: Promise<BasketDB> | null = null;
  private state: StoreState;
  private listeners = new Set<() => void>();
  private sending = false;
  private sendRequested = false;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private backoffMs = 1000;
  private lastToastId = 0;
  private seq = 0;

  constructor() {
    this.state = {
      snapshot: null,
      queue: [],
      online: typeof navigator === "undefined" ? true : navigator.onLine,
      signedOut: false,
      sending: false,
      toasts: [],
      lastMutationAt: null,
      retryIn: null,
    };
    if (typeof window !== "undefined") {
      window.addEventListener("online", () => this.handleOnline());
      window.addEventListener("offline", () => this.handleOffline());
      document.addEventListener("visibilitychange", () => {
        if (document.visibilityState === "visible") void this.sendQueue();
      });
    }
  }

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  getState = (): StoreState => this.state;

  private setState(patch: Partial<StoreState>): void {
    this.state = { ...this.state, ...patch };
    for (const listener of this.listeners) listener();
  }

  private db(): Promise<BasketDB> {
    this.dbPromise ??= openBasketDB();
    return this.dbPromise;
  }

  private async persist(): Promise<void> {
    try {
      const db = await this.db();
      await writeLocal(db, {
        snapshot: this.state.snapshot,
        queue: this.state.queue,
        signedOut: this.state.signedOut,
      });
    } catch {
      // IndexedDB unavailable; keep in-memory state.
    }
  }

  async loadFromDisk(): Promise<void> {
    try {
      const db = await this.db();
      const local = await readLocal(db);
      this.setState({
        snapshot: local.snapshot ?? this.state.snapshot,
        queue: local.queue,
        signedOut: local.signedOut,
      });
      this.seq = local.queue.length;
    } catch {
      // ignore
    }
  }

  async startup(): Promise<void> {
    await this.loadFromDisk();
    if (this.state.signedOut) return;
    if (!this.state.online) return;
    if (this.state.queue.length > 0) {
      void this.sendQueue();
      return;
    }
    await this.refreshFromServer();
  }

  async refreshFromServer(): Promise<void> {
    try {
      const snapshot = await getSnapshot();
      this.setState({ snapshot });
      await this.persist();
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        this.setState({ signedOut: true });
        await this.persist();
      }
    }
  }

  private handleOnline(): void {
    this.setState({ online: true });
    this.backoffMs = 1000;
    void this.sendQueue();
  }

  private handleOffline(): void {
    this.setState({ online: false });
  }

  pushToast(message: string): void {
    this.lastToastId += 1;
    const id = `t${this.lastToastId}`;
    this.setState({ toasts: [...this.state.toasts, { id, message }] });
    setTimeout(() => {
      this.setState({ toasts: this.state.toasts.filter(t => t.id !== id) });
    }, 4000);
  }

  dismissToast(id: string): void {
    this.setState({ toasts: this.state.toasts.filter(t => t.id !== id) });
  }

  async enqueue(op: Op): Promise<void> {
    const withId: Op = { ...op, op_id: op.op_id ?? ulid() } as Op;
    const base = this.state.snapshot ?? emptySnapshot();
    const next = applyOp(base, withId);
    this.seq += 1;
    this.setState({
      snapshot: next,
      queue: [...this.state.queue, withId],
      lastMutationAt: Date.now(),
    });
    await this.persist();
    void this.sendQueue();
  }

  async sendQueue(): Promise<void> {
    if (this.sending) {
      this.sendRequested = true;
      return;
    }
    if (this.state.signedOut || !this.state.online) return;
    if (this.state.queue.length === 0) return;
    this.sending = true;
    this.setState({ sending: true });
    try {
      while (this.state.queue.length > 0) {
        const batch = batchOps(this.state.queue, BATCH)[0];
        if (!batch || batch.length === 0) break;
        const response = await postOps(batch);
        const acked = response.results.map(r => r.op_id);
        const failed = response.results.filter(r => !r.ok && r.error);
        for (const result of failed) this.pushToast(result.error?.message ?? "Could not save change");
        const remaining = filterAckedOps(this.state.queue, acked);
        let rebased = response.snapshot;
        for (const op of remaining) rebased = applyOp(rebased, op);
        this.setState({ snapshot: rebased, queue: remaining, retryIn: null });
        await this.persist();
        if (!response.snapshot.can_undo && remaining.length === 0) {
          // keep can_undo from server
        }
      }
      this.backoffMs = 1000;
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        this.setState({ signedOut: true, retryIn: null });
        await this.persist();
        return;
      }
      const delay = this.backoffMs;
      this.backoffMs = Math.min(this.backoffMs * 2, 30000);
      this.setState({ retryIn: delay });
      if (this.retryTimer) clearTimeout(this.retryTimer);
      this.retryTimer = setTimeout(() => {
        this.retryTimer = null;
        void this.sendQueue();
      }, delay);
    } finally {
      this.sending = false;
      this.setState({ sending: false });
      if (this.sendRequested) {
        this.sendRequested = false;
        void this.sendQueue();
      }
    }
  }

  async signOut(): Promise<void> {
    try {
      await postLogout();
    } catch {
      // ignore
    }
    try {
      const db = await this.db();
      await clearLocal(db);
    } catch {
      // ignore
    }
    this.setState({ signedOut: true, snapshot: null, queue: [], toasts: [] });
  }

  async exportJSON(): Promise<void> {
    await downloadExport();
  }

  async fetchSimilar(title: string): Promise<Item[]> {
    if (!this.state.online) return [];
    try {
      return await getSimilar(title);
    } catch {
      return [];
    }
  }
}

let shared: Store | null = null;

export function getStore(): Store {
  shared ??= new Store();
  return shared;
}
