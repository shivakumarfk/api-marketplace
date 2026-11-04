import { expect, test as setup } from "@playwright/test";
import { env } from "./env";

// Log in once through the real UI; the other specs reuse the session.
setup("log in as admin", async ({ page }) => {
  await page.goto("/admin/login");
  await page.getByLabel("Admin token").fill(env.adminToken);
  await page.getByRole("button", { name: "Log in" }).click();
  await expect(page).toHaveURL(/\/admin\/apis$/);
  await page.context().storageState({ path: "playwright/.auth/admin.json" });
});
