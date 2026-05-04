import { expect, test } from "@playwright/test";

const orgId = "org_test_123";

/** Mirrors control-plane UsagePlan JSON for mocks (billing plan_usage_envelopes). */
function mockTierEnvelope(
  /** @type {"starter"|"team"|"scale"|"enterprise"} */
  tier,
) {
  const base = (
    /** @type {{ id: string; display: string; wl: number; cap: number; gb: number; bytes?: number }} */
    row,
  ) => ({
    id: row.id,
    display_name: row.display,
    workspace_limit: row.wl,
    max_artifacts_per_workspace: row.cap,
    artifact_capacity: row.cap,
    included_storage_gb: row.gb,
    included_storage_bytes:
      typeof row.bytes === "number" ? row.bytes : row.gb * (1024 * 1024 * 1024),
  });
  switch (tier) {
    case "starter":
      return base({
        id: "starter",
        display: "Free",
        wl: 1,
        cap: 1000,
        gb: 1,
        bytes: 256 * 1024 * 1024,
      });
    case "team":
      return base({
        id: "team",
        display: "Pro",
        wl: 5,
        cap: 125_000,
        gb: 25,
      });
    case "scale":
      return base({
        id: "scale",
        display: "Scale",
        wl: 25,
        cap: 2_500_000,
        gb: 250,
      });
    case "enterprise":
      return base({
        id: "enterprise",
        display: "Enterprise",
        wl: 100,
        cap: 100_000_000,
        gb: 1000,
      });
    default:
      throw new Error(tier);
  }
}

test.describe("hosted billing routes (mocked CP API)", () => {
  test.beforeEach(async ({ page }) => {
    await page.route("**/hosted/api/**", async (route) => {
      const url = route.request().url();
      if (url.includes("/account/me")) {
        return route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            account: {
              id: "acct_test_123",
              email: "test@example.com",
              display_name: "Test User",
            },
          }),
        });
      }
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
                  max_artifacts_per_workspace: 1000,
                  artifact_capacity: 1000,
                  included_storage_gb: 1,
                  included_storage_bytes: 256 * 1024 * 1024,
                },
                usage: {
                  workspace_count: 1,
                  artifact_count: 42,
                  storage_bytes: 9_400_000,
                  storage_gb: 1,
                  monthly_launch_count: 0,
                },
                quota: {
                  workspaces_remaining: 0,
                  artifacts_remaining: 958,
                  storage_bytes_remaining: 256 * 1024 * 1024 - 9_400_000,
                  storage_gb_remaining: 0,
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
              plan_usage_envelopes: {
                starter: mockTierEnvelope("starter"),
                team: mockTierEnvelope("team"),
                scale: mockTierEnvelope("scale"),
                enterprise: mockTierEnvelope("enterprise"),
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
                max_artifacts_per_workspace: 1000,
                artifact_capacity: 1000,
                included_storage_gb: 1,
                included_storage_bytes: 256 * 1024 * 1024,
              },
              usage: {
                workspace_count: 1,
                artifact_count: 42,
                storage_bytes: 9_400_000,
                storage_gb: 1,
                monthly_launch_count: 0,
              },
              quota: {
                workspaces_remaining: 0,
                artifacts_remaining: 958,
                storage_bytes_remaining: 256 * 1024 * 1024 - 9_400_000,
                storage_gb_remaining: 0,
              },
              workspaces: [
                {
                  id: "ws_test_123",
                  slug: "personal",
                  display_name: "Personal",
                  artifact_count: 42,
                  storage_bytes: 9_400_000,
                  storage_gb: 1,
                  monthly_launch_count: 0,
                  summary_stale: false,
                },
              ],
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
    await expect(page.getByText("9.0 MB / 256 MB")).toBeVisible();
    await expect(page.getByText("247 MB remaining")).toBeVisible();
  });

  test("billing page shows configuration panel when Stripe incomplete", async ({
    page,
  }) => {
    await page.goto(
      `/hosted/organizations/${encodeURIComponent(orgId)}/billing`,
    );
    await expect(page.getByRole("heading", { name: "Billing" })).toBeVisible();
    await expect(page.getByText("Billing not yet configured.")).toBeVisible();
    await expect(page.getByText("Up to 1 workspaces")).toBeVisible();
    await expect(page.getByText("1,000 artifacts included")).toBeVisible();
    await expect(page.getByText("125,000 artifacts included")).toBeVisible();
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
