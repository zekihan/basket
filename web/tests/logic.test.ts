import assert from "node:assert/strict";
import { describe, test } from "node:test";
import {
  applyOp,
  archivedItems,
  batchOps,
  boughtItems,
  dueAgainItems,
  filterAckedOps,
  formatTRYMinor,
  isUlid,
  linkify,
  openItems,
  parseTRYMinor,
  rebaseSnapshot,
  splitQuickAdd,
  ulid,
} from "../src/data/logic.ts";
import type { Item, Op, Snapshot } from "../src/types.ts";

function item(partial: Partial<Item> & { id: string }): Item {
  return {
    list: "shopping",
    title: "item",
    quantity: 1,
    notes: "",
    target_price: null,
    target_price_minor: null,
    recurring_days: null,
    status: "open",
    created_at: "2026-11-20T10:00:00Z",
    updated_at: "2026-11-20T10:00:00Z",
    version: 1,
    last_purchase_date: null,
    due_date: null,
    due: false,
    ...partial,
  };
}

function snapshot(items: Item[]): Snapshot {
  return { today: "2026-11-24", time_zone: "Europe/Istanbul", items, purchases: [], can_undo: false };
}

describe("splitQuickAdd", () => {
  test("splits on the first comma", () => {
    assert.deepEqual(splitQuickAdd("charger cable, usb-c 60W"), { title: "charger cable", notes: "usb-c 60W" });
  });
  test("trims both sides", () => {
    assert.deepEqual(splitQuickAdd("  shampoo ,  honey, 2in1  "), { title: "shampoo", notes: "honey, 2in1" });
  });
  test("no comma gives empty notes", () => {
    assert.deepEqual(splitQuickAdd("  shampoo  "), { title: "shampoo", notes: "" });
  });
  test("empty or whitespace gives null", () => {
    assert.equal(splitQuickAdd(""), null);
    assert.equal(splitQuickAdd("   "), null);
  });
  test("empty title before comma gives null", () => {
    assert.equal(splitQuickAdd(", notes only"), null);
  });
  test("comma at end gives empty notes", () => {
    assert.deepEqual(splitQuickAdd("shampoo,"), { title: "shampoo", notes: "" });
  });
});

describe("linkify", () => {
  test("plain text stays one segment", () => {
    assert.deepEqual(linkify("hello world"), [{ text: "hello world" }]);
  });
  test("empty string gives empty", () => {
    assert.deepEqual(linkify(""), []);
  });
  test("url becomes a link segment", () => {
    const segs = linkify("see https://example.com/a and more");
    assert.equal(segs.length, 3);
    assert.deepEqual(segs[1], { text: "https://example.com/a", href: "https://example.com/a" });
  });
  test("trailing punctuation is not part of the url", () => {
    const segs = linkify("go to https://example.com/a.");
    assert.deepEqual(segs, [
      { text: "go to " },
      { text: "https://example.com/a", href: "https://example.com/a" },
      { text: "." },
    ]);
  });
});

describe("ulid", () => {
  test("is 26 chars crockford", () => {
    for (let i = 0; i < 20; i++) {
      const value = ulid();
      assert.equal(value.length, 26);
      assert.ok(isUlid(value));
    }
  });
  test("monotonic within the same millisecond", () => {
    const a = ulid(1700000000000);
    const b = ulid(1700000000000);
    const c = ulid(1700000000000);
    assert.ok(a < b);
    assert.ok(b < c);
  });
  test("later timestamp sorts later", () => {
    const a = ulid(1700000000000);
    const b = ulid(1700000000001);
    assert.ok(a < b);
  });
  test("rejects bad ids", () => {
    assert.equal(isUlid("short"), false);
    assert.equal(isUlid("IIIIIIIIIIIIIIIIIIIIIIIIII"), false);
  });
});

