import { openDB } from "idb";
import type { DBSchema, IDBPDatabase } from "idb";
import type { Op, Snapshot } from "../types.ts";
import { emptySnapshot, rebaseSnapshot } from "./logic.ts";

export interface BasketDBSchema extends DBSchema {
  meta: { key: string; value: { generation?: number; operationRevision?: number } };
  snapshot: { key: string; value: Snapshot };
  queue: { key: string; value: { op: Op; seq: number } };
}

export type BasketDB = IDBPDatabase<BasketDBSchema>;

export interface LocalState {
  snapshot: Snapshot | null;
  queue: Op[];
  generation: number;
  operationRevision: number;
}

export type LocalChange =
  | { op: Op }
  | { snapshot: Snapshot; acked?: string[]; hasVolatileAck?: boolean; expectedOperationRevision?: number };

const NAME = "basket";
const KEY = "state";
const STORES = ["meta", "snapshot", "queue"] as const;

export function openBasketDB(): Promise<BasketDB> {
  // Version 1 tabs replace the entire queue. Close those connections on upgrade.
  return openDB<BasketDBSchema>(NAME, 2, {
    upgrade(db, oldVersion) {
      if (oldVersion === 0) {
        db.createObjectStore("meta");
        db.createObjectStore("snapshot");
        db.createObjectStore("queue", { keyPath: "op.op_id" });
      }
    },
    blocking(_current, _next, event) {
      (event.target as IDBDatabase).close();
    },
  });
}

export function readLocal(db: BasketDB): Promise<LocalState> {
  return transactLocal(db);
}

export function writeLocal(db: BasketDB, change: LocalChange, expectedGeneration?: number): Promise<LocalState> {
  return transactLocal(db, change, expectedGeneration);
}

async function transactLocal(db: BasketDB, change?: LocalChange, expectedGeneration?: number): Promise<LocalState> {
  const write = change ? db.transaction(STORES, "readwrite") : null;
  const tx = write ?? db.transaction(STORES, "readonly");
  const [meta, cached, entries] = await Promise.all([
    tx.objectStore("meta").get(KEY),
    tx.objectStore("snapshot").get(KEY),
    tx.objectStore("queue").getAll(),
  ]);
  entries.sort((a, b) => a.seq - b.seq);
  const generation = meta?.generation ?? 0;
  let operationRevision = meta?.operationRevision ?? 0;
  let snapshot = cached ?? null;
  let queue = entries.map(entry => entry.op);

  // The cached snapshot already includes the queue. Append to that projection,
  // or replace it with a server snapshot projected through the remaining queue.
  if (write && change && (expectedGeneration === undefined || expectedGeneration === generation)) {
    const queueStore = write.objectStore("queue");
    const snapshotStore = write.objectStore("snapshot");
    if ("op" in change) {
      const op = change.op;
      if (!queue.some(queued => queued.op_id === op.op_id)) {
        snapshot = rebaseSnapshot(snapshot ?? emptySnapshot(), [op]);
        await Promise.all([
          queueStore.add({ op, seq: (entries.at(-1)?.seq ?? 0) + 1 }),
          snapshotStore.put(snapshot, KEY),
        ]);
        queue.push(op);
      }
    } else {
      const acked = new Set(change.acked);
      const acknowledged = queue.filter(op => acked.has(op.op_id));
      // Another sender can have committed the whole batch in browsers without Web Locks.
      // A refresh must also yield to operation responses committed while it was in flight.
      const acceptSnapshot = change.acked
        ? acknowledged.length > 0 || change.hasVolatileAck
        : (change.expectedOperationRevision ?? operationRevision) === operationRevision;
      if (acceptSnapshot) {
        queue = queue.filter(op => !acked.has(op.op_id));
        snapshot = rebaseSnapshot(change.snapshot, queue);
        if (change.acked) {
          operationRevision += 1;
          await write.objectStore("meta").put({ generation, operationRevision }, KEY);
        }
        await Promise.all([
          ...acknowledged.map(op => queueStore.delete(op.op_id)),
          snapshotStore.put(snapshot, KEY),
        ]);
      }
    }
  }
  await tx.done;
  return { snapshot, queue, generation, operationRevision };
}

export async function clearLocal(db: BasketDB): Promise<void> {
  const tx = db.transaction(STORES, "readwrite");
  const meta = tx.objectStore("meta");
  const previous = await meta.get(KEY);
  await Promise.all([
    // Reject writes from tabs that still hold state from before sign-out.
    meta.put({ generation: (previous?.generation ?? 0) + 1 }, KEY),
    tx.objectStore("snapshot").clear(),
    tx.objectStore("queue").clear(),
  ]);
  await tx.done;
}
