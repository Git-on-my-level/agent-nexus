import { expect, test } from "@playwright/test";

/**
 * Persona sprawl on Access: idle personas fold behind a per-host count, the
 * page filter switches between active, idle and archived, and a person can
 * archive and restore a persona.
 */

const DAY = 24 * 60 * 60 * 1000;
const ago = (ms) => new Date(Date.now() - ms).toISOString();
const json = (body) => ({
  status: 200,
  headers: { "content-type": "application/json" },
  body: JSON.stringify(body),
});

function agent(name, overrides = {}) {
  return {
    id: `agent-${name}`,
    ref: `actor:actor-${name}`,
    actor_id: `actor-${name}`,
    host_id: "host-1",
    host_slug: "studio",
    name,
    handle: `${name}.studio`,
    display_name: `${name} on studio`,
    identity_kind: "derived",
    state: "",
    bridge_online: false,
    current_card_ref: null,
    current_card_title: null,
    last_progress_note: null,
    last_progress_at: null,
    active_run: null,
    open_asks_count: 0,
    waiting_ask: null,
    last_signal_at: null,
    revoked_at: null,
    last_used_at: null,
    archived_at: null,
    archived_reason: null,
    ...overrides,
  };
}

test("folds idle personas, archives and restores one", async ({ page }) => {
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
  let agents = [
    agent("codex", { last_used_at: ago(DAY) }),
    agent("config-judge", { last_used_at: ago(10 * 60 * 1000) }),
    agent("codex-spkfinal", { last_used_at: ago(9 * DAY) }),
    agent("codex-spkfinal-r2", { last_used_at: ago(20 * DAY) }),
    agent("grok-build", {
      archived_at: ago(3 * DAY),
      archived_reason: "inactive",
      last_used_at: ago(40 * DAY),
    }),
  ];
  const hostBody = () => ({
    id: "host-1",
    ref: "host:host-1",
    handle: "studio",
    slug: "studio",
    display_name: "studio",
    os_user: "david",
    hostname: "studio.local",
    discovered_adapters: ["codex"],
    key_id: "hkey_1",
    excluded_names: [],
    agents,
    created_at: "2026-10-10T10:00:00Z",
    revoked_at: null,
  });
  const calls = [];
  await page.route("**/auth/session", (route) =>
    route.fulfill(
      json({
        authenticated: true,
        agent: {
          agent_id: "agent-human",
          actor_id: "actor-human",
          username: "david@example.com",
          principal_kind: "human",
          auth_method: "passkey",
        },
      }),
    ),
  );
  await page.route("**/auth/principals?**", (route) =>
    route.fulfill(json({ principals: [], active_human_principal_count: 1 })),
  );
  await page.route("**/auth/invites", (route) =>
    route.fulfill(json({ invites: [] })),
  );
  await page.route("**/auth/audit?**", (route) =>
    route.fulfill(json({ events: [] })),
  );
  await page.route("**/auth/admins", (route) =>
    route.fulfill(json({ admins: [] })),
  );
  await page.route("**/auth/access-requests", (route) =>
    route.fulfill(json({ access_requests: [] })),
  );
  await page.route("**/auth/hosts/enrollments/pending", (route) =>
    route.fulfill(json({ enrollments: [] })),
  );
  await page.route("**/auth/hosts/enrollment-tokens", (route) =>
    route.fulfill(json({ enrollment_tokens: [] })),
  );
  await page.route(/(?<!\/auth)\/hosts$/, (route) =>
    route.fulfill(json({ hosts: [hostBody()] })),
  );
  await page.route(
    /\/hosts\/host-1\/agents\/[^/]+\/(archive|restore)$/,
    (route) => {
      const [, name, action] = route
        .request()
        .url()
        .match(/agents\/([^/]+)\/(archive|restore)$/);
      calls.push(`${action}:${name}`);
      agents = agents.map((row) =>
        row.name === name
          ? {
              ...row,
              archived_at:
                action === "archive" ? new Date().toISOString() : null,
              archived_reason: action === "archive" ? "manual" : null,
            }
          : row,
      );
      return route.fulfill(json({ host: hostBody() }));
    },
  );
  await page.route(/\/agents$/, (route) => route.fulfill(json({ agents })));

  await page.goto("/o/local/w/local/access");
  const main = page.getByRole("main");
  await expect(main.locator("[data-access-persona-summary]")).toHaveText(
    "1 host · 4 personas · 2 active in the last 7 days",
  );
  await expect(main.locator('[data-host-agent="codex.studio"]')).toBeVisible();
  await expect(
    main.locator('[data-host-agent="config-judge.studio"]'),
  ).toBeVisible();
  await expect(
    main.locator('[data-host-agent="codex-spkfinal.studio"]'),
  ).toHaveCount(0);
  await expect(main.locator("[data-host-idle-personas]")).toContainText(
    "2 idle personas hidden",
  );

  await main.getByRole("button", { name: "Show 2" }).click();
  await expect(
    main.locator('[data-host-agent="codex-spkfinal-r2.studio"]'),
  ).toBeVisible();

  await main.getByRole("button", { name: "Archive codex-spkfinal-r2" }).click();
  const confirm = main.locator(
    '[data-persona-archive-confirm="codex-spkfinal-r2"]',
  );
  await expect(confirm).toContainText("loses access now");
  await confirm.getByRole("button", { name: "Archive" }).click();
  await expect(
    main.locator('[data-host-agent="codex-spkfinal-r2.studio"]'),
  ).toHaveCount(0);
  expect(calls).toEqual(["archive:codex-spkfinal-r2"]);

  await main.locator('[data-persona-filter="archived"]').click();
  const archived = main.locator("[data-archived-personas]");
  await expect(archived).toContainText("grok-build");
  await expect(archived).toContainText("auto-archived");
  await expect(archived).toContainText(
    "Auto-archived personas are hidden until they are used again",
  );
  await expect(archived).toContainText("Names a human archived stay reserved");
  await archived
    .getByRole("button", { name: "Restore grok-build on studio" })
    .click();
  await expect(
    main.locator('[data-archived-persona="grok-build.studio"]'),
  ).toHaveCount(0);
  expect(calls).toEqual(["archive:codex-spkfinal-r2", "restore:grok-build"]);
});
