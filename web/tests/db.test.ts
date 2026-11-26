import assert from "node:assert/strict";
import { describe, test } from "node:test";
import "fake-indexeddb/auto";
import { openDB } from "idb";
import { clearLocal, openBasketDB, readLocal, writeLocal } from "../src/data/db.ts";
import { applyOp } from "../src/data/logic.ts";
import type { Op, Snapshot } from "../src/types.ts";

function snap(): Snapshot {
  return { today: "2026-11-24", time_zone: "Europe/Istanbul", items: [], purchases: [], can_undo: false };
}

describe("basket db", () => {
  test("upgrades a projected v1 snapshot without replaying its queue", async () => {
    const legacy = await openDB("basket", 1, {
      upgrade(db) {
        db.createObjectStore("meta");
        db.createObjectStore("snapshot");
        db.createObjectStore("queue", { keyPath: "op.op_id" });
      },
    });
    const add: Op = { op_id: "add", type: "add_item", id: "item", fields: { title: "coffee" } };
    const bought: Op = { op_id: "buy", type: "mark_bought", id: "item", purchase_id: "purchase" };
    const cached = applyOp(applyOp(snap(), add), bought);
    await legacy.put("snapshot", cached, "state");
    await legacy.put("queue", { op: bought, seq: 1 });
    legacy.close();

    const db = await openBasketDB();
    const local = await readLocal(db);
    assert.deepEqual(local.snapshot, cached);
    assert.deepEqual(local.queue, [bought]);
    const appended = await writeLocal(db, { op: { op_id: "edit", type: "update_item", id: "item", fields: { notes: "dark roast" } } });
    assert.deepEqual(appended.queue.map(op => op.op_id), ["buy", "edit"]);
    assert.equal(appended.snapshot?.purchases.length, 1);
    assert.equal(appended.snapshot?.items[0]?.notes, "dark roast");
    db.close();
  });

  test("round-trips snapshot and ordered queue", async () => {
    const db = await openBasketDB();
    await clearLocal(db);
    const ops = [
      { op_id: "03", type: "undo" as const },
      { op_id: "02", type: "undo" as const },
      { op_id: "01", type: "undo" as const },
    ];
    await writeLocal(db, { snapshot: snap() });
    for (const op of ops) await writeLocal(db, { op });
    const read = await readLocal(db);
    assert.deepEqual(read.queue.map(op => op.op_id), ["03", "02", "01"]);
    assert.equal(read.snapshot?.today, "2026-11-24");
    await writeLocal(db, { snapshot: snap(), acked: ["03", "02"] });
    const read2 = await readLocal(db);
    assert.deepEqual(read2.queue.map(op => op.op_id), ["01"]);
    await clearLocal(db);
    const read3 = await readLocal(db);
    assert.equal(read3.snapshot, null);
    assert.deepEqual(read3.queue, []);
    db.close();
  });

  test("sign-out clears the shared queue and discards stale tab writes", async () => {
    const db = await openBasketDB();
    const other = await openBasketDB();
    const { generation } = await writeLocal(other, { op: { op_id: "pending", type: "undo" } });
    await clearLocal(db);
    await writeLocal(other, { op: { op_id: "stale", type: "undo" } }, generation);
    await writeLocal(other, { snapshot: snap(), acked: ["pending"] }, generation);
    assert.deepEqual(await readLocal(db), { snapshot: null, queue: [], generation: generation + 1, operationRevision: 0 });
    db.close();
    other.close();
  });
});
