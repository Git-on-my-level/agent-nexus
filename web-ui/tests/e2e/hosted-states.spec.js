import { expect, test } from "@playwright/test";

import { AUDIT_VIEWPORTS, expectCleanLayout } from "../helpers/layoutAudit.js";

/**
 * Walks the customer-facing hosted pages (everything under /hosted except
 * /hosted/admin) through every UI state they can reach — loading, empty,
 * populated, each section's error, in-flight submit, submit error, success
 * banners, the header menus — at several viewport sizes, running the geometry
 * audit after each transition.
 *
 * Fixture data is deliberately hostile: long unbroken organization names,
 * long email addresses, long error messages and many rows.
 */

const PRIMARY_ORG_ID = "org_hosted_primary";
const SECOND_ORG_ID = "org_hosted_second";

/** No spaces and no hyphens: zero soft-wrap opportunities. */
const LONG_UNBROKEN_NAME =
  "NorthwindAutonomyInterplanetaryRoboticsAndLogisticsDivisionHoldings";
const LONG_UNBROKEN_SLUG =
  "northwindautonomyinterplanetaryroboticslogisticsdivisionholdings";
const LONG_EMAIL =
  "alexandrakatherinemontgomerybergstrom@verylongcorporatesubdomainexample.com";
const LONG_ERROR =
  "The control plane rejected this request because the organization exceeded its provisioning quota. ".repeat(
    3,
  );

