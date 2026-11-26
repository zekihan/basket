import { ApiError, downloadExport, getSnapshot, getSimilar, postLogout, postOps } from "./api.ts";
import { clearLocal, openBasketDB, readLocal, writeLocal } from "./db.ts";
import { batchOps, emptySnapshot, filterAckedOps, rebaseSnapshot, ulid } from "./logic.ts";
import type { BasketDB, LocalChange, LocalState } from "./db.ts";
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
  private sendRequested: boolean | null = null;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private backoffMs = 1000;
  private lastToastId = 0;
  private generation: number | undefined;
  private volatileQueue: Op[] = [];

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

  private async updateLocal(change?: LocalChange): Promise<LocalState | undefined> {
    const generation = this.generation;
    const local = await this.db()
      .then(db => change ? writeLocal(db, change, generation) : readLocal(db))
      .catch(() => null);
    if (this.state.signedOut) return;
    if (!local) {
      if (!change) return;
      // IndexedDB can be unavailable. Keep local intent in memory for this tab.
      if ("op" in change) {
        const op = change.op;
        if (this.state.queue.some(queued => queued.op_id === op.op_id)) return;
        this.volatileQueue.push(op);
        this.setState({
          snapshot: rebaseSnapshot(this.state.snapshot ?? emptySnapshot(), [op]),
          queue: [...this.state.queue, op],
        });
      } else {
        const queue = filterAckedOps(this.state.queue, change.acked ?? []);
        this.setState({ snapshot: rebaseSnapshot(change.snapshot, queue), queue });
      }
      return;
    }
    if (generation !== undefined && generation !== local.generation) {
      this.volatileQueue = [];
      this.setState({ snapshot: null, queue: [], signedOut: true, toasts: [], retryIn: null });
      return;
    }
    this.generation = local.generation;
    const queuedIds = new Set(local.queue.map(op => op.op_id));
    this.volatileQueue = this.volatileQueue.filter(op => !queuedIds.has(op.op_id));
    const snapshot = this.volatileQueue.length > 0
      ? rebaseSnapshot(local.snapshot ?? emptySnapshot(), this.volatileQueue)
      : local.snapshot;
    this.setState({ snapshot, queue: [...local.queue, ...this.volatileQueue] });
    return local;
  }

  async startup(): Promise<void> {
    await this.updateLocal();
    if (!this.state.online) return;
    if (this.state.queue.length > 0) {
      void this.sendQueue();
      return;
    }
    await this.sendQueue(true);
  }

  private handleOnline(): void {
    this.setState({ online: true });
    this.backoffMs = 1000;
    void this.sendQueue(true);
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
    if (this.state.signedOut) return;
    const withId: Op = { ...op, op_id: op.op_id ?? ulid() } as Op;
    await this.updateLocal({ op: withId });
    if (this.state.signedOut) return;
    this.setState({ lastMutationAt: Date.now() });
    void this.sendQueue();
  }

  private async syncQueue(refresh: boolean): Promise<void> {
    if (this.state.signedOut || !this.state.online) return;
    const local = await this.updateLocal();
    if (this.state.signedOut || !this.state.online) return;
    if (this.state.queue.length === 0 && refresh) {
      const snapshot = await getSnapshot();
      if (this.state.signedOut) return;
      await this.updateLocal({ snapshot, expectedOperationRevision: local?.operationRevision });
    }
    while (!this.state.signedOut && this.state.online && this.state.queue.length > 0) {
      const [batch] = batchOps(this.state.queue, BATCH);
      const response = await postOps(batch);
      if (this.state.signedOut) return;
      const acked = response.results.map(result => result.op_id);
      if (filterAckedOps(batch, acked).length === batch.length) throw new Error("No operations acknowledged");
      const hasVolatileAck = this.volatileQueue.some(op => acked.includes(op.op_id));
      this.volatileQueue = filterAckedOps(this.volatileQueue, acked);
      await this.updateLocal({ snapshot: response.snapshot, acked, hasVolatileAck });
      if (this.state.signedOut) return;
      for (const result of response.results) {
        if (!result.ok && result.error) this.pushToast(result.error.message);
      }
    }
  }

  async sendQueue(refresh = false): Promise<void> {
    if (this.sending) {
      this.sendRequested = refresh || this.sendRequested === true;
      return;
    }
    if (this.state.signedOut || !this.state.online) return;
    this.sending = true;
    this.setState({ sending: true });
    try {
      const locks = typeof navigator === "undefined" ? undefined : navigator.locks;
      // Read the shared queue after acquiring the lock, so tabs send in order
      // and a slower server response cannot overwrite a newer snapshot.
      if (locks) {
        await locks.request("basket-sync", () => this.syncQueue(refresh));
      } else {
        await this.syncQueue(refresh);
      }
      this.backoffMs = 1000;
      if (this.retryTimer) clearTimeout(this.retryTimer);
      this.retryTimer = null;
      this.setState({ retryIn: null });
    } catch (error) {
      if (this.state.signedOut) return;
      if (error instanceof ApiError && error.status === 401) {
        this.setState({ signedOut: true, retryIn: null });
        return;
      }
      const delay = this.backoffMs;
      this.backoffMs = Math.min(this.backoffMs * 2, 30000);
      this.setState({ retryIn: delay });
      if (this.retryTimer) clearTimeout(this.retryTimer);
      this.retryTimer = setTimeout(() => {
        this.retryTimer = null;
        void this.sendQueue(refresh);
      }, delay);
    } finally {
      this.sending = false;
      this.setState({ sending: false });
      if (this.sendRequested !== null) {
        const requestedRefresh = this.sendRequested;
        this.sendRequested = null;
        void this.sendQueue(requestedRefresh);
      }
    }
  }

  async signOut(): Promise<void> {
    if (this.retryTimer) clearTimeout(this.retryTimer);
    this.retryTimer = null;
    this.volatileQueue = [];
    this.setState({ signedOut: true, snapshot: null, queue: [], toasts: [], retryIn: null });
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
