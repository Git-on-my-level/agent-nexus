import { expect, test } from "@playwright/test";

const overview = {
  generated_at: "2026-05-20T02:00:00Z",
  telemetry_max_age_seconds: 300,
  organizations: {
    total: 3,
    by_status: { active: 2, suspended: 1 },
    by_access_mode: { read_write: 2, read_only: 1 },
    by_restriction_reason: { none: 2, quota: 1 },
    by_plan: { starter: 1, team: 2 },
  },
  accounts: {
    total: 9,
    by_status: { active: 8, disabled: 1 },
    by_recent_login_bucket: { "24h": 4, never: 2, older: 3 },
  },
  workspaces: {
    total: 5,
    by_status: { ready: 4, failed: 1 },
    by_access_mode: { read_write: 4, read_only: 1 },
    by_restriction_reason: { none: 4, quota: 1 },
    by_runtime_power_state: { running: 3, stopped: 1, unknown: 1 },
    by_host: { "host-a": 3, unknown: 2 },
    by_freshness: { fresh: 3, stale: 1, unknown: 1 },
  },
  heartbeat_health: { fresh: 3, stale: 1, unknown: 1 },
  usage_totals: {
    storage_bytes: 20 * 1024 * 1024,
    db_bytes: 5 * 1024 * 1024,
    blob_bytes: 15 * 1024 * 1024,
    artifact_count: 1200,
    document_count: 88,
    event_count: 9000,
    agent_count: 16,
    workspace_count: 5,
  },
  top_organizations: [
    {
      id: "org_alpha",
      slug: "alpha",
      display_name: "Alpha Ops",
      status: "active",
      access_mode: "read_write",
      plan_tier: "starter",
      effective_plan_tier: "team",
      storage_bytes: 18 * 1024 * 1024,
      artifact_count: 900,
      event_count: 7000,
      agent_count: 12,
      workspace_count: 3,
      stale_workspace_count: 1,
      last_activity_at: "2026-05-20T01:45:00Z",
    },
  ],
  recent_high_signal_events: [
    {
      id: "audit_1",
      event_type: "billing_webhook_failed",
      organization_id: "org_alpha",
      workspace_id: "",
      actor_account_id: "",
      occurred_at: "2026-05-20T01:59:00Z",
    },
  ],
  recent_operations: {
    provisioning: {
      recent_failure_count: 1,
      recent_change_count: 4,
      recent_jobs: [],
    },
    backups: { recent_failure_count: 0, recent_change_count: 2 },
    billing: { recent_failure_count: 1, recent_change_count: 1 },
    entitlements: { recent_failure_count: 0, recent_change_count: 1 },
  },
};

test.describe("hosted admin overview", () => {
  test("renders read-only overview from the admin analytics API", async ({
    page,
  }) => {
    await page.addInitScript(() => {
      window.localStorage.setItem("anx_admin_token", "admin-secret");
      window.localStorage.setItem("anx_admin_actor", "ops@example.com");
    });

    await page.route(
      "**/hosted/api/admin/analytics/overview",
      async (route) => {
        expect(route.request().headers()["x-anx-admin-token"]).toBe(
          "admin-secret",
        );
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ overview }),
        });
      },
    );

    await page.goto("/hosted/admin");

    await expect(
      page.getByRole("heading", { name: "Admin overview" }),
    ).toBeVisible();
    await expect(page.getByText("Heartbeat issues")).toBeVisible();
    await expect(page.getByText("1 stale / 1 unknown")).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "Top organizations" }),
    ).toBeVisible();
    await expect(page.getByRole("link", { name: /Alpha Ops/ })).toBeVisible();
    await expect(page.getByText("Billing webhook failed")).toBeVisible();
    await expect(page.getByText("Unknown telemetry").first()).toBeVisible();
  });

  test("shows unauthorized state when the admin token is rejected", async ({
    page,
  }) => {
    await page.addInitScript(() => {
      window.localStorage.setItem("anx_admin_token", "wrong-secret");
    });

    await page.route(
      "**/hosted/api/admin/analytics/overview",
      async (route) => {
        await route.fulfill({
          status: 401,
          contentType: "application/json",
          body: JSON.stringify({
            error: {
              code: "auth_required",
              message: "valid admin token is required",
            },
          }),
        });
      },
    );

    await page.goto("/hosted/admin");

    await expect(page.getByRole("alert")).toContainText(
      "valid admin token is required",
    );
  });
});