function deferred() {
  let resolve;
  const promise = new Promise((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

const ACCOUNT = {
  id: "acct_hosted_jordan",
  email: LONG_EMAIL,
  display_name: "Jordan Kim",
};

function org(overrides) {
  return {
    slug: "northwind",
    display_name: "Northwind Autonomy",
    plan_tier: "team",
    status: "active",
    access_mode: "read_write",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    ...overrides,
  };
}

const ORGS = [
  org({
    id: PRIMARY_ORG_ID,
    slug: LONG_UNBROKEN_SLUG,
    display_name: LONG_UNBROKEN_NAME,
  }),
  org({
    id: SECOND_ORG_ID,
    slug: "solstice-ops",
    display_name: "Solstice Ops",
    plan_tier: "starter",
  }),
];

function workspace(overrides) {
  return {
    organization_id: PRIMARY_ORG_ID,
    status: "ready",
    created_at: "2026-02-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    ...overrides,
  };
}

const WORKSPACES = [
  workspace({
    id: "ws_hosted_orbit",
    slug: "orbit",
    display_name: "Orbit Release",
  }),
  workspace({
    id: "ws_hosted_long",
    slug: `${LONG_UNBROKEN_SLUG}-staging`,
    display_name: `${LONG_UNBROKEN_NAME} Staging Fleet`,
    status: "provisioning",
  }),
  workspace({
    id: "ws_hosted_failed",
    slug: "failed-bootstrap",
    display_name: "Failed bootstrap",
    status: "failed",
  }),
  workspace({
    id: "ws_hosted_suspended",
    slug: "suspended",
    display_name: "Suspended workspace",
    status: "suspended",
  }),
];

function membership(overrides) {
  return {
    organization_id: PRIMARY_ORG_ID,
    role: "member",
    status: "active",
    ...overrides,
  };
}

const OWNER_MEMBERSHIP = membership({
  id: "mem_owner",
  account_id: ACCOUNT.id,
  role: "owner",
  account_display_name: ACCOUNT.display_name,
  account_email: ACCOUNT.email,
});

const MEMBERSHIPS = [
  OWNER_MEMBERSHIP,
  membership({
    id: "mem_long",
    account_id: "acct_long",
    role: "admin",
    account_display_name: LONG_UNBROKEN_NAME,
    account_email: `admin.${LONG_EMAIL}`,
  }),
  membership({
    id: "mem_no_name",
    account_id: "acct_no_name",
    role: "viewer",
    account_email: `viewer.${LONG_EMAIL}`,
  }),
  membership({
    id: "mem_id_only",
    account_id: `acct_${"0123456789abcdef".repeat(4)}`,
    role: "member",
  }),
  membership({
    id: "mem_removed",
    account_id: "acct_removed",
    role: "member",
    status: "disabled",
    account_display_name: "Removed Person",
    account_email: "removed@example.com",
  }),
];

const INVITES = [
  {
    id: "inv_pending_long",
    email: `pending.${LONG_EMAIL}`,
    role: "member",
    status: "pending",
  },
  {
    id: "inv_pending_admin",
    email: "ops@example.com",
    role: "admin",
    status: "pending",
  },
  {
    id: "inv_accepted",
    email: "gone@example.com",
    role: "member",
    status: "accepted",
  },
];

function planEnvelope(id, displayName, wl, cap, gb) {
  return {
    id,
    display_name: displayName,
    workspace_limit: wl,
    max_artifacts_per_workspace: cap,
    artifact_capacity: cap,
    included_storage_gb: gb,
    included_storage_bytes: gb * 1024 ** 3,
  };
}

function billingSummary(overrides = {}) {
  return {
    organization_id: PRIMARY_ORG_ID,
    plan_tier: "team",
    billing_account: {
      organization_id: PRIMARY_ORG_ID,
      provider: "stripe",
      billing_status: "active",
      stripe_customer_id: "cus_hosted",
      stripe_subscription_id: "sub_hosted",
      stripe_price_id: "price_hosted",
      stripe_subscription_status: "active",
      current_period_end: "2026-05-18T00:00:00Z",
      cancel_at_period_end: false,
      last_webhook_event_id: "evt_hosted",
      last_webhook_event_type: "customer.subscription.updated",
      last_webhook_received_at: "2026-04-01T00:00:00Z",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-04-01T00:00:00Z",
    },
    usage_summary: usageSummary(),
    configuration: {
      provider: "stripe",
      configured: true,
      publishable_key_configured: true,
      secret_key_configured: true,
      webhook_secret_configured: true,
      checkout_configured: true,
      customer_portal_configured: true,
      plan_price_ids: {},
      missing_configuration: [],
    },
    plan_usage_envelopes: {
      starter: planEnvelope("starter", "Free", 1, 1000, 1),
      team: planEnvelope("team", "Pro", 5, 125_000, 25),
      scale: planEnvelope("scale", "Scale", 25, 2_500_000, 250),
      enterprise: planEnvelope(
        "enterprise",
        "Enterprise",
        100,
        100_000_000,
        1000,
      ),
    },
    ...overrides,
  };
}

function usageSummary(overrides = {}) {
  return {
    organization_id: PRIMARY_ORG_ID,
    plan: planEnvelope("team", "Pro", 5, 125_000, 25),
    usage: {
      workspace_count: 4,
      artifact_count: 384,
      storage_bytes: Math.floor(7.4 * 1024 ** 3),
      storage_gb: 8,
      monthly_launch_count: 118,
    },
    quota: {
      workspaces_remaining: 1,
      artifacts_remaining: 124_616,
      storage_bytes_remaining: Math.floor(17.6 * 1024 ** 3),
      storage_gb_remaining: 17,
    },
    workspaces: WORKSPACES.map((ws, i) => ({
      id: ws.id,
      slug: ws.slug,
      display_name: ws.display_name,
      status: ws.status,
      access_mode: i === 3 ? "read_only" : "read_write",
      artifact_count: [164, 102, 118, 0][i],
      storage_gb: [4, 2, 3, 0][i],
      storage_bytes: [4, 2, 3, 0][i] * 1024 ** 3,
      monthly_launch_count: [42, 38, 38, 0][i],
      last_active_at: "2026-04-01T00:00:00Z",
      summary_stale: false,
    })),
    ...overrides,
  };
}

/** Over-quota numbers: meters must not paint past 100% or spill their track. */
function overQuotaUsageSummary() {
  return usageSummary({
    plan: planEnvelope("starter", "Free", 1, 1000, 1),
    usage: {
      workspace_count: 4,
      artifact_count: 1_284_004,
      storage_bytes: Math.floor(9.75 * 1024 ** 3),
      storage_gb: 10,
      monthly_launch_count: 4210,
    },
    quota: {
      workspaces_remaining: 0,
      artifacts_remaining: 0,
      storage_bytes_remaining: 0,
      storage_gb_remaining: 0,
    },
  });
}

/**
 * Installs a mutable mock of the hosted control-plane proxy (`/hosted/api/*`).
 * Each endpoint reads its behavior from `api` at request time so tests can flip
 * a field and then drive the UI:
 *   - `hold.<name>`: a deferred the response waits on (in-flight states)
 *   - `fail.<name>`: `{ status?, message? }` respond with an error body
 */
async function installHostedApi(page, overrides = {}) {
  const api = {
    account: ACCOUNT,
    organizations: ORGS,
    workspaces: WORKSPACES,
    memberships: MEMBERSHIPS,
    invites: INVITES,
    billing: billingSummary(),
    usage: usageSummary(),
    inviteUrl: `https://app.example.com/hosted/signup?invite=${"a1b2c3d4".repeat(8)}`,
    checkoutSession: { status: "created", url: "/hosted/billing/mock-portal" },
    portalSession: { status: "created", url: "/hosted/billing/mock-portal" },
    hold: {},
    fail: {},
    calls: [],
    ...overrides,
  };

  const json = (route, status, body) =>
    route.fulfill({
      status,
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body),
    });

  async function respond(route, name, okBody, okStatus = 200) {
    api.calls.push(name);
    if (api.hold[name]) await api.hold[name].promise;
    const failure = api.fail[name];
    if (failure) {
      return json(route, failure.status ?? 500, {
        error: {
          code: failure.code ?? "test_failure",
          message: failure.message ?? "request failed",
          details: failure.message ?? "request failed",
        },
      });
    }
    return json(
      route,
      okStatus,
      typeof okBody === "function" ? okBody() : okBody,
    );
  }

  await page.addInitScript((orgId) => {
    localStorage.setItem("anx_hosted_active_org_id", orgId);
  }, PRIMARY_ORG_ID);

  await page.route("**/hosted/api/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname.replace(/^.*\/hosted\/api\//, "");
    const method = request.method();

    if (path.startsWith("account/me")) {
      return respond(route, "accountMe", () => ({ account: api.account }));
    }
    if (path.startsWith("account/sessions")) {
      return respond(route, "signOut", { ok: true });
    }
    if (path === "session") {
      return respond(route, "sessionCookie", { ok: true });
    }
    if (path.startsWith("workspaces")) {
      const launch = path.match(/^workspaces\/([^/?]+)\/launch-sessions/);
      if (launch) {
        return respond(route, "launchSession", () => ({
          launch_session: { finish_url: "/hosted/billing/mock-portal" },
        }));
      }
      if (method === "POST") {
        return respond(route, "createWorkspace", () => ({
          workspace: workspace({
            id: "ws_hosted_created",
            slug: "created",
            display_name: "Created workspace",
          }),
        }));
      }
      const orgFilter = url.searchParams.get("organization_id") ?? "";
      return respond(route, "workspaces", () => ({
        workspaces: api.workspaces.filter(
          (ws) => !orgFilter || String(ws.organization_id) === orgFilter,
        ),
        next_cursor: "",
      }));
    }
    if (path.startsWith("billing/checkout-session/")) {
      return respond(route, "checkoutReturn", () => ({
        organization_id: PRIMARY_ORG_ID,
      }));
    }
    if (path.startsWith("mcp/oauth/browser/authorize")) {
      return respond(route, "mcpAuthorize", () => ({
        redirect_url: "/hosted/billing/mock-portal",
      }));
    }

    const orgScoped = path.match(/^organizations\/([^/?]+)(?:\/([^?]*))?/);
    if (orgScoped) {
      const orgId = decodeURIComponent(orgScoped[1]);
      const rest = (orgScoped[2] ?? "").replace(/\/$/, "");
      if (rest === "" && method === "GET") {
        return respond(route, "organization", () => {
          const found = api.organizations.find((o) => String(o.id) === orgId);
          return { organization: found ?? api.organizations[0] };
        });
      }
      if (rest === "memberships") {
        return respond(route, "memberships", () => ({
          memberships: api.memberships,
        }));
      }
      if (/^memberships\//.test(rest)) {
        return respond(route, "updateMembership", () => {
          const id = rest.split("/")[1];
          const patch = request.postDataJSON() ?? {};
          api.memberships = api.memberships.map((m) =>
            m.id === id ? { ...m, ...patch } : m,
          );
          return { ok: true };
        });
      }
      if (rest === "invites" && method === "POST") {
        return respond(route, "createInvite", () => {
          const body = request.postDataJSON() ?? {};
          api.invites = [
            {
              id: `inv_created_${api.invites.length}`,
              email: body.email,
              role: body.role,
              status: "pending",
            },
            ...api.invites,
          ];
          return { invite: api.invites[0], invite_url: api.inviteUrl };
        });
      }
      if (rest === "invites") {
        return respond(route, "invites", () => ({ invites: api.invites }));
      }
      if (/^invites\/.*\/revoke$/.test(rest)) {
        return respond(route, "revokeInvite", () => {
          const id = rest.split("/")[1];
          api.invites = api.invites.map((inv) =>
            inv.id === id ? { ...inv, status: "revoked" } : inv,
          );
          return { ok: true };
        });
      }
      if (rest === "usage-summary") {
        return respond(route, "usageSummary", () => ({ summary: api.usage }));
      }
      if (rest === "billing") {
        return respond(route, "billing", () => ({ summary: api.billing }));
      }
      if (rest === "billing/checkout-session") {
        return respond(route, "checkoutSession", () => ({
          session: api.checkoutSession,
        }));
      }
      if (rest === "billing/customer-portal-session") {
        return respond(route, "portalSession", () => ({
          session: api.portalSession,
        }));
      }
      if (rest === "billing/mock-checkout-complete") {
        return respond(route, "mockCheckout", { ok: true });
      }
      if (rest === "deactivate") {
        return respond(route, "deactivate", () => {
          api.organizations = api.organizations.map((o) =>
            String(o.id) === orgId
              ? {
                  ...o,
                  status: "suspended",
                  restriction_reason: "decommission",
                }
              : o,
          );
          return {
            organization: api.organizations.find((o) => String(o.id) === orgId),
          };
        });
      }
    }

    if (path.startsWith("organizations")) {
      if (method === "POST") {
        return respond(route, "createOrganization", () => ({
          organization: org({ id: "org_hosted_new", slug: "new-org" }),
        }));
      }
      return respond(route, "organizations", () => ({
        organizations: api.organizations,
        next_cursor: "",
      }));
    }

    return json(route, 404, { error: { message: "not found" } });
  });

  return api;
}

const bothEnds = { scrollPositions: ["top", "bottom"] };

/**
 * These pages fetch the session, the organization and its workspaces before
 * they paint anything, and the shared dev server is slow under load — give the
 * first assertion after a navigation room instead of racing it.
 */
const firstPaint = { timeout: 15_000 };

/**
 * The dev server streams SSR markup before the client module graph finishes
 * loading, so a click can land on markup that has no handlers yet. Svelte's
 * devtools hook (`window.__svelte`) is installed by the client runtime, so it
 * marks the point where the page is actually interactive.
 */
async function waitForHydration(page) {
  await page
    .waitForFunction(() => "__svelte" in window, null, { timeout: 20_000 })
    .catch(() => {});
}

async function gotoHosted(page, path) {
  await page.goto(path);
  await waitForHydration(page);
}

for (const viewport of AUDIT_VIEWPORTS) {
  test.describe(`hosted pages @ ${viewport.name}`, () => {
    test.use({
      viewport: { width: viewport.width, height: viewport.height },
      permissions: ["clipboard-read", "clipboard-write"],
    });

    test("public sign-in and sign-up pages", async ({ page }) => {
      await installHostedApi(page, {
        fail: { accountMe: { status: 401 }, organizations: { status: 401 } },
      });
      await gotoHosted(page, "/hosted/signin");
      await expect(
        page.getByRole("heading", { name: "Welcome back" }),
      ).toBeVisible();
      await expectCleanLayout(page, "signin", bothEnds);

      await page.getByRole("link", { name: "Create an account" }).click();
      await expect(
        page.getByRole("heading", { name: "Create your account" }),
      ).toBeVisible({ timeout: 15_000 });
      await waitForHydration(page);
      await expectCleanLayout(page, "signup", bothEnds);

      // A pasted invite code is one long unbroken token.
      const inviteField = page.getByLabel("Organization invite (optional)");
      await inviteField.fill("oinv_".concat("a1b2c3d4".repeat(10)));
      await expect(inviteField).toHaveValue(/^oinv_a1b2/);
      await expectCleanLayout(page, "signup with long invite token");

      await page.route("**/hosted/api/account/oauth/**", (route) =>
        route.fulfill({
          status: 500,
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ error: { message: LONG_ERROR } }),
        }),
      );
      // The dev server can still be wiring the page module up; retry the click
      // until the handler is live.
      await expect(async () => {
        await page
          .getByRole("button", { name: "Continue with Google" })
          .click({ timeout: 3000 });
        await expect(page.getByRole("alert")).toBeVisible({ timeout: 3000 });
      }).toPass({ timeout: 20_000 });
      await expectCleanLayout(page, "signup oauth start failed", bothEnds);
    });

    test("header org switcher and account menu", async ({ page }) => {
      const manyOrgs = [
        ...ORGS,
        ...Array.from({ length: 10 }, (_, i) =>
          org({
            id: `org_hosted_extra_${i}`,
            slug: `${LONG_UNBROKEN_SLUG}-${i}`,
            display_name: `${LONG_UNBROKEN_NAME} ${i}`,
            plan_tier: i % 2 ? "scale" : "starter",
          }),
        ),
      ];
      await installHostedApi(page, { organizations: manyOrgs });
      await gotoHosted(page, "/hosted/dashboard");
      await expect(
        page.getByRole("heading", { level: 1, name: LONG_UNBROKEN_NAME }),
      ).toBeVisible({ timeout: 15_000 });
      await expectCleanLayout(page, "dashboard chrome", bothEnds);

      const orgButton = page.locator("header button[aria-haspopup='listbox']");
      const accountButton = page.locator("header button[aria-haspopup='menu']");

      await orgButton.click();
      await expect(page.getByRole("listbox")).toBeVisible();
      await expectCleanLayout(page, "org switcher open");

      await page.keyboard.press("Escape");
      await expect(page.getByRole("listbox")).toHaveCount(0);
      await accountButton.click();
      await expect(page.getByRole("menu")).toBeVisible();
      await expectCleanLayout(page, "account menu open");

      // The two popovers overlap and neither is modal, so opening one must
      // close the other instead of stacking on top of it.
      await orgButton.click();
      await expect(page.getByRole("listbox")).toBeVisible();
      await expect(page.getByRole("menu")).toHaveCount(0);
      await expectCleanLayout(page, "account menu replaced by org switcher");
    });

    test("dashboard loading, error, empty and populated", async ({ page }) => {
      const api = await installHostedApi(page);
      api.hold.workspaces = deferred();
      api.hold.usageSummary = deferred();
      await gotoHosted(page, "/hosted/dashboard");
      await expect(
        page.getByRole("heading", { name: "Workspaces" }),
      ).toBeVisible(firstPaint);
      await expectCleanLayout(page, "dashboard loading");

      api.hold.usageSummary.resolve();
      await expectCleanLayout(
        page,
        "dashboard usage loaded, workspaces pending",
      );
      api.hold.workspaces.resolve();
      api.hold = {};
      await expect(page.getByText("Orbit Release")).toBeVisible();
      await expectCleanLayout(page, "dashboard populated", bothEnds);

      api.fail.usageSummary = { message: LONG_ERROR };
      api.fail.workspaces = { message: LONG_ERROR };
      await page.reload();
      await waitForHydration(page);
      await expect(page.getByText("Usage didn't load")).toBeVisible(firstPaint);
      await expectCleanLayout(page, "dashboard sections failed", bothEnds);

      api.fail = {};
      api.workspaces = [];
      await page.reload();
      await waitForHydration(page);
      await expect(
        page.getByRole("heading", { name: "Spin up your first workspace" }),
      ).toBeVisible(firstPaint);
      await expectCleanLayout(page, "dashboard no workspaces", bothEnds);
    });

    test("dashboard over-quota meters and launch failure", async ({ page }) => {
      const api = await installHostedApi(page, {
        usage: overQuotaUsageSummary(),
      });
      await gotoHosted(page, "/hosted/dashboard");
      await expect(page.getByText("Orbit Release")).toBeVisible(firstPaint);
      await expectCleanLayout(page, "dashboard over quota", bothEnds);

      api.fail.launchSession = { message: LONG_ERROR };
      await page.getByRole("button", { name: "Open" }).first().click();
      await expect(page.getByRole("alert")).toBeVisible();
      await expectCleanLayout(page, "dashboard launch failed", bothEnds);
    });

    test("dashboard created-workspace notice and no organizations", async ({
      page,
    }) => {
      const api = await installHostedApi(page);
      await gotoHosted(
        page,
        `/hosted/dashboard?organization_id=${PRIMARY_ORG_ID}` +
          `&created_workspace_id=ws_hosted_long` +
          `&created_workspace=${encodeURIComponent(LONG_UNBROKEN_NAME)}`,
      );
      await expect(page.getByText(/created$/).first()).toBeVisible(firstPaint);
      await expectCleanLayout(
        page,
        "dashboard created workspace notice",
        bothEnds,
      );

      api.organizations = [];
      await page.reload();
      await waitForHydration(page);
      await expect(
        page.getByRole("heading", { name: "Create your first organization" }),
      ).toBeVisible(firstPaint);
      await expectCleanLayout(
        page,
        "dashboard without organizations",
        bothEnds,
      );
    });

    test("organizations list states", async ({ page }) => {
      const api = await installHostedApi(page);
      api.hold.organizations = deferred();
      await gotoHosted(page, "/hosted/organizations?billing_error=1");
      await expect(
        page.getByRole("heading", { name: "Organizations" }),
      ).toBeVisible();
      await expectCleanLayout(page, "organizations loading");

      api.hold.organizations.resolve();
      api.hold = {};
      await expect(page.getByText(LONG_UNBROKEN_NAME).first()).toBeVisible(
        firstPaint,
      );
      await expectCleanLayout(page, "organizations populated", bothEnds);

      api.organizations = [
        org({
          id: "org_deactivated",
          slug: LONG_UNBROKEN_SLUG,
          display_name: LONG_UNBROKEN_NAME,
          status: "suspended",
          plan_tier: "enterprise",
        }),
        ...Array.from({ length: 12 }, (_, i) =>
          org({
            id: `org_row_${i}`,
            slug: `row-${i}`,
            display_name: `Row ${i}`,
          }),
        ),
      ];
      await page.reload();
      await waitForHydration(page);
      await expect(page.getByText("Deactivated")).toBeVisible(firstPaint);
      await expectCleanLayout(
        page,
        "organizations with deactivated row",
        bothEnds,
      );

      api.fail.organizations = { message: LONG_ERROR };
      await page.reload();
      await waitForHydration(page);
      await expect(page.getByRole("alert").first()).toBeVisible();
      await expectCleanLayout(page, "organizations failed", bothEnds);

      api.fail = {};
      api.organizations = [];
      await page.reload();
      await waitForHydration(page);
      await expect(
        page.getByRole("heading", { name: "No organizations yet" }),
      ).toBeVisible(firstPaint);
      await expectCleanLayout(page, "organizations empty", bothEnds);
    });

    test("create organization form", async ({ page }) => {
      const api = await installHostedApi(page);
      await gotoHosted(page, "/hosted/organizations/new");
      await expect(
        page.getByRole("heading", { name: "Create an organization" }),
      ).toBeVisible();
      await expectCleanLayout(page, "new organization form", bothEnds);

      await page.getByLabel("Organization name").fill(LONG_UNBROKEN_NAME);
      // Slug mirrors the name only once the client is live.
      await expect(page.getByLabel("URL slug")).not.toHaveValue("");
      await expectCleanLayout(page, "new organization long slug", bothEnds);

      api.fail.createOrganization = { message: LONG_ERROR };
      await page.getByRole("button", { name: "Create organization" }).click();
      await expect(page.getByRole("alert")).toBeVisible();
      await expectCleanLayout(page, "new organization failed", bothEnds);
    });

    test("team page: member view, invites and role changes", async ({
      page,
    }) => {
      page.on("dialog", (dialog) => dialog.accept());
      const api = await installHostedApi(page);
      api.hold.memberships = deferred();
      await gotoHosted(page, `/hosted/organizations/${PRIMARY_ORG_ID}/team`);
      await expect(
        page.getByRole("heading", { name: "Team & invites" }),
      ).toBeVisible();
      await expectCleanLayout(page, "team loading");

      api.hold.memberships.resolve();
      api.hold = {};
      await expect(
        page.getByRole("heading", { name: "Invite by email" }),
      ).toBeVisible(firstPaint);
      await expectCleanLayout(page, "team populated", bothEnds);

      api.fail.createInvite = { message: LONG_ERROR };
      await page.getByRole("button", { name: "Send invite" }).click();
      await expect(page.getByText("Email is required.")).toBeVisible();
      await expectCleanLayout(page, "team invite validation error");

      await page.getByLabel("Email", { exact: true }).fill(LONG_EMAIL);
      await page.getByRole("button", { name: "Send invite" }).click();
      await expect(page.getByRole("alert")).toBeVisible();
      await expectCleanLayout(page, "team invite failed", bothEnds);

      api.fail = {};
      api.hold.createInvite = deferred();
      await page.getByRole("button", { name: "Send invite" }).click();
      await expectCleanLayout(page, "team invite in flight");
      api.hold.createInvite.resolve();
      api.hold = {};
      await expect(page.getByText(LONG_EMAIL, { exact: true })).toHaveCount(2);
      await expectCleanLayout(page, "team invite created", bothEnds);

      // Role change on another member reloads the list in place.
      const adminRow = page
        .getByRole("listitem")
        .filter({ hasText: LONG_UNBROKEN_NAME });
      await adminRow.getByRole("combobox").selectOption("viewer");
      await expect(adminRow.getByRole("combobox")).toHaveValue("viewer");
      await expectCleanLayout(page, "team role changed", bothEnds);

      api.fail.updateMembership = { message: LONG_ERROR };
      await adminRow.getByRole("button", { name: "Remove" }).click();
      await expect(page.getByRole("status")).toBeVisible();
      await expectCleanLayout(page, "team remove failed", bothEnds);

      api.fail.revokeInvite = { message: LONG_ERROR };
      await page.getByRole("button", { name: "Revoke" }).first().click();
      await expect(page.getByRole("status")).toBeVisible();
      await expectCleanLayout(page, "team revoke failed", bothEnds);
    });

    test("team page without manage rights and empty members", async ({
      page,
    }) => {
      const api = await installHostedApi(page, {
        memberships: [
          membership({
            id: "mem_self_viewer",
            account_id: ACCOUNT.id,
            role: "viewer",
            account_display_name: ACCOUNT.display_name,
            account_email: ACCOUNT.email,
          }),
          ...MEMBERSHIPS.slice(1),
        ],
      });
      await gotoHosted(page, `/hosted/organizations/${PRIMARY_ORG_ID}/team`);
      await expect(page.getByText(/can send invites/)).toBeVisible(firstPaint);
      await expectCleanLayout(page, "team read-only view", bothEnds);

      api.memberships = [];
      api.invites = [];
      await page.reload();
      await waitForHydration(page);
      await expect(
        page.getByRole("heading", { name: "No members listed" }),
      ).toBeVisible(firstPaint);
      await expectCleanLayout(page, "team empty", bothEnds);

      api.fail.memberships = { message: LONG_ERROR };
      await page.reload();
      await waitForHydration(page);
      await expect(page.getByRole("alert").first()).toBeVisible();
      await expectCleanLayout(page, "team load failed", bothEnds);
    });

    test("billing page: manager view, warnings and plan cards", async ({
      page,
    }) => {
      const api = await installHostedApi(page);
      api.hold.billing = deferred();
      await gotoHosted(page, `/hosted/organizations/${PRIMARY_ORG_ID}/billing`);
      await expect(
        page.getByRole("heading", { name: "Billing & Usage" }),
      ).toBeVisible();
      await expectCleanLayout(page, "billing loading");

      api.hold.billing.resolve();
      api.hold = {};
      await expect(
        page.getByRole("heading", { name: "Workspace usage" }),
      ).toBeVisible(firstPaint);
      await expectCleanLayout(page, "billing populated", bothEnds);

      api.fail.portalSession = { message: LONG_ERROR };
      await page.getByRole("button", { name: "Manage in Stripe" }).click();
      await expect(page.getByRole("alert").first()).toBeVisible();
      await expectCleanLayout(page, "billing portal failed", bothEnds);

      // Unconfigured Stripe: warning strip above the plan grid.
      api.fail = {};
      api.billing = billingSummary({
        plan_tier: "starter",
        billing_account: {
          ...billingSummary().billing_account,
          stripe_subscription_id: "",
          stripe_subscription_status: "past_due",
        },
        configuration: {
          ...billingSummary().configuration,
          configured: false,
          missing_configuration: ["stripe secret key", "webhook secret"],
        },
      });
      await page.reload();
      await waitForHydration(page);
      await expect(page.getByText("Billing not yet configured.")).toBeVisible(
        firstPaint,
      );
      await expectCleanLayout(page, "billing not configured", bothEnds);

      api.fail.checkoutSession = { message: LONG_ERROR };
      await page
        .getByRole("button", { name: /^Upgrade to/ })
        .first()
        .click();
      await expect(page.getByRole("alert").first()).toBeVisible();
      await expectCleanLayout(page, "billing checkout failed", bothEnds);
    });

    test("billing page: public beta, member role and load error", async ({
      page,
    }) => {
      const api = await installHostedApi(page, {
        billing: billingSummary({
          configuration: {
            ...billingSummary().configuration,
            public_beta_no_paid_upgrades: true,
          },
        }),
      });
      await gotoHosted(page, `/hosted/organizations/${PRIMARY_ORG_ID}/billing`);
      await expect(
        page.getByText("Public beta — no live payments"),
      ).toBeVisible(firstPaint);
      await expectCleanLayout(page, "billing public beta", bothEnds);

      api.fail.billing = { status: 403, message: "forbidden" };
      await page.reload();
      await waitForHydration(page);
      await expect(
        page.getByRole("heading", { name: "Who manages billing" }),
      ).toBeVisible(firstPaint);
      await expectCleanLayout(page, "billing member view", bothEnds);

      api.memberships = [];
      await page.reload();
      await waitForHydration(page);
      await expect(page.getByText("No active managers listed.")).toBeVisible(
        firstPaint,
      );
      await expectCleanLayout(page, "billing member view without managers");

      api.fail.billing = { status: 500, message: LONG_ERROR };
      await page.reload();
      await waitForHydration(page);
      await expect(page.getByRole("alert").first()).toBeVisible();
      await expectCleanLayout(page, "billing load failed", bothEnds);
    });

    test("billing activation banner after checkout return", async ({
      page,
    }) => {
      await installHostedApi(page);
      await page.addInitScript(
        ({ orgId, snapshot }) => {
          sessionStorage.setItem(
            `anx_billing_snapshot_${orgId}`,
            JSON.stringify({ ...snapshot, ts: Date.now() }),
          );
        },
        {
          orgId: PRIMARY_ORG_ID,
          // Same tier/status as the summary: the page keeps polling and shows
          // the "Activating your subscription…" strip.
          snapshot: { plan_tier: "team", stripe_subscription_status: "active" },
        },
      );
      await gotoHosted(
        page,
        `/hosted/organizations/${PRIMARY_ORG_ID}/billing?activating=1`,
      );
      await expect(page.getByText("Activating your subscription…")).toBeVisible(
        firstPaint,
      );
      await expectCleanLayout(page, "billing activating banner", bothEnds);
    });

    test("organization settings and deactivation", async ({ page }) => {
      const api = await installHostedApi(page);
      api.hold.organization = deferred();
      await gotoHosted(
        page,
        `/hosted/organizations/${PRIMARY_ORG_ID}/settings`,
      );
      await expect(
        page.getByRole("heading", { name: "Organization settings" }),
      ).toBeVisible();
      await expectCleanLayout(page, "settings loading");

      api.hold.organization.resolve();
      api.hold = {};
      await expect(
        page.getByRole("heading", { name: "Deactivate organization" }),
      ).toBeVisible(firstPaint);
      await expectCleanLayout(page, "settings populated", bothEnds);

      const confirmField = page.getByLabel(/^Type /);
      await confirmField.fill("wrong");
      await expectCleanLayout(page, "settings confirmation mismatch", bothEnds);

      await confirmField.fill(LONG_UNBROKEN_NAME);
      api.fail.deactivate = { message: LONG_ERROR };
      await page
        .getByRole("button", { name: "Deactivate organization" })
        .click();
      await expect(
        page.getByText(/control plane rejected/).first(),
      ).toBeVisible();
      await expectCleanLayout(page, "settings deactivate failed", bothEnds);

      api.fail = {};
      await confirmField.fill(LONG_UNBROKEN_NAME);
      await page
        .getByRole("button", { name: "Deactivate organization" })
        .click();
      await expect(
        page.getByText("This organization is already deactivated."),
      ).toBeVisible();
      await expectCleanLayout(page, "settings deactivated", bothEnds);

      api.fail.organization = { message: LONG_ERROR };
      await page.reload();
      await waitForHydration(page);
      await expect(page.getByRole("alert").first()).toBeVisible();
      await expectCleanLayout(page, "settings load failed", bothEnds);
    });

    test("create workspace form and provisioning failure", async ({ page }) => {
      const api = await installHostedApi(page, {
        organizations: [
          org({
            id: PRIMARY_ORG_ID,
            slug: LONG_UNBROKEN_SLUG,
            display_name: LONG_UNBROKEN_NAME,
            flags: { allow_byo_service_identity: true },
          }),
        ],
      });
      await gotoHosted(page, "/hosted/workspaces/new");
      await expect(
        page.getByRole("heading", { name: "Create a workspace" }),
      ).toBeVisible();
      await expectCleanLayout(page, "new workspace form", bothEnds);

      await page.getByRole("button", { name: "Advanced settings" }).click();
      await expect(page.getByLabel("Service identity id")).toBeVisible();
      await expectCleanLayout(page, "new workspace advanced open", bothEnds);

      await page.getByLabel("Workspace name").fill(LONG_UNBROKEN_NAME);
      await expect(page.getByLabel("Slug")).not.toHaveValue("");
      await page.getByLabel("Service identity id").fill("svc_identity_only");
      await page.getByRole("button", { name: "Create workspace" }).click();
      await expect(page.getByRole("alert")).toBeVisible();
      await expectCleanLayout(page, "new workspace field mismatch", bothEnds);

      await page.getByLabel("Service identity id").fill("");
      api.fail.createWorkspace = { status: 409, message: LONG_ERROR };
      await page.getByRole("button", { name: "Create workspace" }).click();
      await expect(
        page.getByText(/control plane rejected/).first(),
      ).toBeVisible();
      await expectCleanLayout(page, "new workspace quota exceeded", bothEnds);

      api.fail = {};
      api.hold.createWorkspace = deferred();
      await page.getByRole("button", { name: "Create workspace" }).click();
      await expect(
        page.getByRole("button", { name: "Creating…" }),
      ).toBeVisible();
      await expectCleanLayout(page, "new workspace creating", bothEnds);
      api.hold.createWorkspace.resolve();
    });

    test("onboarding organization and workspace steps", async ({ page }) => {
      const api = await installHostedApi(page, { organizations: [] });
      await gotoHosted(page, "/hosted/onboarding/organization");
      await expect(
        page.getByRole("heading", { name: "Name your organization" }),
      ).toBeVisible();
      await expectCleanLayout(page, "onboarding organization", bothEnds);

      api.fail.createOrganization = { message: LONG_ERROR };
      await page.getByLabel("Organization name").fill(LONG_UNBROKEN_NAME);
      await page.getByRole("button", { name: "Continue" }).click();
      await expect(page.getByRole("alert")).toBeVisible();
      await expectCleanLayout(page, "onboarding organization failed", bothEnds);

      const api2 = await installHostedApi(page, { workspaces: [] });
      await gotoHosted(page, "/hosted/onboarding/workspace");
      await expect(
        page.getByRole("heading", { name: "Name your first workspace" }),
      ).toBeVisible();
      await expectCleanLayout(page, "onboarding workspace", bothEnds);

      api2.fail.createWorkspace = { message: LONG_ERROR };
      await page.getByLabel("Workspace name").fill(LONG_UNBROKEN_NAME);
      await page.getByRole("button", { name: "Create workspace" }).click();
      await expect(page.getByRole("alert")).toBeVisible();
      await expectCleanLayout(page, "onboarding workspace failed", bothEnds);
    });

    test("MCP authorize consent screen", async ({ page }) => {
      const api = await installHostedApi(page);
      const query =
        "?client_id=chatgpt&redirect_uri=https%3A%2F%2Fchatgpt.com%2Fconnector_platform_oauth_redirect" +
        "&response_type=code&state=abc&code_challenge=xyz&code_challenge_method=S256&scope=mcp";
      await gotoHosted(page, `/hosted/mcp/authorize${query}`);
      await expect(
        page.getByRole("heading", { name: "Connect ChatGPT" }),
      ).toBeVisible();
      // The consent screen fans out one workspace request per organization.
      await expect(page.getByLabel("Workspace")).toBeVisible({
        timeout: 15_000,
      });
      await expectCleanLayout(page, "mcp authorize form", bothEnds);

      await page.getByLabel("ChatGPT agent name").fill(LONG_UNBROKEN_NAME);
      api.fail.mcpAuthorize = { message: LONG_ERROR };
      await page.getByRole("button", { name: "Connect ChatGPT" }).click();
      await expect(page.getByRole("alert")).toBeVisible();
      await expectCleanLayout(page, "mcp authorize failed", bothEnds);

      api.workspaces = [];
      await page.reload();
      await waitForHydration(page);
      await expect(
        page.getByText("Create a hosted workspace before connecting ChatGPT."),
      ).toBeVisible(firstPaint);
      await expectCleanLayout(
        page,
        "mcp authorize without workspaces",
        bothEnds,
      );
    });

    test("oauth callback pending and error states", async ({ page }) => {
      await installHostedApi(page);
      await gotoHosted(
        page,
        "/hosted/oauth/google/callback?error=access_denied&state=missing",
      );
      await expect(
        page.getByRole("heading", { name: "Finishing sign-in" }),
      ).toBeVisible();
      await expect(page.getByRole("alert")).toBeVisible();
      await expectCleanLayout(page, "oauth callback provider error", bothEnds);

      await gotoHosted(page, "/hosted/oauth/github/callback");
      await expect(page.getByRole("alert")).toBeVisible();
      await expectCleanLayout(page, "oauth callback missing code", bothEnds);

      await gotoHosted(page, "/hosted/oauth/unknown/callback?code=a&state=b");
      await expect(page.getByText("Unsupported OAuth provider.")).toBeVisible();
      await expectCleanLayout(page, "oauth callback unsupported provider");
    });

    test("billing return, mock portal and legal pages", async ({ page }) => {
      const api = await installHostedApi(page);
      api.hold.checkoutReturn = deferred();
      await gotoHosted(
        page,
        "/hosted/billing/return?session_id=cs_mock_hosted_1",
      );
      await expect(
        page.getByRole("heading", { name: "Returning from checkout" }),
      ).toBeVisible();
      await expectCleanLayout(page, "billing return redirecting");
      api.hold.checkoutReturn.resolve();
      api.hold = {};
      await page.waitForURL(/\/billing(\?.*)?$/);

      await gotoHosted(page, "/hosted/billing/mock-portal");
      await expect(
        page.getByRole("heading", { name: "Local Stripe mock" }),
      ).toBeVisible();
      await expectCleanLayout(page, "mock portal", bothEnds);

      for (const slug of ["privacy", "terms", "cookies"]) {
        await gotoHosted(page, `/hosted/legal/${slug}`);
        await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
        await expectCleanLayout(page, `legal ${slug}`, bothEnds);
      }
    });

    test("session error panel in the hosted shell", async ({ page }) => {
      const api = await installHostedApi(page, {
        fail: { organizations: { status: 500, message: LONG_ERROR } },
      });
      await gotoHosted(page, "/hosted/dashboard");
      await expect(
        page.getByRole("heading", { name: "We couldn't load your account" }),
      ).toBeVisible(firstPaint);
      await expectCleanLayout(page, "hosted session error", bothEnds);

      api.hold.organizations = deferred();
      await page.getByRole("button", { name: "Retry" }).first().click();
      await expectCleanLayout(page, "hosted session retrying", bothEnds);
      api.hold.organizations.resolve();
    });
  });
}
