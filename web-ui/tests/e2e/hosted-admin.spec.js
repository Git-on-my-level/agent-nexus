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

const organization = {
  id: "org_alpha",
  slug: "alpha",
  display_name: "Alpha Ops",
  status: "active",
  access_mode: "read_write",
  plan_tier: "starter",
  plan_resolution: {
    effective_plan_tier: "team",
    source: "operator",
    quota: { storage_bytes: 104857600, workspace_count: 3 },
  },
  billing: {
    billing_status: "active",
    stripe_subscription_status: "trialing",
  },
  usage: {
    storage_bytes: 20 * 1024 * 1024,
    db_bytes: 5 * 1024 * 1024,
    blob_bytes: 15 * 1024 * 1024,
    artifact_count: 1200,
    document_count: 88,
    event_count: 9000,
    agent_count: 16,
    workspace_count: 2,
  },
  member_counts: { owner: 1, admin: 1 },
  workspace_counts: { total: 2, by_status: { ready: 2 } },
  recent_audit_events: [
    {
      id: "audit_org",
      event_type: "quota_enforcement_applied",
      occurred_at: "2026-05-20T01:00:00Z",
    },
  ],
  recent_provisioning_jobs: [],
  recent_backup_runs: [],
  last_usage_aggregation_at: "2026-05-20T01:30:00Z",
  created_at: "2026-05-19T00:00:00Z",
  updated_at: "2026-05-20T01:30:00Z",
};

const workspace = {
  id: "ws_alpha",
  organization_id: "org_alpha",
  organization_slug: "alpha",
  slug: "alpha-main",
  display_name: "Alpha Main",
  status: "ready",
  access_mode: "read_write",
  restriction_reason: "",
  host_id: "host_1",
  host_label: "packed-a",
  listen_port: 18100,
  container_id_short: "abcdef123456",
  runtime_image_tag: "anx-core:test",
  runtime_power_state: "running",
  heartbeat_freshness: "fresh",
  heartbeat_age_seconds: 12,
  heartbeat_version: "0.10.5",
  heartbeat_build: "build-a",
  last_activity_at: "2026-05-20T01:45:00Z",
  active_stream_count: 2,
  last_successful_backup_at: "2026-05-20T00:30:00Z",
  usage: organization.usage,
  health_summary: { database: "ok" },
  recent_jobs: [],
  recent_backup_runs: [],
  recent_audit_events: [],
  created_at: "2026-05-19T00:00:00Z",
  updated_at: "2026-05-20T01:30:00Z",
};

const account = {
  id: "acct_alpha",
  email: "operator@example.com",
  display_name: "Operator Example",
  status: "active",
  created_at: "2026-05-19T00:00:00Z",
  last_login_at: "2026-05-20T01:45:00Z",
  oauth_providers: ["google", "github"],
  active_session_count: 1,
  organization_memberships: [
    {
      organization_id: "org_alpha",
      organization_slug: "alpha",
      role: "owner",
      status: "active",
      created_at: "2026-05-19T00:00:00Z",
    },
  ],
  recent_audit_events: [
    {
      id: "audit_account",
      event_type: "workspace_session_exchanged",
      organization_id: "org_alpha",
      occurred_at: "2026-05-20T01:10:00Z",
    },
  ],
};

async function installAdminAnalyticsRoutes(page) {
  await page.route("**/hosted/api/admin/analytics/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    if (path.endsWith("/organizations/org_alpha")) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ organization }),
      });
      return;
    }
    if (path.endsWith("/workspaces/ws_alpha")) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ workspace }),
      });
      return;
    }
    if (path.endsWith("/accounts/acct_alpha")) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ account }),
      });
      return;
    }
    if (path.endsWith("/organizations")) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ organizations: [organization] }),
      });
      return;
    }
    if (path.endsWith("/workspaces")) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ workspaces: [workspace] }),
      });
      return;
    }
    if (path.endsWith("/accounts")) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ accounts: [account] }),
      });
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ overview }),
    });
  });
}

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

  test("renders org workspace and account drilldowns with redacted fields", async ({
    page,
  }) => {
    await page.addInitScript(() => {
      window.localStorage.setItem("anx_admin_token", "admin-secret");
      window.localStorage.setItem("anx_admin_actor", "ops@example.com");
    });
    await installAdminAnalyticsRoutes(page);

    await page.goto("/hosted/admin/organizations");
    await expect(
      page.getByRole("heading", { name: "Organizations" }),
    ).toBeVisible();
    await expect(page.getByRole("link", { name: /Alpha Ops/ })).toBeVisible();

    await page.goto("/hosted/admin/organizations/org_alpha");
    await expect(
      page.getByRole("heading", { name: "Alpha Ops" }),
    ).toBeVisible();
    await expect(page.getByText("Quota envelope")).toBeVisible();
    await expect(page.getByRole("link", { name: /Alpha Main/ })).toBeVisible();

    await page.goto("/hosted/admin/workspaces/ws_alpha");
    await expect(
      page.getByRole("heading", { name: "Alpha Main" }),
    ).toBeVisible();
    await expect(page.getByText("abcdef123456")).toBeVisible();
    await expect(page.getByText("database")).toBeVisible();

    await page.goto("/hosted/admin/accounts/acct_alpha");
    await expect(
      page.getByRole("heading", { name: "Operator Example" }),
    ).toBeVisible();
    await expect(page.getByText("Provider subject identifiers")).toBeVisible();
    await expect(page.getByText("google-sub-secret")).toHaveCount(0);
  });
});
