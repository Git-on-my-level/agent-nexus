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
    host_slug: "m5-mbp",
    name: "codex",
    handle: "codex.m5-mbp",
    display_name: "codex on m5-mbp",
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
            username: "codex.m5-mbp",
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
            handle: "m5-mbp",
            slug: "m5-mbp",
            display_name: "m5-mbp",
            os_user: "david",
            hostname: "m5-mbp.local",
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
  await expect(main.locator('[data-host-agent="codex.m5-mbp"]')).toBeVisible();
  await expect(main.getByText("riley@example.com")).toBeVisible();
  // The derived agent is not repeated as a principal, and no invite form
  // offers agent registration any more.
  await expect(main.locator('[data-principal="agent-codex"]')).toHaveCount(0);
  await expect(main.getByText("Standalone agents")).toHaveCount(0);
  await expect(main.getByLabel("Kind")).toHaveCount(0);
  // The principal id is a copy affordance, not the row's label.
  await expect(
    main.locator('[data-principal="agent-ops-human"] p').first(),
  ).toHaveText(/^riley@example\.com/);
});
