import assert from "node:assert/strict";
import { beforeEach, test } from "node:test";
import "fake-indexeddb/auto";
import { clearLocal, openBasketDB, readLocal, writeLocal } from "../src/data/db.ts";
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
  const connectivity = { onLine: false };
  Object.defineProperty(globalThis, "navigator", { value: connectivity, configurable: true });
  Object.assign(globalThis, { window: events, document: new EventTarget() });
  const store = new Store();
  Reflect.deleteProperty(globalThis, "window");
  Reflect.deleteProperty(globalThis, "document");
  return {
    store,
    events,
    reconnect() {
      connectivity.onLine = true;
      events.dispatchEvent(new Event("online"));
      return store.getState().sending ? synced(store) : Promise.resolve();
    },
  };
}

const first: Op = { op_id: "01ARZ3NDEKTSV4RRFFQ69G5FAW", type: "add_item", id: "coffee", fields: { title: "coffee" } };
const second: Op = { op_id: "01ARZ3NDEKTSV4RRFFQ69G5FAV", type: "add_item", id: "filters", fields: { title: "filters" } };
const serverSnapshot = applyOp({ ...emptySnapshot(), today: "2026-11-26" }, {
  ...first,
  id: "tea",
  fields: { title: "tea" },
});

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

for (const cached of [null, applyOp(emptySnapshot(), first)]) {
  test(`refreshes a ${cached ? "stale" : "missing"} snapshot on reconnect with no queued edits`, { timeout: 5000 }, async t => {
    if (cached) {
      const db = await openBasketDB();
      await writeLocal(db, { snapshot: cached });
      db.close();
    }
    const fetch = t.mock.method(globalThis, "fetch", async (_url: string, _options: RequestInit) => Response.json(serverSnapshot));
    const tab = offlineTab();
    await tab.store.startup();
    assert.deepEqual(tab.store.getState().snapshot, cached);
    assert.equal(fetch.mock.callCount(), 0);

    await tab.reconnect();
    assert.equal(fetch.mock.callCount(), 1);
    const [url, options] = fetch.mock.calls[0].arguments;
    assert.equal(url, "/api/snapshot");
    assert.equal(options?.method ?? "GET", "GET");
    assert.deepEqual(tab.store.getState().snapshot, serverSnapshot);
  });
}

test("reapplies edits queued during a reconnect refresh before sending them in order", { timeout: 5000 }, async t => {
  const tab = offlineTab();
  await tab.store.startup();
  const refreshStarted = signal();
  const releaseRefresh = signal();
  const sendStarted = signal();
  const releaseSend = signal();
  const sent: Op[] = [];
  const committed = applyOp(applyOp(serverSnapshot, first), second);
  t.mock.method(globalThis, "fetch", async (url: string, options: RequestInit) => {
    if (url === "/api/snapshot") {
      refreshStarted.resolve();
      await releaseRefresh.promise;
      return Response.json(serverSnapshot);
    }
    assert.equal(url, "/api/ops");
    const { ops } = JSON.parse(options.body as string) as { ops: Op[] };
    sent.push(...ops);
    sendStarted.resolve();
    await releaseSend.promise;
    return Response.json({ snapshot: committed, results: ops.map(op => ({ op_id: op.op_id, ok: true })) });
  });

  const refreshed = tab.reconnect();
  await refreshStarted.promise;
  await tab.store.enqueue(first);
  await tab.store.enqueue(second);
  assert.deepEqual(items(tab.store), ["coffee", "filters"]);
  releaseRefresh.resolve();
  await sendStarted.promise;
  assert.deepEqual(tab.store.getState().queue, [first, second]);
  assert.deepEqual(items(tab.store), ["coffee", "filters", "tea"]);
  assert.deepEqual(sent, [first, second]);
  releaseSend.resolve();
  await refreshed;
  assert.deepEqual(tab.store.getState().queue, []);
  assert.deepEqual(tab.store.getState().snapshot, committed);
});

test("keeps a newer operation response when another tab sends during a reconnect refresh", { timeout: 5000 }, async t => {
  const refreshing = offlineTab();
  const editing = offlineTab();
  await Promise.all([refreshing.store.startup(), editing.store.startup()]);
  const refreshStarted = signal();
  const releaseRefresh = signal();
  const committed = applyOp(serverSnapshot, first);
  t.mock.method(globalThis, "fetch", async (url: string, options: RequestInit) => {
    if (url === "/api/snapshot") {
      refreshStarted.resolve();
      await releaseRefresh.promise;
      return Response.json(serverSnapshot);
    }
    assert.equal(url, "/api/ops");
    assert.deepEqual(JSON.parse(options.body as string), { ops: [first] });
    return Response.json({ snapshot: committed, results: [{ op_id: first.op_id, ok: true }] });
  });

  const refreshed = refreshing.reconnect();
  await refreshStarted.promise;
  await editing.store.enqueue(first);
  await editing.reconnect();
  assert.deepEqual(editing.store.getState().snapshot, committed);
  releaseRefresh.resolve();
  await refreshed;
  assert.deepEqual(refreshing.store.getState().snapshot, committed);
});

test("discards a reconnect response after sign-out starts", { timeout: 5000 }, async t => {
  const tab = offlineTab();
  await tab.store.startup();
  const refreshStarted = signal();
  const releaseRefresh = signal();
  const releaseLogout = signal();
  t.mock.method(globalThis, "fetch", async (url: string) => {
    if (url === "/auth/logout") {
      await releaseLogout.promise;
      return new Response(null, { status: 204 });
    }
    assert.equal(url, "/api/snapshot");
    refreshStarted.resolve();
    await releaseRefresh.promise;
    return Response.json(serverSnapshot);
  });

  const refreshed = tab.reconnect();
  await refreshStarted.promise;
  const signedOut = tab.store.signOut();
  try {
    releaseRefresh.resolve();
    await refreshed;
    assert.equal(tab.store.getState().signedOut, true);
    assert.equal(tab.store.getState().snapshot, null);
    const db = await openBasketDB();
    const local = await readLocal(db);
    db.close();
    assert.equal(local.snapshot, null);
  } finally {
    releaseLogout.resolve();
    await signedOut;
  }
});

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
