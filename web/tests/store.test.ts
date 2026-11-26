import assert from "node:assert/strict";
import { beforeEach, test } from "node:test";
import "fake-indexeddb/auto";
import { clearLocal, openBasketDB } from "../src/data/db.ts";
import { applyOp, emptySnapshot } from "../src/data/logic.ts";
import { Store } from "../src/data/store.ts";
import type { Op } from "../src/types.ts";

beforeEach(async () => {
  const db = await openBasketDB();
  await clearLocal(db);
  db.close();
});

function offlineTab() {
  const events = new EventTarget();
  Object.defineProperty(globalThis, "navigator", { value: { onLine: false }, configurable: true });
  Object.assign(globalThis, { window: events, document: new EventTarget() });
  const store = new Store();
  Reflect.deleteProperty(globalThis, "window");
  Reflect.deleteProperty(globalThis, "document");
  return { store, events };
}

const first: Op = { op_id: "01ARZ3NDEKTSV4RRFFQ69G5FAW", type: "add_item", id: "coffee", fields: { title: "coffee" } };
const second: Op = { op_id: "01ARZ3NDEKTSV4RRFFQ69G5FAV", type: "add_item", id: "filters", fields: { title: "filters" } };

function items(store: Store) {
  return store.getState().snapshot?.items.map(item => item.id).sort();
}

function signal() {
  let resolve!: () => void;
  const promise = new Promise<void>(done => { resolve = done; });
  return { promise, resolve };
}

function synced(store: Store): Promise<void> {
  return new Promise(resolve => {
    const unsubscribe = store.subscribe(() => {
      if (store.getState().sending) return;
      unsubscribe();
      resolve();
    });
  });
}

test("reloads both tabs' offline changes in insertion order", async () => {
  {
    const { store: a } = offlineTab();
    const { store: b } = offlineTab();
    await Promise.all([a.startup(), b.startup()]);
    await a.enqueue(first);
    await b.enqueue(second);
  }

  const { store: reloaded } = offlineTab();
  await reloaded.startup();
  assert.deepEqual(reloaded.getState().queue, [first, second]);
  assert.deepEqual(items(reloaded), ["coffee", "filters"]);
});

test("commits an acknowledgement without removing another tab's queued operation", { timeout: 5000 }, async t => {
  const { store: a, events: aEvents } = offlineTab();
  const { store: b, events: bEvents } = offlineTab();
  await Promise.all([a.startup(), b.startup()]);

  const requestStarted = signal();
  const releaseResponse = signal();
  const sent: Op[] = [];
  let snapshot = emptySnapshot();
  t.mock.method(globalThis, "fetch", async (url: string, options: RequestInit) => {
    assert.equal(url, "/api/ops");
    const { ops } = JSON.parse(options.body as string) as { ops: Op[] };
    if (sent.length === 0) {
      requestStarted.resolve();
      await releaseResponse.promise;
    }
    for (const op of ops) snapshot = applyOp(snapshot, op);
    sent.push(...ops);
    return Response.json({ snapshot, results: ops.map(op => ({ op_id: op.op_id, ok: true })) });
  });

  await a.enqueue(first);
  aEvents.dispatchEvent(new Event("online"));
  await requestStarted.promise;
  await b.enqueue(second);
  aEvents.dispatchEvent(new Event("offline"));
  const firstSynced = synced(a);
  releaseResponse.resolve();
  await firstSynced;

  const { store: reloaded } = offlineTab();
  await reloaded.startup();
  assert.deepEqual(reloaded.getState().queue, [second]);
  assert.deepEqual(items(reloaded), ["coffee", "filters"]);

  bEvents.dispatchEvent(new Event("online"));
  await synced(b);
  assert.deepEqual(sent, [first, second]);

  const { store: finalReload } = offlineTab();
  await finalReload.startup();
  assert.deepEqual(finalReload.getState().queue, []);
  assert.deepEqual(items(finalReload), ["coffee", "filters"]);
});