describe("parseTRYMinor", () => {
  test("parses family-tab format", () => {
    assert.equal(parseTRYMinor("189,90"), 18990);
    assert.equal(parseTRYMinor("1.299,90"), 129990);
    assert.equal(parseTRYMinor("1299.90"), 129990);
    assert.equal(parseTRYMinor("1299"), 129900);
  });
  test("round-trips through format", () => {
    assert.equal(formatTRYMinor(129990), "1.299,90");
    assert.equal(formatTRYMinor(null), null);
  });
  test("rejects junk", () => {
    assert.equal(parseTRYMinor(""), null);
    assert.equal(parseTRYMinor("abc"), null);
  });
});

describe("applyOp", () => {
  test("add_item adds newest first", () => {
    const s = snapshot([]);
    const next = applyOp(s, { op_id: "01", type: "add_item", id: "AAA", fields: { title: "shampoo" } });
    assert.equal(next.items.length, 1);
    assert.equal(next.items[0].title, "shampoo");
    assert.equal(next.items[0].list, "shopping");
  });
  test("add_item with duplicate id is a no-op", () => {
    const s = snapshot([item({ id: "AAA", title: "old" })]);
    const next = applyOp(s, { op_id: "01", type: "add_item", id: "AAA", fields: { title: "new" } });
    assert.equal(next.items.length, 1);
    assert.equal(next.items[0].title, "old");
  });
  test("update_item writes only present fields", () => {
    const s = snapshot([item({ id: "AAA", title: "a", quantity: 2, notes: "keep" })]);
    const next = applyOp(s, { op_id: "01", type: "update_item", id: "AAA", fields: { quantity: 5 } });
    assert.equal(next.items[0].quantity, 5);
    assert.equal(next.items[0].notes, "keep");
  });
  test("moving to wishlist clears recurring_days", () => {
    const s = snapshot([item({ id: "AAA", recurring_days: 30 })]);
    const next = applyOp(s, { op_id: "01", type: "update_item", id: "AAA", fields: { list: "wishlist" } });
    assert.equal(next.items[0].list, "wishlist");
    assert.equal(next.items[0].recurring_days, null);
  });
  test("remove_item drops item and its purchases", () => {
    const s = snapshot([item({ id: "AAA" }), item({ id: "BBB" })]);
    s.purchases.push({ id: "P1", item_id: "AAA", date: "2026-11-24", price: null, price_minor: null, quantity: 1, store: null, created_at: "2026-11-24T10:00:00Z" });
    const next = applyOp(s, { op_id: "01", type: "remove_item", id: "AAA" });
    assert.equal(next.items.length, 1);
    assert.equal(next.purchases.length, 0);
  });
  test("mark_bought adds purchase and sets status", () => {
    const s = snapshot([item({ id: "AAA", quantity: 2 })]);
    const next = applyOp(s, { op_id: "01", type: "mark_bought", id: "AAA", purchase_id: "P1" });
    assert.equal(next.items[0].status, "bought");
    assert.equal(next.purchases.length, 1);
    assert.equal(next.purchases[0].quantity, 2);
    assert.equal(next.purchases[0].date, "2026-11-24");
  });
  test("add_purchase_details fills price and store", () => {
    const s = snapshot([item({ id: "AAA" })]);
    s.purchases.push({ id: "P1", item_id: "AAA", date: "2026-11-24", price: null, price_minor: null, quantity: 1, store: null, created_at: "2026-11-24T10:00:00Z" });
    const next = applyOp(s, { op_id: "01", type: "add_purchase_details", purchase_id: "P1", price: "189,90", store: "Trendyol" });
    assert.equal(next.purchases[0].price, "189,90");
    assert.equal(next.purchases[0].price_minor, 18990);
    assert.equal(next.purchases[0].store, "Trendyol");
  });
  test("reopen_item sets open", () => {
    const s = snapshot([item({ id: "AAA", status: "bought" })]);
    const next = applyOp(s, { op_id: "01", type: "reopen_item", id: "AAA" });
    assert.equal(next.items[0].status, "open");
  });
  test("archive_item sets archived", () => {
    const s = snapshot([item({ id: "AAA" })]);
    const next = applyOp(s, { op_id: "01", type: "archive_item", id: "AAA" });
    assert.equal(next.items[0].status, "archived");
  });
  test("delete_purchase removes the purchase", () => {
    const s = snapshot([item({ id: "AAA" })]);
    s.purchases.push({ id: "P1", item_id: "AAA", date: "2026-11-24", price: null, price_minor: null, quantity: 1, store: null, created_at: "2026-11-24T10:00:00Z" });
    const next = applyOp(s, { op_id: "01", type: "delete_purchase", purchase_id: "P1" });
    assert.equal(next.purchases.length, 0);
  });
  test("set_time_zone updates zone", () => {
    const s = snapshot([]);
    const next = applyOp(s, { op_id: "01", type: "set_time_zone", time_zone: "UTC" });
    assert.equal(next.time_zone, "UTC");
  });
  test("undo leaves the snapshot alone", () => {
    const s = snapshot([item({ id: "AAA" })]);
    const next = applyOp(s, { op_id: "01", type: "undo" });
    assert.equal(next.items.length, 1);
  });
});

