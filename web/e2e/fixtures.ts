import { expect, test as base, type APIRequestContext, type Page } from "@playwright/test";
import { env, unique } from "./env";

// cp: the admin API directly, for steps with no UI yet (subscribing an org).
// gateway: customer calls with the seeded institution key.
// cleanup (automatic): suspends every API the test created, pass or fail, so
// test APIs never stay in the public catalog.
type Fixtures = { cp: APIRequestContext; gateway: APIRequestContext; cleanup: void };

const createdByTest = new Map<string, string[]>();

function track(slug: string) {
  const id = base.info().testId;
  createdByTest.set(id, [...(createdByTest.get(id) ?? []), slug]);
}

export const test = base.extend<Fixtures>({
  cleanup: [
    async ({ cp }, provide, testInfo) => {
      await provide();
      for (const slug of createdByTest.get(testInfo.testId) ?? []) {
        // 409 NOT_PUBLISHED (still a draft, or already suspended) is fine.
        const res = await cp.post(`/v1/admin/apis/${slug}/suspend`);
        if (![200, 409].includes(res.status())) throw new Error(`cleanup of ${slug}: ${res.status()} ${await res.text()}`);
      }
      createdByTest.delete(testInfo.testId);
    },
    { auto: true },
  ],
  cp: async ({ playwright }, provide) => {
    const ctx = await playwright.request.newContext({
      baseURL: env.controlPlane,
      extraHTTPHeaders: { Authorization: `Bearer ${env.adminToken}` },
    });
    await provide(ctx);
    await ctx.dispose();
  },
  gateway: async ({ playwright }, provide) => {
    const ctx = await playwright.request.newContext({ baseURL: env.gateway, extraHTTPHeaders: { "X-API-Key": env.institutionKey } });
    await provide(ctx);
    await ctx.dispose();
  },
});
export { expect };

// ---------- UI helpers (they drive the real forms) ----------

export async function createProvider(page: Page, opts: { signed?: boolean } = {}): Promise<string> {
  const name = unique("prov");
  await page.goto("/admin/providers");
  await page.getByLabel("Name", { exact: true }).fill(name);
  await page.getByLabel("Contact email").fill(`${name}@example.com`);
  if (opts.signed) await page.getByLabel("Resale agreement already signed").check();
  await page.getByRole("button", { name: "Add provider" }).click();
  await expect(page.getByTestId(`provider-row-${name}`)).toBeVisible();
  return name;
}

type AuthOpts = { type: "none" } | { type: "api_key" | "query"; name: string; secret: string } | { type: "bearer" | "basic"; secret: string };

export async function createAPI(page: Page, opts: {
  provider: string; slug?: string; secret?: string; baseURL?: string; auth?: AuthOpts; fixedHeaders?: [string, string][];
}): Promise<string> {
  const slug = opts.slug ?? unique();
  const auth: AuthOpts = opts.auth ?? { type: "api_key", name: env.mockProviderHeader, secret: opts.secret ?? env.mockProviderSecret };
  await page.goto("/admin/apis/new");
  await page.getByLabel("Provider", { exact: true }).selectOption({ label: opts.provider });
  await page.getByLabel("API path").fill(slug);
  await page.getByLabel("Name", { exact: true }).fill(`E2E ${slug}`);
  await page.getByLabel("Category").fill("testing");
  await page.getByLabel("Base URL").fill(opts.baseURL ?? env.mockProviderURL);
  await page.getByLabel("Upstream auth").selectOption(auth.type);
  if (auth.type === "api_key") await page.getByLabel("Header name").fill(auth.name);
  if (auth.type === "query") await page.getByLabel("Query parameter name").fill(auth.name);
  if (auth.type !== "none") await page.locator('input[name="secret"]').fill(auth.secret);
  for (const [i, [name, value]] of (opts.fixedHeaders ?? []).entries()) {
    await page.getByRole("button", { name: "Add header" }).click();
    await page.getByLabel(`Header ${i + 1}`, { exact: true }).fill(name);
    await page.getByLabel(`Value ${i + 1}`, { exact: true }).fill(value);
  }
  await page.getByRole("button", { name: "Create draft" }).click();
  await expect(page).toHaveURL(new RegExp(`/admin/apis/${slug}$`));
  track(slug);
  await expect(page.getByTestId("status").first()).toHaveText("draft");
  return slug;
}

