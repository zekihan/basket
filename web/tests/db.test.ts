import assert from "node:assert/strict";
import { describe, test } from "node:test";
import "fake-indexeddb/auto";
import { clearLocal, openBasketDB, readLocal, writeLocal } from "../src/data/db.ts";
import type { Snapshot } from "../src/types.ts";

function snap(): Snapshot {
  return { today: "2026-11-24", time_zone: "Europe/Istanbul", items: [], purchases: [], can_undo: false };
}

describe("basket db", () => {
  test("round-trips snapshot and ordered queue", async () => {
    const db = await openBasketDB();
    await clearLocal(db);
    const ops = [
      { op_id: "01", type: "undo" as const },
      { op_id: "02", type: "undo" as const },
      { op_id: "03", type: "undo" as const },
    ];
    await writeLocal(db, { snapshot: snap(), queue: ops });
    const read = await readLocal(db);
    assert.deepEqual(read.queue.map(op => op.op_id), ["01", "02", "03"]);
    assert.equal(read.snapshot?.today, "2026-11-24");
    await writeLocal(db, { snapshot: snap(), queue: [ops[2]] });
    const read2 = await readLocal(db);
    assert.deepEqual(read2.queue.map(op => op.op_id), ["03"]);
    await clearLocal(db);
    const read3 = await readLocal(db);
    assert.equal(read3.snapshot, null);
    assert.deepEqual(read3.queue, []);
  });
});
