import { createAPI, createProvider, expect, test } from "./fixtures";

test.describe("public catalog", () => {
  test("needs no login and lists the seeded APIs with prices", async ({ browser }) => {
    const ctx = await browser.newContext({ storageState: { cookies: [], origins: [] } });
    const page = await ctx.newPage();
    await page.goto("/");
    await expect(page.getByRole("heading", { name: "API catalog" })).toBeVisible();
    await expect(page.getByTestId("catalog-ifsc")).toContainText("IFSC Lookup");
    await expect(page.getByTestId("catalog-ifsc")).toContainText("₹0.0200 per call");
    await expect(page.getByTestId("catalog-echo")).toContainText("₹499.00/month · 10,000 calls");
    await ctx.close();
  });

  test("never shows drafts", async ({ page }) => {
    const provider = await createProvider(page, { signed: true });
    const slug = await createAPI(page, { provider });
    await page.goto("/");
    await expect(page.getByTestId("catalog-ifsc")).toBeVisible();
    await expect(page.getByTestId(`catalog-${slug}`)).toHaveCount(0);
  });
});

test("cards line up: same height in every row", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/");
  const cards = page.locator('[data-testid^="catalog-"]');
  await expect(cards.first()).toBeVisible();
  const heights = new Set((await cards.evaluateAll((els) => els.map((e) => Math.round(e.getBoundingClientRect().height)))));
  expect([...heights]).toHaveLength(1);
});
