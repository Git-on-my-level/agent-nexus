import { expect, test } from "@playwright/test";

test("renders the access page without auth seeding", async ({ page }) => {
  await page.goto("/o/local/w/local/access");

  await expect(
    page.getByRole("heading", { name: "Select Actor Identity" }),
  ).toBeVisible();
  await expect(page.getByText("Prefer authenticated access?")).toBeVisible();
  await expect(page.locator("body")).not.toContainText("anx_ui_refresh_token");
});

test("reads the cookie-backed session from the same-origin endpoint", async ({
  page,
}) => {
  await page.context().addCookies([
    {
      name: "anx_ui_session_local",
      value: "test-refresh-token",
      domain: "127.0.0.1",
      path: "/",
      httpOnly: true,
    },
  ]);

  await page.route("**/auth/session", async (route) => {
    expect(route.request().headers().cookie ?? "").toContain(
      "anx_ui_session_local=test-refresh-token",
    );
    await route.fulfill({
      status: 200,
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        authenticated: true,
        agent: {
          agent_id: "agent-ops-ai",
          actor_id: "actor-ops-ai",
          username: "ops-ai",
        },
      }),
    });
  });

  await page.goto("/o/local/w/local/access");

  const session = await page.evaluate(async () => {
    const response = await fetch("/auth/session", {
      headers: {
        "x-anx-workspace-slug": "local",
      },
    });
    return response.json();
  });

  expect(session).toEqual({
    authenticated: true,
    agent: {
      agent_id: "agent-ops-ai",
      actor_id: "actor-ops-ai",
      username: "ops-ai",
    },
  });
});

test("lists agents under their host and people by name", async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem("workspaceTourSeen.local", "1");
  });
  await page.context().addCookies([
    {
      name: "anx_ui_session_local",
      value: "test-refresh-token",
      domain: "127.0.0.1",
      path: "/",
      httpOnly: true,
    },
  ]);
  const json = (body) => ({
    status: 200,
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });
  const agent = {
    id: "agent-codex",
    ref: "actor:actor-codex",
    actor_id: "actor-codex",
    host_id: "host-1",
    host_slug: "workstation-a",
    name: "codex",
    handle: "codex.workstation-a",
    display_name: "codex on workstation-a",
    identity_kind: "derived",
    state: "working",
    bridge_online: true,
    current_card_ref: null,
    current_card_title: null,
    last_progress_note: null,
    last_progress_at: null,
    active_run: null,
    open_asks_count: 0,
    last_signal_at: new Date().toISOString(),
    revoked_at: null,
  };
  await page.route("**/auth/session", (route) =>
    route.fulfill(
      json({
        authenticated: true,
        agent: {
          agent_id: "agent-ops-human",
          actor_id: "actor-ops-human",
          username: "passkey.ops.human.8dff59fc",
        },
      }),
    ),
  );
  await page.route("**/auth/principals?**", (route) =>
    route.fulfill(
      json({
        principals: [
          {
            agent_id: "agent-ops-human",
            actor_id: "actor-ops-human",
            username: "riley@example.com",
            principal_kind: "human",
            auth_method: "passkey",
            created_at: "2026-03-01T10:00:00Z",
            last_seen_at: "2026-03-20T11:15:00Z",
            revoked: false,
          },
          {
            agent_id: "agent-codex",
            actor_id: "actor-codex",
            username: "codex.workstation-a",
            principal_kind: "agent",
            auth_method: "host_assertion",
            created_at: "2026-03-01T10:00:00Z",
            revoked: false,
          },
        ],
        active_human_principal_count: 1,
      }),
    ),
  );
  await page.route("**/auth/invites", (route) =>
    route.fulfill(json({ invites: [] })),
  );
  await page.route("**/auth/audit?**", (route) =>
    route.fulfill(json({ events: [] })),
  );
  await page.route("**/auth/hosts/enrollments/pending", (route) =>
    route.fulfill(json({ enrollments: [] })),
  );
  await page.route("**/auth/hosts/enrollment-tokens", (route) =>
    route.fulfill(json({ enrollment_tokens: [] })),
  );
  await page.route(/(?<!\/auth)\/hosts$/, (route) =>
    route.fulfill(
      json({
        hosts: [
          {
            id: "host-1",
            ref: "host:host-1",
            handle: "workstation-a",
            slug: "workstation-a",
            display_name: "workstation-a",
            os_user: "operator",
            hostname: "workstation-a.local",
            discovered_adapters: ["codex"],
            key_id: "hkey_1",
            excluded_names: [],
            agents: [agent],
            created_at: "2026-03-01T10:00:00Z",
            revoked_at: null,
          },
        ],
      }),
    ),
  );
  await page.route(/\/agents$/, (route) =>
    route.fulfill(json({ agents: [agent] })),
  );

  await page.goto("/o/local/w/local/access");
  const main = page.getByRole("main");
  await expect(
    main.locator('[data-host-agent="codex.workstation-a"]'),
  ).toBeVisible();
  await expect(main.getByText("riley@example.com")).toBeVisible();
  // The derived agent is not repeated as a principal, and invites stay human-only.
  await expect(main.locator('[data-principal="agent-codex"]')).toHaveCount(0);
  await expect(main.getByText("Standalone agents")).toHaveCount(0);
  await expect(main.getByLabel("Kind")).toHaveCount(0);
  // The principal id is a copy affordance, not the row's label.
  await expect(
    main.locator('[data-principal="agent-ops-human"] p').first(),
  ).toHaveText(/^riley@example\.com/);
});