describe("queue bookkeeping", () => {
  test("batch slices at 200", () => {
    const ops = Array.from({ length: 450 }, (_, i) => ({ op_id: String(i), type: "undo" as const }));
    const batches = batchOps(ops);
    assert.equal(batches.length, 3);
    assert.equal(batches[0].length, 200);
    assert.equal(batches[2].length, 50);
  });
  test("filterAckedOps removes acked ids, keeps order", () => {
    const ops = [
      { op_id: "a", type: "undo" as const },
      { op_id: "b", type: "undo" as const },
      { op_id: "c", type: "undo" as const },
    ];
    assert.deepEqual(filterAckedOps(ops, ["b"]).map(op => op.op_id), ["a", "c"]);
  });
  test("rebase reapplies remaining ops on server snapshot", () => {
    const server = snapshot([]);
    const remaining = [
      { op_id: "01", type: "add_item" as const, id: "AAA", fields: { title: "shampoo" } },
      { op_id: "02", type: "update_item" as const, id: "AAA", fields: { quantity: 3 } },
    ];
    const rebased = rebaseSnapshot(server, remaining);
    assert.equal(rebased.items.length, 1);
    assert.equal(rebased.items[0].quantity, 3);
  });
});

const recurringMilk = item({
  id: "milk",
  title: "Milk",
  status: "bought",
  recurring_days: 7,
  last_purchase_date: "2026-11-01",
  due_date: "2026-11-08",
  due: true,
});

