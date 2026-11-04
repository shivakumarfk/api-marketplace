import { addPlan, createAPI, createProvider, expect, test } from "./fixtures";
import { env, unique } from "./env";

test("API form shows control-plane errors inline and keeps the input", async ({ page }) => {
  const provider = await createProvider(page);
  const taken = await createAPI(page, { provider });

  const cases: [string, string, string][] = [
    ["healthz", env.mockProviderURL, "RESERVED_SLUG"],
    [unique(), "https://x.example/?key=1", "INVALID_BASE_URL"],
    [taken, env.mockProviderURL, "SLUG_TAKEN"],
  ];
  for (const [slug, baseURL, code] of cases) {
    await page.goto("/admin/apis/new");
    await page.getByLabel("Provider", { exact: true }).selectOption({ label: provider });
    await page.getByLabel("API path").fill(slug);
    await page.getByLabel("Name", { exact: true }).fill("Keep me");
    await page.getByLabel("Base URL").fill(baseURL);
    await page.getByRole("button", { name: "Create draft" }).click();
    await expect(page.getByTestId("error-code"), slug).toHaveText(code);
    await expect(page).toHaveURL(/\/admin\/apis\/new$/);
    await expect(page.getByLabel("Name", { exact: true })).toHaveValue("Keep me");
    await expect(page.getByLabel("API path")).toHaveValue(slug);
    await expect(page.getByLabel("Base URL")).toHaveValue(baseURL);
  }
});

test("plan form rejects bad money and negative margins", async ({ page }) => {
  const provider = await createProvider(page);
  await createAPI(page, { provider });
  const form = page.locator("form").filter({ has: page.getByRole("button", { name: "Add plan" }) });

  await addPlan(page, { name: "Sci", price: "1e3" });
  await expect(form.getByTestId("error-code")).toHaveText("INVALID_AMOUNT");
  await expect(form.getByLabel("Plan name")).toHaveValue("Sci");

  await addPlan(page, { name: "Loss", price: "0.01", cost: "0.02" });
  await expect(form.getByTestId("error-code")).toHaveText("NEGATIVE_MARGIN");

  await addPlan(page, { name: "Starter", kind: "tier", fee: "499", included: "10000" });
  await expect(page.getByTestId("plan-row-Starter")).toContainText("₹499.00/month · 10,000 calls");
});

test("the last active plan of a published API cannot be retired", async ({ page }) => {
  const provider = await createProvider(page, { signed: true });
  await createAPI(page, { provider });
  await addPlan(page, { name: "Only", price: "0.05" });
  await expect(page.getByTestId("plan-row-Only")).toBeVisible();
  await page.getByTestId("publish").click();
  await expect(page.getByTestId("status").first()).toHaveText("published");

  const row = page.getByTestId("plan-row-Only");
  await row.getByTestId("retire-Only").click();
  await expect(row.getByTestId("error-code")).toHaveText("LAST_ACTIVE_PLAN");

  // With a second plan the first can go.
  await addPlan(page, { name: "Backup", price: "0.05" });
  await expect(page.getByTestId("plan-row-Backup")).toBeVisible();
  await page.getByTestId("plan-row-Only").getByTestId("retire-Only").click();
  await expect(page.getByTestId("plan-row-Only").getByTestId("status")).toHaveText("retired");
});
