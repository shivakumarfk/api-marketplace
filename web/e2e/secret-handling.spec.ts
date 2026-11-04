import { addPlan, createAPI, createProvider, expect, expectGatewayStatus, subscribe, test } from "./fixtures";
import { env } from "./env";

// A distinctive value we can search for in every byte the browser receives.
const MARKER = env.mockProviderSecret;

test("the provider secret never reaches the browser, and edits keep, rotate or clear it", async ({ page, cp, gateway }) => {
  const seen: string[] = [];
  page.on("response", async (res) => {
    try {
      seen.push(`${res.url()}\n${await res.text()}`);
    } catch {
      // redirects and aborted responses have no body
    }
  });

  const provider = await createProvider(page, { signed: true });
  const slug = await createAPI(page, { provider });
  await addPlan(page, { price: "0.02" });
  await expect(page.getByTestId("plan-row-PAYG")).toBeVisible();
  await page.getByTestId("publish").click();
  await expect(page.getByTestId("status").first()).toHaveText("published");
  await subscribe(cp, slug);
  await expectGatewayStatus(gateway, `/${slug}/today`, 200);

  // Edit page: secret field empty, "stored" hint shown.
  await page.reload();
  await expect(page.getByLabel("Provider secret")).toHaveValue("");
  await expect(page.getByText("A secret is stored")).toBeVisible();

  // Save with a blank secret (rename only) → kept: upstream still accepts.
  await page.getByLabel("Name", { exact: true }).fill(`Renamed ${slug}`);
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toContainText(`Renamed ${slug}`);
  await page.waitForTimeout(6000); // one gateway catalog refresh
  expect((await gateway.get(`/${slug}/today`)).status()).toBe(200);

  // Rotate to a wrong secret → the gateway now sends it and mockprovider answers 401.
  await page.getByLabel("Provider secret").fill("rotated-wrong-secret");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByRole("status").last()).toHaveText("Saved");
  await expectGatewayStatus(gateway, `/${slug}/today`, 401);

  // Switch auth to none → header and secret are gone.
  await page.getByLabel("Upstream auth").selectOption("none");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect.poll(async () => (await (await cp.get(`/v1/admin/apis/${slug}`)).json()).auth).toEqual({ type: "none", has_secret: false });
  await page.reload();
  await expect(page.getByLabel("Upstream auth")).toHaveValue("none");
  await expect(page.getByLabel("Header name")).toHaveCount(0);

  // Nothing the browser received ever contained the secret.
  await page.goto("/admin/apis");
  const leaks = seen.filter((s) => s.includes(MARKER));
  expect(leaks.map((s) => s.split("\n")[0])).toEqual([]);
});
