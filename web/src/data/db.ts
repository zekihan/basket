import { openDB } from "idb";
import type { DBSchema, IDBPDatabase } from "idb";
import type { Op, Snapshot } from "../types.ts";

export interface BasketDBSchema extends DBSchema {
  meta: { key: string; value: { signedOut?: boolean } };
  snapshot: { key: string; value: Snapshot };
  queue: { key: string; value: { op: Op; seq: number } };
}

export type BasketDB = IDBPDatabase<BasketDBSchema>;

const NAME = "basket";
const KEY = "state";

export function openBasketDB(): Promise<BasketDB> {
  return openDB<BasketDBSchema>(NAME, 1, {
    upgrade(db) {
      db.createObjectStore("meta");
      db.createObjectStore("snapshot");
      db.createObjectStore("queue", { keyPath: "op.op_id" });
    },
    blocking(_current, _next, event) {
      (event.target as IDBDatabase).close();
    },
  });
}

export async function readLocal(db: BasketDB): Promise<{ snapshot: Snapshot | null; queue: Op[]; signedOut: boolean }> {
  const tx = db.transaction(["meta", "snapshot", "queue"] as const, "readonly");
  const [meta, snapshot, entries] = await Promise.all([
    tx.objectStore("meta").get(KEY),
    tx.objectStore("snapshot").get(KEY),
    tx.objectStore("queue").getAll(),
  ]);
  await tx.done;
  const sorted = [...(entries ?? [])].sort((a, b) => a.seq - b.seq);
  return {
    snapshot: (snapshot as Snapshot | undefined) ?? null,
    queue: sorted.map(entry => entry.op),
    signedOut: !!(meta as { signedOut?: boolean } | undefined)?.signedOut,
  };
}

export async function writeLocal(db: BasketDB, state: { snapshot: Snapshot | null; queue: Op[]; signedOut?: boolean }): Promise<void> {
  const tx = db.transaction(["meta", "snapshot", "queue"] as const, "readwrite");
  const queueStore = tx.objectStore("queue");
  const existing = await queueStore.getAll();
  const keep = new Set(state.queue.map(op => op.op_id));
  const seqById = new Map(existing.map(entry => [entry.op.op_id, entry.seq]));
  for (const entry of existing) {
    if (!keep.has(entry.op.op_id)) await queueStore.delete(entry.op.op_id);
  }
  let maxSeq = Math.max(0, ...existing.map(entry => entry.seq));
  for (const op of state.queue) {
    const seq = seqById.get(op.op_id) ?? ++maxSeq;
    await queueStore.put({ op, seq });
  }
  if (state.snapshot) {
    await tx.objectStore("snapshot").put(state.snapshot, KEY);
  } else {
    await tx.objectStore("snapshot").delete(KEY);
  }
  if (state.signedOut !== undefined) {
    await tx.objectStore("meta").put({ signedOut: state.signedOut }, KEY);
  }
  await tx.done;
}

export async function clearLocal(db: BasketDB): Promise<void> {
  const tx = db.transaction(["meta", "snapshot", "queue"] as const, "readwrite");
  await Promise.all([
    tx.objectStore("meta").clear(),
    tx.objectStore("snapshot").clear(),
    tx.objectStore("queue").clear(),
  ]);
  await tx.done;
}