test("badges pending access on the account trigger and the Access item", async ({
  page,
}) => {
  await page.addInitScript(() => {
    localStorage.setItem("workspaceTourSeen.local", "1");
  });
  await page.context().addCookies([
    {
      name: "anx_ui_session_local",
      value: "test-refresh-token",
      domain: "127.0.0.1",
      path: "/",
      httpOnly: true,
    },
  ]);
  const json = (body) => ({
    status: 200,
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });
  const state = {
    requests: [
      {
        id: "areq_1",
        principal_id: "agent-fleet",
        actor_id: "actor-fleet",
        username: "fleet.build-runner",
        grant: "auth-admin",
        reason: "approve enrollments while you are asleep",
        status: "pending",
        created_at: new Date(Date.now() - 90_000).toISOString(),
        request_event_ref: "event:evt_1",
        inbox_item_id: "inbox_1",
      },
    ],
    pending: [
      {
        id: "henr_1",
        user_code: "J6FA-N4XI",
        requested_slug: "ci-runner",
        os_user: "runner",
        hostname: "ci-runner.local",
        discovered_adapters: ["generic"],
        adoption_names: [],
        requesting_ip: "203.0.113.17",
        status: "pending",
        expires_at: new Date(Date.now() + 8 * 60_000).toISOString(),
        created_at: new Date().toISOString(),
      },
      // Approved: waiting on the machine, so it is not part of the number.
      {
        id: "henr_2",
        user_code: "K7GB-P5YJ",
        requested_slug: "staging-box",
        os_user: "runner",
        hostname: "staging-box.local",
        discovered_adapters: ["generic"],
        adoption_names: [],
        status: "approved",
        expires_at: new Date(Date.now() + 8 * 60_000).toISOString(),
        created_at: new Date().toISOString(),
      },
    ],
  };
  await page.route("**/auth/session", (route) =>
    route.fulfill(
      json({
        authenticated: true,
        agent: {
          agent_id: "agent-ops-human",
          actor_id: "actor-ops-human",
          username: "passkey.ops.human.8dff59fc",
          principal_kind: "human",
        },
      }),
    ),
  );
  await page.route("**/auth/hosts/enrollments/pending", (route) =>
    route.fulfill(json({ enrollments: state.pending })),
  );
  await page.route("**/auth/access-requests", (route) =>
    route.fulfill(json({ requests: state.requests })),
  );
  await page.route("**/auth/admins", (route) =>
    route.fulfill(json({ admins: [] })),
  );
  await page.route("**/auth/principals?**", (route) =>
    route.fulfill(
      json({
        principals: [
          {
            agent_id: "agent-ops-human",
            actor_id: "actor-ops-human",
            username: "riley@example.com",
            principal_kind: "human",
            auth_method: "passkey",
            created_at: "2026-03-01T10:00:00Z",
            revoked: false,
          },
        ],
        active_human_principal_count: 1,
      }),
    ),
  );
  await page.route("**/auth/invites", (route) =>
    route.fulfill(json({ invites: [] })),
  );
  await page.route("**/auth/audit?**", (route) =>
    route.fulfill(json({ events: [] })),
  );
  await page.route("**/auth/hosts/enrollment-tokens", (route) =>
    route.fulfill(json({ enrollment_tokens: [] })),
  );
  await page.route(/(?<!\/auth)\/hosts$/, (route) =>
    route.fulfill(json({ hosts: [] })),
  );
  await page.route(/\/agents$/, (route) => route.fulfill(json({ agents: [] })));

  await page.goto("/o/local/w/local/access");

  // One number covering both kinds, on the trigger that hides Access. The
  // approved enrollment waits on its machine, so it is not in it.
  const trigger = page.locator("[data-access-trigger-count]");
  await expect(trigger).toHaveText("2");
  await expect(trigger).toHaveAttribute("title", "2 access requests waiting");
  // The trigger's own aria-label suppresses descendant names, so the number
  // has to be in the label or it reaches no screen reader at all.
  await expect(
    page.getByRole("button", {
      name: "Account menu, 2 access requests waiting",
    }),
  ).toBeVisible();
  // The page cannot disagree with the badge.
  await expect(page.locator("[data-pending-access-count]")).toHaveText("2");
  await expect(page.locator("[data-host-enrollment]")).toHaveCount(2);
  await expect(page.locator("[data-access-request]")).toHaveCount(1);
  await expect(page.getByText(/asks to administer access/)).toBeVisible();

  // And on the Access item itself, once the menu is open.
  await page.getByRole("button", { name: /^Account menu/ }).click();
  await expect(page.locator("[data-access-nav-count]")).toHaveText("2");

  // Deciding everything clears the badge rather than showing a zero.
  await page.keyboard.press("Escape");
  await page.route("**/auth/access-requests/areq_1/deny", (route) =>
    route.fulfill(json({ request: { id: "areq_1", status: "denied" } })),
  );
  await page.route("**/auth/hosts/enrollments/henr_1/deny", (route) =>
    route.fulfill(json({ enrollment: { id: "henr_1", status: "denied" } })),
  );
  state.requests = [];
  state.pending = [];
  await page
    .locator('[data-access-request="areq_1"]')
    .getByRole("button", { name: "Deny", exact: true })
    .click();
  await expect(page.locator("[data-access-trigger-count]")).toHaveCount(0);
  await expect(page.locator("[data-pending-access]")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Account menu" }),
  ).toBeVisible();
});
