import { expect, itemRow, quickAdd, snapshot, test } from "./fixtures.ts";

test("signs in through OIDC and opens an empty Shopping tab", async ({ page, signIn, provider }) => {
  await Promise.all([
    page.waitForRequest(request => request.url().startsWith(`${provider.issuer}/authorize?`)),
    signIn(),
  ]);

  const me = await page.request.get("/auth/me");
  await expect(me).toBeOK();
  expect(await me.json()).toMatchObject({ subject: provider.subject });
  await expect(page.getByTestId("tab-shopping")).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("region", { name: "Shopping" })).toHaveText("Nothing open here yet.");
  await expect(page.getByTestId("item-row")).toHaveCount(0);
});

test("quick-add splits title and notes and saves them across reload", async ({ page, signIn }) => {
  await signIn();
  await quickAdd(page, "charger cable, usb-c 60W");

  const row = itemRow(page, "charger cable");
  await expect(row).toBeVisible();
  await expect(row.getByText("usb-c 60W", { exact: true })).toBeVisible();
  await expect(page.getByTestId("sync-status")).toHaveText("Synced");
  const id = await row.getAttribute("data-item-id");
  expect(id).toBeTruthy();
  expect((await snapshot(page)).items).toMatchObject([
    { id, list: "shopping", title: "charger cable", notes: "usb-c 60W", status: "open" },
  ]);

  await page.reload();
  await expect(row).toHaveAttribute("data-item-id", id!);
  await expect(row.getByText("usb-c 60W", { exact: true })).toBeVisible();
});

test("checks off an item and records its price and store in purchase history", async ({ page, signIn }) => {
  await signIn();
  await quickAdd(page, "charger cable");

  const row = itemRow(page, "charger cable");
  await row.getByTestId("bought-button").click();
  const purchase = page.getByRole("dialog", { name: "Bought: charger cable" });
  await purchase.getByTestId("purchase-price").fill("189,90");
  await purchase.getByTestId("purchase-store").fill("Trendyol");
  await purchase.getByTestId("purchase-save").click();
  await expect(purchase).toBeHidden();
  await expect(row).toHaveCount(0);
  await expect(page.getByTestId("sync-status")).toHaveText("Synced");

  await page.getByTestId("tab-bought").click();
  await row.getByRole("button", { name: "charger cable", exact: true }).click();
  const history = page.getByRole("dialog", { name: "charger cable" })
    .getByRole("group", { name: "Purchase history" });
  await expect(history.getByText(/ · 189,90 · Trendyol$/)).toBeVisible();
});

test("undo restores a deleted row on the server and across reload", async ({ page, signIn }) => {
  await signIn();
  await quickAdd(page, "dish soap");
  const row = itemRow(page, "dish soap");
  await expect(row).toBeVisible();
  await expect(page.getByTestId("sync-status")).toHaveText("Synced");
  const id = await row.getAttribute("data-item-id");
  expect(id).toBeTruthy();

  await row.getByTestId("delete-button").click();
  await expect(row).toHaveCount(0);
  await expect(page.getByTestId("sync-status")).toHaveText("Synced");
  expect((await snapshot(page)).items).toEqual([]);

  await page.getByTestId("undo-button").click();
  await expect(row).toHaveAttribute("data-item-id", id!);
  await expect(page.getByTestId("sync-status")).toHaveText("Synced");
  expect((await snapshot(page)).items).toMatchObject([
    { id, title: "dish soap", status: "open" },
  ]);

  await page.reload();
  await expect(row).toHaveAttribute("data-item-id", id!);
});

test("quick-add from Wishlist keeps the item out of Shopping", async ({ page, signIn }) => {
  await signIn();
  await page.getByTestId("tab-wishlist").click();
  await quickAdd(page, "desk lamp");
  const row = itemRow(page, "desk lamp");

  await expect(page.getByRole("region", { name: "Wishlist" }).locator(row)).toBeVisible();
  await expect(page.getByTestId("sync-status")).toHaveText("Synced");
  expect((await snapshot(page)).items).toMatchObject([
    { list: "wishlist", title: "desk lamp", status: "open" },
  ]);
  await page.getByTestId("tab-shopping").click();
  await expect(page.getByRole("region", { name: "Shopping" })).toHaveText("Nothing open here yet.");
  await expect(row).toHaveCount(0);
});
