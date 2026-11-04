import { addPlan, createAPI, createProvider, expect, expectGatewayStatus, signAgreement, subscribe, test } from "./fixtures";

test("draft → plan → agreement → publish → callable → suspend", async ({ page, cp, gateway }) => {
  const provider = await createProvider(page);
  const slug = await createAPI(page, { provider });

  // A draft is invisible to customers.
  await expectGatewayStatus(gateway, `/${slug}/today`, 404);

  // Publishing needs a signed agreement, then an active plan.
  await page.getByTestId("publish").click();
  await expect(page.getByTestId("error-code")).toHaveText("AGREEMENT_REQUIRED");

  await signAgreement(page, provider);
  await page.goto(`/admin/apis/${slug}`);
  await page.getByTestId("publish").click();
  await expect(page.getByTestId("error-code")).toHaveText("NO_ACTIVE_PLAN");

  await addPlan(page, { name: "PAYG", price: "0.02", cost: "0.01" });
  const row = page.getByTestId("plan-row-PAYG");
  await expect(row).toContainText("₹0.02 per call");
  await expect(row).toContainText("₹0.01/call");

  await page.getByTestId("publish").click();
  await expect(page.getByTestId("status").first()).toHaveText("published");

  // Public catalog shows it at once (the publish action refreshes the cache tag).
  await page.goto("/");
  const card = page.getByTestId(`catalog-${slug}`);
  await expect(card).toContainText(`E2E ${slug}`);
  await expect(card).toContainText("₹0.0200 per call");

  // A subscribed customer can call it; the gateway injects the secret typed in the UI.
  await subscribe(cp, slug);
  await expectGatewayStatus(gateway, `/${slug}/today?city=Mysuru`, 200);
  expect(await (await gateway.get(`/${slug}/today?city=Mysuru`)).json()).toMatchObject({ city: "Mysuru" });

  // Suspend: calls stop, the catalog drops it.
  await page.goto(`/admin/apis/${slug}`);
  page.once("dialog", (d) => d.accept());
  await page.getByTestId("suspend").click();
  await expect(page.getByTestId("status").first()).toHaveText("suspended");
  await expectGatewayStatus(gateway, `/${slug}/today`, 404);
  await page.goto("/");
  await expect(page.getByTestId(`catalog-${slug}`)).toHaveCount(0);

  // Publishing again restores access for the existing subscriber.
  await page.goto(`/admin/apis/${slug}`);
  await page.getByTestId("publish").click();
  await expectGatewayStatus(gateway, `/${slug}/today`, 200);
});
