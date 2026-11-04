import { expect, test } from "@playwright/test";
import { env } from "./env";

// These run signed out.
test.use({ storageState: { cookies: [], origins: [] } });

test("admin pages redirect to login and come back after it", async ({ page }) => {
  await page.goto("/admin/providers");
  await expect(page).toHaveURL(/\/admin\/login\?next=%2Fadmin%2Fproviders$/);
  await page.getByLabel("Admin token").fill(env.adminToken);
  await page.getByRole("button", { name: "Log in" }).click();
  await expect(page).toHaveURL(/\/admin\/providers$/);
});

test("a wrong token is rejected and sets no cookie", async ({ page, context }) => {
  await page.goto("/admin/login");
  await page.getByLabel("Admin token").fill("not-the-token");
  await page.getByRole("button", { name: "Log in" }).click();
  await expect(page.getByTestId("error-code")).toHaveText("UNAUTHORIZED");
  expect((await context.cookies()).find((c) => c.name === "am_admin")).toBeUndefined();
});

test("session cookie is HttpOnly and SameSite=Strict, and logout clears it", async ({ page, context }) => {
  await page.goto("/admin/login");
  await page.getByLabel("Admin token").fill(env.adminToken);
  await page.getByRole("button", { name: "Log in" }).click();
  await expect(page).toHaveURL(/\/admin\/apis$/);

  const cookie = (await context.cookies()).find((c) => c.name === "am_admin");
  expect(cookie).toMatchObject({ httpOnly: true, sameSite: "Strict" });
  expect(await page.evaluate(() => document.cookie)).not.toContain("am_admin");

  await page.getByRole("button", { name: "Log out" }).click();
  await expect(page).toHaveURL(/\/admin\/login$/);
  expect((await context.cookies()).find((c) => c.name === "am_admin")).toBeUndefined();
});

test("a rejected session cookie is cleared and the user is sent to log in", async ({ page, context }) => {
  await context.addCookies([{ name: "am_admin", value: "stale-or-revoked", url: env.web, httpOnly: true, sameSite: "Strict" }]);
  await page.goto("/admin/apis");
  await expect(page).toHaveURL(/\/admin\/login\?expired=1$/);
  await expect(page.getByText("Your session ended")).toBeVisible();
  expect((await context.cookies()).find((c) => c.name === "am_admin")).toBeUndefined();
});
