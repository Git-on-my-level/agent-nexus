import { expect, test } from "@playwright/test";

const orgId = "org_test_123";

test.describe("hosted billing routes (mocked CP API)", () => {
  test.beforeEach(async ({ page }) => {
    await page.route("**/hosted/api/**", async (route) => {
      const url = route.request().url();
      if (
        url.includes(`/organizations/${orgId}/billing`) &&
        route.request().method() === "GET"
      ) {
        return route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            summary: {
              organization_id: orgId,
              plan_tier: "starter",
              billing_account: {
                organization_id: orgId,
                provider: "stripe",
                billing_status: "free",
                stripe_customer_id: "",
                stripe_subscription_id: "",
                stripe_price_id: "",
                stripe_subscription_status: "not_started",
                current_period_end: null,
                cancel_at_period_end: false,
                last_webhook_event_id: "",
                last_webhook_event_type: "",
                last_webhook_received_at: null,
                created_at: "2026-01-01T00:00:00Z",
                updated_at: "2026-01-01T00:00:00Z",
              },
              usage_summary: {
                organization_id: orgId,
                plan: {
                  id: "starter",
                  display_name: "Free",
                  workspace_limit: 1,
                  human_seat_limit: 1000,
                  included_storage_gb: 1,
                },
                usage: {
                  workspace_count: 0,
                  human_seat_count: 1,
                  storage_gb: 0,
                  monthly_launch_count: 0,
                },
                quota: {
                  workspaces_remaining: 1,
                  human_seats_remaining: 999,
                  storage_gb_remaining: 1,
                },
                workspaces: [],
              },
              configuration: {
                provider: "stripe",
                configured: false,
                publishable_key_configured: false,
                secret_key_configured: false,
                webhook_secret_configured: false,
                checkout_configured: false,
                customer_portal_configured: false,
                plan_price_ids: {},
                missing_configuration: ["stripe secret key"],
              },
            },
          }),
        });
      }
      if (url.includes(`/organizations/${orgId}/usage-summary`)) {
        return route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            summary: {
              organization_id: orgId,
              plan: {
                id: "starter",
                display_name: "Free",
                workspace_limit: 1,
                human_seat_limit: 1000,
                included_storage_gb: 1,
              },
              usage: {
                workspace_count: 0,
                human_seat_count: 1,
                storage_gb: 0,
                monthly_launch_count: 0,
              },
              quota: {
                workspaces_remaining: 1,
                human_seats_remaining: 999,
                storage_gb_remaining: 1,
              },
              workspaces: [],
            },
          }),
        });
      }
      if (url.includes("/organizations?")) {
        return route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            organizations: [
              {
                id: orgId,
                slug: "test",
                display_name: "Test Org",
                plan_tier: "starter",
                status: "active",
                created_at: "2026-01-01T00:00:00Z",
                updated_at: "2026-01-01T00:00:00Z",
              },
            ],
            next_cursor: "",
          }),
        });
      }
      if (url.includes("billing/checkout-session/")) {
        return route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ organization_id: orgId }),
        });
      }
      return route.fulfill({ status: 404, body: "{}" });
    });
  });

  test("organizations index lists org and links", async ({ page }) => {
    await page.goto("/hosted/organizations");
    await expect(
      page.getByRole("heading", { name: "Organizations" }),
    ).toBeVisible();
    await expect(page.getByRole("link", { name: "Billing" })).toBeVisible();
    await expect(page.getByRole("link", { name: "Usage" })).toBeVisible();
  });

  test("usage page renders meters", async ({ page }) => {
    await page.goto(`/hosted/organizations/${encodeURIComponent(orgId)}/usage`);
    await expect(page.getByRole("heading", { name: "Usage" })).toBeVisible();
    await expect(page.getByText("Free")).toBeVisible();
  });

  test("billing page shows configuration panel when Stripe incomplete", async ({
    page,
  }) => {
    await page.goto(
      `/hosted/organizations/${encodeURIComponent(orgId)}/billing`,
    );
    await expect(page.getByRole("heading", { name: "Billing" })).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Billing not yet configured" }),
    ).toBeVisible();
  });

  test("checkout return redirects toward org billing with activating", async ({
    page,
  }) => {
    await page.goto(
      `/hosted/billing/return?session_id=${encodeURIComponent("cs_test_1")}`,
    );
    await page.waitForURL(
      new RegExp(
        `/hosted/organizations/${orgId.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}/billing\\?activating=1`,
      ),
    );
  });
});
