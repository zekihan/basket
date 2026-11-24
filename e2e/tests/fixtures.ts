import { spawn } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { test as base, expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import type { Snapshot } from "../../web/src/types.ts";

const bin = fileURLToPath(new URL("../.bin/", import.meta.url));

type Provider = { issuer: string; clientId: string; clientSecret: string; subject: string };

function start(binary: string, env = process.env) {
  const child = spawn(join(bin, binary), [], { env, stdio: ["ignore", "pipe", "pipe"] });
  let stdout = "";
  let stderr = "";
  let stopped = "";
  child.stdout.setEncoding("utf8").on("data", chunk => { stdout += chunk; });
  child.stderr.setEncoding("utf8").on("data", chunk => { stderr += chunk; });
  child.once("error", error => { stopped = error.message; });
  const closed = new Promise<void>(resolve => child.once("close", (code, signal) => {
    stopped ||= `Exited with ${signal ?? code}`;
    resolve();
  }));

  return {
    stdout: () => stdout,
    assertRunning() {
      if (stopped) throw new Error(`${binary}: ${stopped}\n${stderr}`);
    },
    async stop() {
      if (!stopped) child.kill();
      const deadline = setTimeout(() => child.kill("SIGKILL"), 3_000);
      try { await closed; } finally { clearTimeout(deadline); }
    },
  };
}

async function availablePort(): Promise<number> {
  const listener = createServer();
  await new Promise<void>((resolve, reject) => {
    listener.once("error", reject);
    listener.listen(0, "127.0.0.1", resolve);
  });
  const address = listener.address();
  await new Promise<void>((resolve, reject) => listener.close(error => error ? reject(error) : resolve()));
  if (!address || typeof address === "string") throw new Error("No loopback port was allocated");
  return address.port;
}

export const test = base.extend<{ signIn: () => Promise<void> }, { provider: Provider }>({
  provider: [async ({}, use) => {
    const provider = start("mockidp");
    try {
      await expect.poll(() => {
        provider.assertRunning();
        return provider.stdout();
      }).toContain("\n");
      await use(JSON.parse(provider.stdout()) as Provider);
    } finally {
      await provider.stop();
    }
  }, { scope: "worker" }],

  baseURL: async ({ provider }, use) => {
    const address = `127.0.0.1:${await availablePort()}`;
    const url = `http://${address}`;
    const directory = await mkdtemp(join(tmpdir(), "basket-e2e-"));
    const server = start("basket", {
      ...process.env,
      PUBLIC_URL: url,
      LISTEN_ADDR: address,
      METRICS_ADDR: "",
      COOKIE_INSECURE: "true",
      DB_PATH: join(directory, "basket.db"),
      BACKUP_DIR: "",
      OIDC_ISSUER: provider.issuer,
      OIDC_CLIENT_ID: provider.clientId,
      OIDC_CLIENT_SECRET: provider.clientSecret,
      OWNER_SUBJECT: provider.subject,
      MCP_CLIENT_ID: "",
    });
    try {
      await expect.poll(async () => {
        server.assertRunning();
        const response = await fetch(`${url}/healthz`).catch(() => null);
        await response?.body?.cancel();
        return response?.status;
      }).toBe(200);
      await use(url);
    } finally {
      await server.stop();
      await rm(directory, { recursive: true, force: true });
    }
  },

  signIn: async ({ page }, use) => {
    await use(async () => {
      await page.goto("/");
      await page.getByRole("link", { name: "Sign in" }).click();
      await expect(page.getByTestId("tab-shopping")).toBeVisible();
    });
  },
});

export async function snapshot(page: Page): Promise<Snapshot> {
  const response = await page.request.get("/api/snapshot");
  await expect(response).toBeOK();
  return await response.json() as Snapshot;
}

export async function quickAdd(page: Page, text: string): Promise<void> {
  const input = page.getByTestId("quick-add");
  await input.fill(text);
  await input.press("Enter");
}

export function itemRow(page: Page, title: string) {
  return page.getByTestId("item-row").filter({
    has: page.getByRole("button", { name: title, exact: true }),
  });
}

export { expect };