export async function addPlan(page: Page, opts: { name?: string; kind?: "payg" | "tier" | "free"; price?: string; cost?: string; included?: string; fee?: string } = {}) {
  const name = opts.name ?? "PAYG";
  const form = page.locator("form").filter({ has: page.getByRole("button", { name: "Add plan" }) });
  await form.getByLabel("Plan name").fill(name);
  await form.getByLabel("Kind").selectOption(opts.kind ?? "payg");
  // Set every visible field: after an error the form keeps the previous input.
  const kind = opts.kind ?? "payg";
  if (kind === "tier") await form.getByLabel("Monthly fee (₹)").fill(opts.fee ?? "");
  if (kind !== "payg") await form.getByLabel("Included calls / month").fill(opts.included ?? "");
  if (kind !== "free") await form.getByLabel(/price per call/i).fill(opts.price ?? "");
  await form.getByLabel("Provider cost per call (₹)").fill(opts.cost ?? "");
  await form.getByLabel("Rate limit (req/s)").fill("10");
  await form.getByRole("button", { name: "Add plan" }).click();
  return name;
}

export async function signAgreement(page: Page, provider: string) {
  await page.goto("/admin/providers");
  await page.getByTestId(`sign-${provider}`).click();
  await expect(page.getByTestId(`provider-row-${provider}`)).toContainText("Signed");
}

// subscribe puts the seeded institution on the API's first active plan.
export async function subscribe(cp: APIRequestContext, slug: string) {
  const api = await (await cp.get(`/v1/admin/apis/${slug}`)).json();
  const plan = api.plans.find((p: { status: string }) => p.status === "active");
  const res = await cp.post(`/v1/admin/orgs/${env.institutionOrg}/subscriptions`, { data: { api_slug: slug, plan_id: plan.id } });
  expect(res.status(), await res.text()).toBe(201);
}

// gatewayStatus polls until the gateway (which refreshes its catalog every few
// seconds) returns the expected status.
export async function expectGatewayStatus(gateway: APIRequestContext, path: string, status: number) {
  await expect.poll(async () => (await gateway.get(path)).status(), { timeout: 20_000, intervals: [500, 1000] }).toBe(status);
}

type ParamRow = { name: string; in?: "query" | "path"; required?: boolean; description?: string; example?: string };
type HeaderRow = { name: string; required?: boolean; example?: string };

// addEndpoint fills the admin "Add endpoint" form on the API page.
export async function addEndpoint(page: Page, e: {
  method?: string; path: string; summary?: string; params?: ParamRow[]; headers?: HeaderRow[];
  requestExample?: string; status?: string; response?: string;
}) {
  const form = page.getByTestId("add-endpoint");
  await form.getByLabel("Method").selectOption(e.method ?? "GET");
  await form.getByLabel("Path", { exact: true }).fill(e.path);
  if (e.summary) await form.getByLabel("Summary").fill(e.summary);
  const params = form.locator('[data-testid$="-params"]');
  for (const [i, p] of (e.params ?? []).entries()) {
    await form.getByRole("button", { name: "Add parameter" }).click();
    await params.getByLabel(`Name ${i + 1}`, { exact: true }).fill(p.name);
    await params.getByLabel(`In ${i + 1}`, { exact: true }).selectOption(p.in ?? "query");
    if (p.required) await params.getByLabel(`Required ${i + 1}`, { exact: true }).check();
    if (p.description) await params.getByLabel(`Description ${i + 1}`, { exact: true }).fill(p.description);
    if (p.example) await params.getByLabel(`Example ${i + 1}`, { exact: true }).fill(p.example);
  }
  const headers = form.locator('[data-testid$="-headers"]');
  for (const [i, h] of (e.headers ?? []).entries()) {
    await form.getByRole("button", { name: "Add header" }).click();
    await headers.getByLabel(`Header ${i + 1}`, { exact: true }).fill(h.name);
    if (h.required) await headers.getByLabel(`Required ${i + 1}`, { exact: true }).check();
    if (h.example) await headers.getByLabel(`Example ${i + 1}`, { exact: true }).fill(h.example);
  }
  if (e.requestExample !== undefined) await form.getByLabel("Request body example").fill(e.requestExample);
  await form.getByLabel("Response status").fill(e.status ?? "200");
  if (e.response !== undefined) await form.getByLabel("Response example").fill(e.response);
  await form.getByRole("button", { name: "Add endpoint" }).click();
}

// publishWithPlan adds a pay-per-call plan and publishes (provider must be signed).
export async function publishWithPlan(page: Page) {
  await addPlan(page, { price: "0.02" });
  await expect(page.getByTestId("plan-row-PAYG")).toBeVisible();
  await page.getByTestId("publish").click();
  await expect(page.getByTestId("status").first()).toHaveText("published");
}
