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
import type { Item, Snapshot } from "../src/types.ts";

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
