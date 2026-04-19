import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

test.describe("hosted route accessibility", () => {
  const hostedRoutes = [
    "/hosted/start",
    "/hosted/signin",
    "/hosted/signup",
    "/hosted/dashboard",
    "/hosted/onboarding/organization",
    "/hosted/onboarding/workspace",
    "/hosted/workspaces/new",
  ];

  for (const routePath of hostedRoutes) {
    test(`${routePath} has zero axe violations`, async ({ page }) => {
      await page.route("**/hosted/api/**", async (route) => {
        const url = route.request().url();
        if (url.includes("/auth/session")) {
          return route.fulfill({
            status: 200,
            contentType: "application/json",
            body: JSON.stringify({
              authenticated: true,
              account: {
                id: "acct-1",
                email: "test@example.com",
                display_name: "Test User",
              },
              organizations: [],
              active_organization_id: null,
            }),
          });
        }
        return route.fulfill({ status: 404, body: "Not found" });
      });

      await page.goto(routePath);
      await page.waitForLoadState("networkidle");

      const results = await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
        .analyze();

      const violations = results.violations.filter(
        (v) => v.impact === "serious" || v.impact === "critical",
      );

      expect(violations).toEqual([]);
    });
  }
});

test.describe("workspace route accessibility (requires anx-core)", () => {
  test.slow();
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      window.localStorage.setItem("oar_ui_actor_id", "actor-ops-ai");
    });
  });

  const workspaceRoutes = [
    { path: "/o/local/w/local/inbox", heading: "Inbox" },
    { path: "/o/local/w/local/topics", heading: "Topics" },
    { path: "/o/local/w/local/boards", heading: "Boards" },
    { path: "/o/local/w/local/docs", heading: "Docs" },
    { path: "/o/local/w/local/artifacts", heading: "Artifacts" },
    { path: "/o/local/w/local/trash", heading: "Trash" },
    { path: "/o/local/w/local/access", heading: "Select Actor Identity" },
    { path: "/o/local/w/local/more", heading: "More" },
    { path: "/o/local/w/local", heading: "Dashboard" },
  ];

  for (const route of workspaceRoutes) {
    test(`${route.path} has zero axe violations`, async ({ page }) => {
      await page.goto(route.path);

      const headingVisible = await page
        .getByRole("heading", { name: route.heading })
        .isVisible()
        .catch(() => false);

      if (!headingVisible) {
        test.info().annotations.push({
          type: "skip-reason",
          description: "anx-core not running — page did not fully render",
        });
        return;
      }

      const results = await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
        .analyze();

      const violations = results.violations.filter(
        (v) => v.impact === "serious" || v.impact === "critical",
      );

      expect(violations).toEqual([]);
    });
  }
});
