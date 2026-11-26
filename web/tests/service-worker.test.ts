import assert from "node:assert/strict";
import test from "node:test";
import type { TestContext } from "node:test";
import { ensureLatestApp } from "../src/data/service-worker.ts";

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>(done => { resolve = done; });
  return { promise, resolve };
}

function browser(t: TestContext, onLine = true) {
  const original = {};
  const registration = Object.assign(new EventTarget(), {
    update: async () => {}, installing: null as EventTarget | null, waiting: null as EventTarget | null,
  });
  const register = t.mock.fn(async (_url: string, _options: RegistrationOptions) => registration);
  const serviceWorker = Object.assign(new EventTarget(), {
    controller: original, register, getRegistration: async () => registration,
  });
  const reload = t.mock.fn();
  const navigatorDescriptor = Object.getOwnPropertyDescriptor(globalThis, "navigator");
  const locationDescriptor = Object.getOwnPropertyDescriptor(globalThis, "location");
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: { onLine, serviceWorker } });
  Object.defineProperty(globalThis, "location", { configurable: true, value: { reload } });
  t.after(() => {
    if (navigatorDescriptor) Object.defineProperty(globalThis, "navigator", navigatorDescriptor);
    else Reflect.deleteProperty(globalThis, "navigator");
    if (locationDescriptor) Object.defineProperty(globalThis, "location", locationDescriptor);
    else Reflect.deleteProperty(globalThis, "location");
  });
  const activate = () => {
    serviceWorker.controller = {};
    serviceWorker.dispatchEvent(new Event("controllerchange"));
  };
  return { serviceWorker, registration, activate, reload };
}

const flush = () => new Promise<void>(resolve => setImmediate(resolve));

test("online startup waits through a slow service worker check and installation", async t => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const { serviceWorker, registration, activate, reload } = browser(t);
  const entered = deferred();
  const checked = deferred();
  registration.update = () => {
    entered.resolve();
    return checked.promise;
  };
  let finished = false;
  const startup = ensureLatestApp(5000).then(result => {
    finished = true;
    return result;
  });
  await entered.promise;
  assert.deepEqual(serviceWorker.register.mock.calls[0]?.arguments, ["/sw.js", { updateViaCache: "none" }]);
  t.mock.timers.tick(3500);
  await flush();
  assert.equal(finished, false);
  const worker = Object.assign(new EventTarget(), { state: "installing", postMessage: t.mock.fn() });
  registration.installing = worker;
  checked.resolve();
  await flush();
  t.mock.timers.tick(500);
  await flush();
  assert.equal(finished, false);
  assert.equal(worker.postMessage.mock.callCount(), 0);
  worker.state = "installed";
  worker.dispatchEvent(new Event("statechange"));
  await flush();
  assert.deepEqual(worker.postMessage.mock.calls[0]?.arguments, [{ type: "activate" }]);
  assert.equal(finished, false);
  activate();
  assert.equal(await startup, true);
  activate();
  assert.equal(reload.mock.callCount(), 1);
});

test("an update discovered after the startup deadline stays waiting", async t => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const { registration, activate, reload } = browser(t);
  const checked = deferred();
  registration.update = () => checked.promise;
  const startup = ensureLatestApp(50);
  await flush();
  t.mock.timers.tick(50);
  assert.equal(await startup, false);

  const worker = Object.assign(new EventTarget(), { state: "installed", postMessage: t.mock.fn() });
  registration.waiting = worker;
  registration.dispatchEvent(new Event("updatefound"));
  checked.resolve();
  await flush();
  assert.equal(worker.postMessage.mock.callCount(), 0);
  activate();
  assert.equal(reload.mock.callCount(), 0);
});

test("offline startup skips the update check and leaves a waiting worker alone", async t => {
  const { registration, activate, reload } = browser(t, false);
  const update = t.mock.method(registration, "update");
  const worker = Object.assign(new EventTarget(), { state: "installed", postMessage: t.mock.fn() });
  registration.waiting = worker;

  assert.equal(await ensureLatestApp(5000), false);
  activate();
  assert.equal(update.mock.callCount(), 0);
  assert.equal(worker.postMessage.mock.callCount(), 0);
  assert.equal(reload.mock.callCount(), 0);
});