for (const [mode, apply] of [
  ["applyOp", (s: Snapshot, ops: Op[]) => ops.reduce(applyOp, s)],
  ["rebaseSnapshot", rebaseSnapshot],
] as const) {
  describe(`${mode} recurrence`, () => {
    function boughtMilk(...purchases: Array<[string, string]>): Snapshot {
      const s = snapshot([recurringMilk]);
      s.purchases = purchases.map(([id, date]) => ({
        id, item_id: recurringMilk.id, date, price: null, price_minor: null,
        quantity: 1, store: null, created_at: "2026-11-24T10:00:00Z",
      }));
      return s;
    }

    test("reopen and archive retain the due date and remove the item from Due again", () => {
      const s = boughtMilk(["p1", "2026-11-01"]);
      for (const [type, status] of [["reopen_item", "open"], ["archive_item", "archived"]] as const) {
        const next = apply(s, [{ op_id: "01", type, id: recurringMilk.id }]);
        assert.equal(next.items[0].status, status);
        assert.equal(next.items[0].last_purchase_date, "2026-11-01");
        assert.equal(next.items[0].due_date, "2026-11-08");
        assert.deepEqual(dueAgainItems(next), []);
      }
    });

    test("update_item recalculates or clears the due date", () => {
      const s = boughtMilk(["p1", "2026-11-01"]);
      const update = (fields: Extract<Op, { type: "update_item" }>["fields"]) =>
        apply(s, [{ op_id: "01", type: "update_item", id: recurringMilk.id, fields }]);
      const dueToday = update({ recurring_days: 23 });
      assert.equal(dueToday.items[0].due_date, s.today);
      assert.equal(dueAgainItems(dueToday).length, 1);
      const dueTomorrow = update({ recurring_days: 24 });
      assert.equal(dueTomorrow.items[0].due_date, "2026-11-25");
      assert.deepEqual(dueAgainItems(dueTomorrow), []);
      for (const cleared of [update({ recurring_days: null }), update({ list: "wishlist" })]) {
        assert.equal(cleared.items[0].last_purchase_date, "2026-11-01");
        assert.equal(cleared.items[0].due_date, null);
        assert.deepEqual(dueAgainItems(cleared), []);
      }
    });

    test("delete_purchase uses the latest remaining date and clears dates with no history", () => {
      const s = boughtMilk(["p1", "2026-11-01"], ["p2", "2026-10-20"], ["p3", "2026-10-31"]);
      const deletions: Op[] = ["p1", "p2", "p3"].map(purchase_id => ({
        op_id: purchase_id, type: "delete_purchase", purchase_id,
      }));
      const fallback = apply(s, deletions.slice(0, 1));
      assert.equal(fallback.items[0].last_purchase_date, "2026-10-31");
      assert.equal(fallback.items[0].due_date, "2026-11-07");
      assert.equal(dueAgainItems(fallback).length, 1);
      const cleared = apply(s, deletions);
      assert.equal(cleared.items[0].last_purchase_date, null);
      assert.equal(cleared.items[0].due_date, null);
      assert.deepEqual(dueAgainItems(cleared), []);
    });

    test("mark_bought uses the latest calendar date even when a new purchase is backdated", () => {
      const next = apply(snapshot([item({ id: "milk", recurring_days: 7 })]), [
        { op_id: "01", type: "mark_bought", id: "milk", purchase_id: "p1", date: "2024-02-24" },
        { op_id: "02", type: "mark_bought", id: "milk", purchase_id: "p2", date: "2024-01-01" },
      ]);
      assert.equal(next.items[0].last_purchase_date, "2024-02-24");
      assert.equal(next.items[0].due_date, "2024-03-02");
      assert.equal(dueAgainItems(next).length, 1);
    });
  });
}

describe("view grouping", () => {
  const s = snapshot([
    item({ id: "A", status: "open", list: "shopping", created_at: "2026-11-20T10:00:00Z" }),
    item({ id: "B", status: "open", list: "shopping", created_at: "2026-11-22T10:00:00Z" }),
    item({ id: "C", status: "open", list: "wishlist", created_at: "2026-11-21T10:00:00Z" }),
    item({ id: "D", status: "bought", due: true, recurring_days: 30, last_purchase_date: "2026-10-24", due_date: "2026-11-23" }),
    item({ id: "E", status: "bought", last_purchase_date: "2026-11-10" }),
    item({ id: "F", status: "archived" }),
  ]);
  test("open items are newest first per list", () => {
    assert.deepEqual(openItems(s, "shopping").map(i => i.id), ["B", "A"]);
    assert.deepEqual(openItems(s, "wishlist").map(i => i.id), ["C"]);
  });
  test("due again picks due items", () => {
    assert.deepEqual(dueAgainItems(s).map(i => i.id), ["D"]);
  });
  test("bought excludes archived", () => {
    assert.deepEqual(boughtItems(s).map(i => i.id).sort(), ["D", "E"]);
    assert.ok(!boughtItems(s).some(i => i.id === "F"));
  });
  test("archived picks archived", () => {
    assert.deepEqual(archivedItems(s).map(i => i.id), ["F"]);
  });
});
