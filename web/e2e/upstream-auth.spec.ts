import { createAPI, createProvider, expect, expectGatewayStatus, publishWithPlan, subscribe, test } from "./fixtures";
import { env } from "./env";

// The in-stack apimock answers without a credential and echoes the query
// string, so it shows exactly what the gateway sent upstream.
const APIMOCK = "http://apimock:9000";

test("fixed upstream headers are sent on every call", async ({ page, cp, gateway }) => {
  const provider = await createProvider(page, { signed: true });
  // No auth; mockprovider's credential goes as a fixed header instead.
  const slug = await createAPI(page, { provider, auth: { type: "none" },
    fixedHeaders: [[env.mockProviderHeader, env.mockProviderSecret]] });
  await publishWithPlan(page);
  await subscribe(cp, slug);
  await expectGatewayStatus(gateway, `/${slug}/today`, 200);

  // Remove the header → mockprovider rejects the call.
  await page.getByRole("button", { name: "Remove row 1" }).click();
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect.poll(async () => (await (await cp.get(`/v1/admin/apis/${slug}`)).json()).upstream_headers).toEqual([]);
  await expectGatewayStatus(gateway, `/${slug}/today`, 401);
});

test("query auth: the gateway adds the key and a customer cannot override it", async ({ page, cp, gateway }) => {
  const provider = await createProvider(page, { signed: true });
  const slug = await createAPI(page, { provider, baseURL: APIMOCK, auth: { type: "query", name: "appid", secret: "q-secret-1" } });
  await expect(page.getByTestId("status").first()).toHaveText("draft");
  const stored = await (await cp.get(`/v1/admin/apis/${slug}`)).json();
  expect(stored.auth).toEqual({ type: "query", header: "appid", has_secret: true });
  await publishWithPlan(page);
  await subscribe(cp, slug);
  await expectGatewayStatus(gateway, `/${slug}/echo?x=1`, 200);

  const res = await gateway.get(`/${slug}/echo?x=1&appid=customer-value`);
  const echoed = new URLSearchParams((await res.json()).query);
  expect(echoed.getAll("appid")).toEqual(["q-secret-1"]);
  expect(echoed.get("x")).toBe("1");
});
