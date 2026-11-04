import { addEndpoint, createAPI, createProvider, expect, publishWithPlan, test } from "./fixtures";
import { env } from "./env";

test("documented endpoints appear on the public page with request details and Try it", async ({ page, browser }) => {
  const provider = await createProvider(page, { signed: true });
  const slug = await createAPI(page, { provider });
  await addEndpoint(page, {
    path: "/items/{id}", summary: "Get an item",
    params: [
      { name: "id", in: "path", description: "Item id", example: "42" },
      { name: "fields", in: "query", required: true, description: "Fields to return", example: "name,price" },
    ],
    headers: [{ name: "Accept-Language", required: false, example: "en-IN" }],
    response: `{"id":42,"name":"Widget","price":"9.99"}`,
  });
  await expect(page.getByTestId("endpoint-row-GET-/items/{id}")).toContainText("Get an item");
  await publishWithPlan(page);

  // The public page, as an anonymous visitor.
  const ctx = await browser.newContext({ storageState: { cookies: [], origins: [] } });
  const pub = await ctx.newPage();
  await pub.goto(`/apis/${slug}`);
  await expect(pub.getByTestId("base-url")).toHaveText(`http://localhost:8080/${slug}`);
  const doc = pub.getByTestId("endpoint-GET-/items/{id}");
  await expect(doc).toContainText(`/${slug}/items/{id}`);
  await expect(doc).toContainText("Fields to return");
  await expect(doc).toContainText("Accept-Language");
  await expect(pub.getByText("₹0.0200 per call")).toBeVisible();

  // Try it: curl is built from examples, then from what the user types.
  const tryIt = doc.getByTestId("try-GET-/items/{id}");
  const curl = tryIt.getByTestId("try-curl");
  await expect(curl).toContainText(`http://localhost:8080/${slug}/items/42?fields=name%2Cprice`);
  await expect(curl).toContainText("X-API-Key: $API_KEY");
  await expect(curl).toContainText("Accept-Language: en-IN");
  await tryIt.getByLabel("id (path)").fill("7");
  await expect(curl).toContainText(`/${slug}/items/7?`);
  await tryIt.getByRole("button", { name: "Send to mock" }).click();
  await expect(tryIt.getByTestId("try-status")).toHaveText("200");
  await expect(tryIt.getByTestId("try-body")).toContainText(`"name": "Widget"`);

  // Missing required value → told what to fill, no fake response.
  await tryIt.getByLabel("fields (query)").fill("");
  await tryIt.getByRole("button", { name: "Send to mock" }).click();
  await expect(tryIt.getByTestId("try-result")).toContainText("Fill the required value: fields");

  // Nothing internal reaches the public page.
  const html = await pub.content();
  for (const internal of ["mockprovider", env.mockProviderHeader, env.mockProviderSecret, "Provider cost"]) {
    expect(html, internal).not.toContain(internal);
  }
  await ctx.close();
});

test("endpoint validation errors show inline, and edits and deletes reach the public page", async ({ page }) => {
  const provider = await createProvider(page, { signed: true });
  const slug = await createAPI(page, { provider });
  const form = page.getByTestId("add-endpoint");

  await addEndpoint(page, { path: "/things/{id}" });
  await expect(form.getByTestId("error-code")).toHaveText("INVALID_PARAM");
  await expect(form.getByLabel("Path", { exact: true })).toHaveValue("/things/{id}");

  await addEndpoint(page, { path: "/today?city=x" });
  await expect(form.getByTestId("error-code")).toHaveText("INVALID_PATH");

  await addEndpoint(page, { path: "/today", summary: "First" });
  await expect(page.getByTestId("endpoint-row-GET-/today")).toBeVisible();
  await addEndpoint(page, { path: "/today" });
  await expect(form.getByTestId("error-code")).toHaveText("ENDPOINT_EXISTS");

  await publishWithPlan(page);

  // Edit the summary inline.
  const row = page.getByTestId("endpoint-row-GET-/today");
  await row.locator("summary").click();
  await row.getByLabel("Summary").fill("Renamed summary");
  await row.getByRole("button", { name: "Save endpoint" }).click();
  await expect(row.locator("summary")).toContainText("Renamed summary");
  await page.goto(`/apis/${slug}`);
  await expect(page.getByTestId("endpoint-GET-/today")).toContainText("Renamed summary");

  // Delete it.
  await page.goto(`/admin/apis/${slug}`);
  await page.getByTestId("endpoint-row-GET-/today").locator("summary").click();
  page.once("dialog", (d) => d.accept());
  await page.getByTestId("delete-GET-/today").click();
  await expect(page.getByTestId("endpoint-row-GET-/today")).toHaveCount(0);
  await page.goto(`/apis/${slug}`);
  await expect(page.getByText("hasn't documented endpoints yet")).toBeVisible();
});

test("seeded APIs have public docs", async ({ page }) => {
  await page.goto("/");
  await page.getByTestId("catalog-ifsc").getByRole("link").click();
  await expect(page).toHaveURL(/\/apis\/ifsc$/);
  const doc = page.getByTestId("endpoint-GET-/{ifsc}");
  await doc.getByRole("button", { name: "Send to mock" }).click();
  await expect(doc.getByTestId("try-body")).toContainText("HDFC Bank");
});
