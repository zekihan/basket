import { expect, itemRow, quickAdd, snapshot, test } from "./fixtures.ts";

test("reloads queued changes offline and applies them exactly once after reconnect", async ({ page, context, signIn }) => {
  await signIn();
  await expect.poll(() => page.evaluate(() => navigator.serviceWorker.controller !== null)).toBe(true);
  await quickAdd(page, "coffee");
  const coffee = itemRow(page, "coffee");
  const filters = itemRow(page, "coffee filters");
  const status = page.getByTestId("sync-status");
  await expect(coffee).toBeVisible();
  await expect(status).toHaveText("Synced");
  const coffeeId = await coffee.getAttribute("data-item-id");
  expect(coffeeId).toBeTruthy();

  await context.setOffline(true);
  await expect(status).toHaveText("Offline");
  await quickAdd(page, "coffee filters");
  await expect(filters).toBeVisible();
  const filtersId = await filters.getAttribute("data-item-id");
  expect(filtersId).toBeTruthy();
  await coffee.getByTestId("bought-button").click();
  await page.getByRole("dialog", { name: "Bought: coffee" }).getByRole("button", { name: "Skip" }).click();
  await expect(coffee).toHaveCount(0);
  await expect(status).toHaveText("Offline · 2 pending");

  const remoteBefore = await snapshot(page);
  expect(remoteBefore.items).toMatchObject([{ id: coffeeId, status: "open" }]);
  expect(remoteBefore.purchases).toEqual([]);

  const reload = await page.reload();
  expect(reload?.fromServiceWorker()).toBe(true);
  await expect(status).toHaveText("Offline · 2 pending");
  await expect(filters).toHaveAttribute("data-item-id", filtersId!);
  await expect(coffee).toHaveCount(0);
  await page.getByTestId("tab-bought").click();
  await expect(coffee).toHaveAttribute("data-item-id", coffeeId!);
  await coffee.getByRole("button", { name: "coffee", exact: true }).click();
  const history = page.getByRole("dialog", { name: "coffee" })
    .getByRole("group", { name: "Purchase history" });
  await expect(history.getByTestId("delete-purchase")).toHaveCount(1);

  await context.setOffline(false);
  await expect(status).toHaveText("Synced");
  const remoteAfter = await snapshot(page);
  expect(remoteAfter.items).toMatchObject([
    { id: filtersId, title: "coffee filters", list: "shopping", status: "open" },
    { id: coffeeId, title: "coffee", list: "shopping", status: "bought" },
  ]);
  expect(remoteAfter.purchases).toMatchObject([{ item_id: coffeeId, quantity: 1 }]);
});
