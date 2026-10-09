import { expect, test } from "@playwright/test";

import { AUDIT_VIEWPORTS, expectCleanLayout } from "../helpers/layoutAudit.js";

/**
 * Walks the Access page through the states it can reach (loading, errors,
 * host approval, exclusions, host revoke, headless tokens, people, invites,
 * break-glass) at several viewport sizes and runs the geometry audit after
 * each transition.
 */

const ACCESS_PATH = "/o/local/w/local/access";
const LONG_HASH =
  "1fb951be68b4aa395611181d1d7af40857ea6c038e26393eab6433516f2ee888";
const SELF_AGENT_ID = `agent_ext_${LONG_HASH}`;
const HOST_ID = "host_7b0c2f3e-1111-4c7d-8116-3a212e1fe301";
const NOW = Date.now();
const iso = (offsetMs) => new Date(NOW + offsetMs).toISOString();

function principal(overrides) {
  return {
    actor_id: `actor-${overrides.agent_id}`,
    principal_kind: "human",
    auth_method: "passkey",
    created_at: "2026-03-01T10:00:00Z",
    last_seen_at: "2026-03-20T11:15:00Z",
    updated_at: "2026-03-28T10:00:00Z",
    revoked: false,
    ...overrides,
  };
}

function agentSummary(name, state, overrides = {}) {
  return {
    id: `agent-${name}`,
    ref: `actor:actor-${name}`,
    actor_id: `actor-${name}`,
    host_id: HOST_ID,
    host_slug: "workstation-a",
    name,
    handle: `${name}.workstation-a`,
    display_name: `${name} on workstation-a`,
    identity_kind: "derived",
    state,
    bridge_online: state === "working",
    current_card_ref: null,
    current_card_title: null,
    last_progress_note: null,
    last_progress_at: null,
    active_run: null,
    open_asks_count: 0,
    last_signal_at: iso(-60_000),
    revoked_at: null,
    ...overrides,
  };
}

const AGENTS = [
  agentSummary("codex", "working"),
  agentSummary("claude", "waiting_on_human", { open_asks_count: 1 }),
  agentSummary("release-bot-with-a-rather-long-persona-name", "stale", {
    last_signal_at: null,
  }),
];

function host(overrides = {}) {
  return {
    id: HOST_ID,
    ref: `host:${HOST_ID}`,
    handle: "workstation-a",
    slug: "workstation-a",
    display_name: "workstation-a",
    os_user: "operator",
    hostname: "workstation-a.local",
    discovered_adapters: ["claude", "codex", "cursor"],
    key_id: "hkey_6ecc6649-91e8-46cf-9e5b-57f1cbf8ffd4",
    excluded_names: [],
    // Host reads carry the same derived agent state as the roster.
    agents: AGENTS,
    created_at: "2026-03-01T10:00:00Z",
    revoked_at: null,
    ...overrides,
  };
}

const PENDING = [
  {
    id: "henr_1",
    user_code: "J6FA-N4XI",
    requested_slug: "ci-runner-with-a-long-machine-name-3",
    os_user: "runner",
    hostname: "ip-10-0-3-17.eu-west-1.compute.internal",
    discovered_adapters: ["generic"],
    adoption_names: [],
    requesting_ip: "203.0.113.17",
    status: "pending",
    expires_at: iso(8 * 60_000),
    created_at: iso(-2 * 60_000),
  },
];

const HUMANS = [
  principal({
    agent_id: SELF_AGENT_ID,
    // Hosted humans arrive with a synthesized, very long username.
    username: `external.${LONG_HASH.slice(0, 54)}`,
    auth_method: "external_grant",
  }),
  principal({ agent_id: "agent-second-human", username: "riley@example.com" }),
];

const PRINCIPALS = [
  ...HUMANS,
  // Derived agents are principals too; the page lists them under hosts.
  ...AGENTS.map((agent) =>
    principal({
      agent_id: agent.id,
      actor_id: agent.actor_id,
      username: agent.handle,
      principal_kind: "agent",
      auth_method: "host_assertion",
    }),
  ),
  principal({
    agent_id: "agent-legacy",
    username: "legacy-hermes",
    principal_kind: "agent",
    auth_method: "public_key",
  }),
];

const AUDIT_EVENT_TYPES = [
  "host_enroll_started",
  "host_enroll_approved",
  "host_enroll_completed",
  "derived_agent_created",
  "host_exclusions_changed",
  "host_revoked",
  "invite_created",
  "principal_revoked",
  "some_future_event_type",
];

const AUDIT = AUDIT_EVENT_TYPES.map((event_type, i) => ({
  event_id: `authevt_91e4b6b0-b80f-4a1b-a9fa-8d67634bf6a${i}`,
  event_type,
  occurred_at: "2026-03-28T10:00:00Z",
  actor_agent_id: SELF_AGENT_ID,
  subject_agent_id: `agent_${LONG_HASH}`,
  metadata: {
    host_id: HOST_ID,
    name: "codex",
    requesting_ip: "203.0.113.17",
    excluded_names: ["cursor"],
  },
}));

function deferred() {
  let resolve;
  const promise = new Promise((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

/**
 * Installs a mutable mock of the access API. Each endpoint reads its behavior
 * from `api` at request time, so tests flip a field and trigger the UI.
 *   - `hold.<name>`: a deferred the response waits on (in-flight states)
 *   - `fail.<name>`: respond with an error body
 */
async function installAccessApi(page, overrides = {}) {
  const api = {
    self: { agent_id: SELF_AGENT_ID, actor_id: "actor-self", username: "" },
    authenticated: true,
    principals: PRINCIPALS,
    activeHumans: 2,
    invites: [
      {
        id: "invite_61ab15c4-e615-4c7d-8116-3a212e1fe301",
        kind: "human",
        created_at: "2026-03-28T10:00:00Z",
      },
    ],
    audit: AUDIT,
    auditNextCursor: "",
    hosts: [host()],
    pending: PENDING,
    tokens: [
      {
        id: "htok_used",
        label: "seed runner",
        created_at: "2026-03-20T10:00:00Z",
        expires_at: "2026-03-20T11:00:00Z",
        consumed_at: "2026-03-20T10:05:00Z",
        revoked_at: null,
      },
    ],
    agents: AGENTS,
    createdToken: "oinv_yeJecICpb-7U8xbmFtTzivIiQg2TI32f",
    hostToken: `htok_secret_${"Q2TI32fyeJecICpb".repeat(3)}`,
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
  const errorBody = (message) => ({
    error: { code: "test_failure", message, details: message },
  });

  async function respond(route, name, okBody) {
    api.calls.push(name);
    if (api.hold[name]) await api.hold[name].promise;
    if (api.fail[name]) {
      const failure = api.fail[name];
      return json(
        route,
        failure.status ?? 500,
        failure.body ?? errorBody(failure.message ?? String(failure)),
      );
    }
    return json(route, 200, typeof okBody === "function" ? okBody() : okBody);
  }

  // A lone human would otherwise be redirected into the first-run tour.
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

  await page.route("**/auth/session", (route) =>
    json(
      route,
      200,
      api.authenticated
        ? { authenticated: true, agent: api.self }
        : { authenticated: false },
    ),
  );

  await page.route(/\/auth\/principals(\?.*)?$/, (route) =>
    respond(route, "principals", () => ({
      principals: api.principals,
      active_human_principal_count: api.activeHumans,
    })),
  );

  await page.route(/\/auth\/invites$/, (route) => {
    if (route.request().method() === "POST") {
      return respond(route, "createInvite", () => {
        const kind = route.request().postDataJSON()?.kind;
        api.invites = [
          {
            id: `invite_created-${api.invites.length}-4c7d-8116-3a212e1fe3ff`,
            kind,
            created_at: new Date().toISOString(),
          },
          ...api.invites,
        ];
        return { token: api.createdToken, invite: api.invites[0] };
      });
    }
    return respond(route, "invites", () => ({ invites: api.invites }));
  });

  await page.route(/\/auth\/invites\/[^/]+\/revoke$/, (route) =>
    respond(route, "revokeInvite", () => {
      const id = decodeURIComponent(
        route.request().url().split("/invites/")[1].split("/")[0],
      );
      api.invites = api.invites.map((invite) =>
        invite.id === id
          ? { ...invite, revoked_at: new Date().toISOString() }
          : invite,
      );
      return { ok: true };
    }),
  );

  await page.route(/\/auth\/principals\/[^/]+\/revoke$/, (route) =>
    respond(route, "revokePrincipal", () => {
      const id = decodeURIComponent(
        route.request().url().split("/principals/")[1].split("/")[0],
      );
      api.principals = api.principals.map((p) =>
        p.agent_id === id ? { ...p, revoked: true } : p,
      );
      return { ok: true };
    }),
  );

  await page.route(/\/auth\/audit(\?.*)?$/, (route) => {
    const cursor = new URL(route.request().url()).searchParams.get("cursor");
    return respond(route, cursor ? "auditMore" : "audit", () => ({
      events: cursor
        ? api.audit.slice(0, 4).map((e, i) => ({
            ...e,
            event_id: `${e.event_id}-page2-${i}`,
          }))
        : api.audit,
      next_cursor: cursor ? "" : api.auditNextCursor,
    }));
  });

  await page.route(/\/auth\/hosts\/enrollments\/pending$/, (route) =>
    respond(route, "pending", () => ({ enrollments: api.pending })),
  );
  await page.route(
    /\/auth\/hosts\/enrollments\/[^/]+\/(approve|deny)$/,
    (route) => {
      const [, id, action] = route
        .request()
        .url()
        .match(/enrollments\/([^/]+)\/(approve|deny)$/);
      return respond(route, action, () => {
        const enrollment = api.pending.find((entry) => entry.id === id);
        api.pending = api.pending.filter((entry) => entry.id !== id);
        return {
          enrollment: {
            ...enrollment,
            status: action === "approve" ? "approved" : "denied",
          },
          poll_interval_seconds: 3,
        };
      });
    },
  );
  await page.route(/\/auth\/hosts\/enrollment-tokens$/, (route) => {
    if (route.request().method() === "POST") {
      return respond(route, "createToken", () => {
        const body = route.request().postDataJSON();
        const token = {
          id: `htok_${api.tokens.length}`,
          label: body.label,
          created_at: new Date().toISOString(),
          expires_at: body.expires_at,
          consumed_at: null,
          revoked_at: null,
        };
        api.tokens = [token, ...api.tokens];
        return { enrollment_token: token, token: api.hostToken };
      });
    }
    return respond(route, "tokens", () => ({
      enrollment_tokens: api.tokens,
    }));
  });
  await page.route(
    /\/auth\/hosts\/enrollment-tokens\/[^/]+\/revoke$/,
    (route) =>
      respond(route, "revokeToken", () => {
        const id = route
          .request()
          .url()
          .split("/enrollment-tokens/")[1]
          .split("/")[0];
        api.tokens = api.tokens.map((token) =>
          token.id === id
            ? { ...token, revoked_at: new Date().toISOString() }
            : token,
        );
        return { enrollment_token: api.tokens.find((t) => t.id === id) };
      }),
  );

  await page.route(/(?<!\/auth)\/hosts(\?.*)?$/, (route) =>
    respond(route, "hosts", () => ({ hosts: api.hosts })),
  );
  await page.route(/(?<!\/auth)\/hosts\/[^/?]+$/, (route) => {
    const method = route.request().method();
    const id = decodeURIComponent(route.request().url().split("/hosts/")[1]);
    if (method === "PATCH") {
      return respond(route, "patchHost", () => {
        const body = route.request().postDataJSON();
        api.hosts = api.hosts.map((entry) =>
          entry.id === id ? { ...entry, ...body } : entry,
        );
        return { host: api.hosts.find((entry) => entry.id === id) };
      });
    }
    if (method === "DELETE") {
      return respond(route, "revokeHost", () => {
        api.hosts = api.hosts.map((entry) =>
          entry.id === id
            ? { ...entry, revoked_at: new Date().toISOString() }
            : entry,
        );
        return { host: api.hosts.find((entry) => entry.id === id) };
      });
    }
    return respond(route, "getHost", () => ({
      host: api.hosts.find((entry) => entry.id === id),
    }));
  });

  await page.route(/\/agents(\?.*)?$/, (route) =>
    respond(route, "agents", () => ({ agents: api.agents })),
  );

  return api;
}

/**
 * The access page reads `outOfWorkspaceMode` from its server load. The e2e
 * server runs the local provider, so hosted layout is exercised by rewriting
 * that field in SvelteKit's `__data.json` during a client-side navigation.
 */
async function gotoAccessAsHosted(page) {
  await page.route("**/access/__data.json*", async (route) => {
    const response = await route.fetch();
    const payload = await response.json();
    for (const node of payload.nodes ?? []) {
      const table = node?.data;
      const index = table?.[0]?.outOfWorkspaceMode;
      if (Array.isArray(table) && Number.isInteger(index)) {
        table[index] = "hosted";
      }
    }
    await route.fulfill({ response, json: payload });
  });
  await page.goto("/o/local/w/local/more");
  await page
    .getByRole("main")
    .getByRole("link", { name: /access/i })
    .first()
    .click();
  await expect(page).toHaveURL(/\/access$/);
  await expect(
    page.getByRole("heading", { name: "Access", exact: true }),
  ).toBeVisible();
}

const bothEnds = { scrollPositions: ["top", "bottom"] };

for (const viewport of AUDIT_VIEWPORTS) {
  test.describe(`access page states @ ${viewport.name}`, () => {
    test.use({
      viewport: { width: viewport.width, height: viewport.height },
      permissions: ["clipboard-read", "clipboard-write"],
    });

    test("signed out", async ({ page }) => {
      await installAccessApi(page, { authenticated: false });
      await page.goto(ACCESS_PATH);
      await expect(page.getByRole("main")).toBeVisible();
      await expectCleanLayout(page, "signed out");
    });

    test("loading, populated, empty and failed sections", async ({ page }) => {
      const api = await installAccessApi(page);
      api.hold.hosts = deferred();
      api.hold.principals = deferred();
      await page.goto(ACCESS_PATH);
      await expect(
        page.getByRole("heading", { name: "Access", exact: true }),
      ).toBeVisible();
      await expectCleanLayout(page, "initial loading");

      api.hold.hosts.resolve();
      api.hold.principals.resolve();
      api.hold = {};
      const main = page.getByRole("main");
      await expect(main.locator('[data-host="workstation-a"]')).toBeVisible();
      // Agents appear under their host with the roster's state, humans by
      // name, the legacy agent on its own; ids stay behind copy buttons.
      await expect(
        main.locator('[data-host-agent="codex.workstation-a"]'),
      ).toContainText("Working");
      await expect(main.getByText("riley@example.com")).toBeVisible();
      await expect(main.getByText("Standalone agents")).toBeVisible();
      await expect(main.getByText(SELF_AGENT_ID, { exact: true })).toHaveCount(
        0,
      );
      await expect(
        main.getByText(/workstation-a approved|approved workstation-a/),
      ).toBeVisible();
      await expectCleanLayout(page, "populated", bothEnds);

      api.fail = {
        hosts: { message: "hosts backend unavailable ".repeat(6) },
        principals: { message: "principals backend unavailable" },
        audit: { message: "audit backend unavailable" },
      };
      await page.reload();
      await expect(page.getByText("audit backend unavailable")).toBeVisible();
      await expectCleanLayout(page, "sections failed", bothEnds);

      api.fail = {};
      api.principals = [HUMANS[0]];
      api.invites = [];
      api.audit = [];
      api.hosts = [];
      api.pending = [];
      api.tokens = [];
      api.agents = [];
      await page.reload();
      await expect(page.getByText("Connect your first machine")).toBeVisible();
      await expect(page.locator("[data-host-waiting]")).toBeVisible();
      await expectCleanLayout(page, "empty workspace", bothEnds);

      /*
       * The flip to "Enrolled" is driven by the setup panel watching its own
       * token, and this suite runs against a loopback core where that panel
       * withholds its prompt and issues no token. The behaviour is covered in
       * `tests/unit/accessPage.test.js`, which can choose the base URL; what
       * belongs here is that the empty state reads correctly at every width.
       */
      await expectCleanLayout(page, "first machine panel", bothEnds);
    });

    test("approve and deny host enrollment", async ({ page }) => {
      const api = await installAccessApi(page, {
        pending: [
          PENDING[0],
          {
            ...PENDING[0],
            id: "henr_2",
            requested_slug: "workstation-a-2",
            user_code: "OJQR-P6XT",
          },
        ],
      });
      await page.goto(ACCESS_PATH);
      const first = page.locator('[data-host-enrollment="henr_1"]');
      await expect(first.locator("[data-enrollment-code]")).toHaveText(
        "J6FA-N4XI",
      );
      await expect(first.locator("[data-enrollment-ip]")).toHaveText(
        "203.0.113.17",
      );
      await expectCleanLayout(page, "pending requests", bothEnds);

      // Approval takes a second, deliberate step that repeats the code.
      await first.getByRole("button", { name: "Approve…" }).click();
      const confirm = first.locator("[data-enrollment-confirm]");
      await expect(confirm).toContainText("J6FA-N4XI");
      expect(api.calls).not.toContain("approve");
      await expectCleanLayout(page, "approve confirmation");

      api.fail.approve = { status: 409, message: "host slug already taken" };
      await confirm.getByRole("button", { name: /Codes match/ }).click();
      await expect(first.getByText("host slug already taken")).toBeVisible();
      await expectCleanLayout(page, "approve failed");

      api.fail = {};
      await confirm.getByRole("button", { name: /Codes match/ }).click();
      await expect(first).toHaveCount(0);
      await expect(page.locator("[data-enrollment-notice]")).toContainText(
        "Approved ci-runner-with-a-long-machine-name-3",
      );
      await expectCleanLayout(page, "approved");

      await page
        .locator('[data-host-enrollment="henr_2"]')
        .getByRole("button", { name: "Deny" })
        .click();
      await expect(page.locator("[data-host-enrollment]")).toHaveCount(0);
      expect(api.calls).toContain("deny");
      await expectCleanLayout(page, "denied");
    });

    test("host exclusions and revoke", async ({ page }) => {
      const api = await installAccessApi(page, { pending: [] });
      await page.goto(ACCESS_PATH);
      const card = page.locator('[data-host="workstation-a"]');
      await card.getByRole("button", { name: "Edit" }).click();
      const input = card.getByLabel("Name to exclude on workstation-a");
      await input.fill("Cursor!");
      await expect(card.getByText(/Use lowercase letters/)).toBeVisible();
      await expectCleanLayout(page, "invalid exclusion");
      await input.fill("cursor");
      await card.getByRole("button", { name: "Exclude", exact: true }).click();
      await expect(
        card.locator('[data-host-exclusion="cursor"]'),
      ).toBeVisible();
      expect(api.hosts[0].excluded_names).toEqual(["cursor"]);
      await expectCleanLayout(page, "exclusion saved");

      await card
        .getByRole("button", { name: "Allow cursor on workstation-a again" })
        .click();
      await expect(card.locator('[data-host-exclusion="cursor"]')).toHaveCount(
        0,
      );
      await card.getByRole("button", { name: "Done" }).click();

      await card.getByRole("button", { name: "Revoke host…" }).click();
      const revoke = card.locator("[data-host-revoke-confirm]");
      await expect(revoke).toContainText("all 3 of its agents");
      const submit = revoke.getByRole("button", { name: "Revoke host" });
      await expect(submit).toBeDisabled();
      await revoke
        .getByLabel("Type workstation-a to confirm")
        .fill("workstation");
      await expect(submit).toBeDisabled();
      await expectCleanLayout(page, "revoke confirmation", bothEnds);

      api.fail.revokeHost = { message: "revoke refused by policy" };
      await revoke
        .getByLabel("Type workstation-a to confirm")
        .fill("workstation-a");
      await submit.click();
      await expect(revoke.getByText("revoke refused by policy")).toBeVisible();
      await expectCleanLayout(page, "revoke failed");

      api.fail = {};
      await submit.click();
      await expect(
        page.getByRole("button", { name: /Show 1 revoked host/ }),
      ).toBeVisible();
      expect(api.calls).toContain("revokeHost");
      await expectCleanLayout(page, "host revoked", bothEnds);
    });

    test("headless enrollment token", async ({ page }) => {
      const api = await installAccessApi(page, { pending: [] });
      await page.goto(ACCESS_PATH);
      await page.getByRole("button", { name: "Enroll a machine" }).click();
      await expect(page.locator("[data-host-enroll-command]")).toContainText(
        "host enroll",
      );
      await page
        .getByPlaceholder("e.g. GitHub Actions runner")
        .fill("GitHub Actions runner");
      await page.getByRole("button", { name: "Create token" }).click();
      const created = page.locator("[data-host-token-created]");
      await expect(created).toContainText(api.hostToken);
      await expect(created.locator("[data-host-token-command]")).toContainText(
        `host enroll --token ${api.hostToken}`,
      );
      await expectCleanLayout(page, "token created", bothEnds);

      await created.getByRole("button", { name: "Copy command" }).click();
      expect(
        await page.evaluate(() => navigator.clipboard.readText()),
      ).toContain(api.hostToken);

      await page
        .locator('[data-host-token="htok_1"]')
        .getByRole("button", { name: "Revoke" })
        .click();
      await expect(page.locator('[data-host-token="htok_1"]')).toHaveCount(0);
      await page.getByRole("button", { name: /used or expired/ }).click();
      await expect(page.locator('[data-host-token="htok_1"]')).toContainText(
        "revoked",
      );
      await expectCleanLayout(page, "token revoked", bothEnds);
    });

    test("invite a person", async ({ page }) => {
      const api = await installAccessApi(page, { pending: [] });
      await page.goto(ACCESS_PATH);
      api.fail.createInvite = { message: "invite quota exceeded ".repeat(4) };
      await page.getByRole("button", { name: "Invite a person" }).click();
      await expect(page.getByText(/invite quota exceeded/)).toBeVisible();
      await expectCleanLayout(page, "invite failed");

      api.fail = {};
      await page.getByRole("button", { name: "Invite a person" }).click();
      const banner = page.locator("[data-invite-token-banner]");
      await expect(banner).toContainText(api.createdToken);
      expect(
        api.invites.every((invite) => invite.kind === "human"),
      ).toBeTruthy();
      await expectCleanLayout(page, "invite created", bothEnds);

      await page
        .locator(`[data-invite="${api.invites[1].id}"]`)
        .getByRole("button", { name: "Revoke" })
        .click();
      await page
        .getByRole("dialog", { name: "Revoke invite" })
        .getByRole("button", { name: "Revoke" })
        .click();
      await expect(
        page.locator(`[data-invite="${api.invites[1].id}"]`),
      ).toHaveCount(0);
      await expectCleanLayout(page, "invite revoked");
    });

    test("external mode sends people to the account", async ({ page }) => {
      await installAccessApi(page, { pending: [] });
      await gotoAccessAsHosted(page);
      await expect(
        page.getByRole("link", { name: "your account" }),
      ).toBeVisible();
      await expect(
        page.getByRole("button", { name: "Invite a person" }),
      ).toHaveCount(0);
      await expectCleanLayout(page, "hosted", bothEnds);
    });

    test("revoke a person and break glass", async ({ page }) => {
      const api = await installAccessApi(page, { pending: [] });
      await page.goto(ACCESS_PATH);
      await page
        .locator('[data-principal="agent-second-human"]')
        .getByRole("button", { name: "Revoke…" })
        .click();
      const dialog = page.getByRole("dialog");
      await expect(dialog).toBeVisible();
      await expectCleanLayout(page, "confirm revoke", bothEnds);

      // Core reports the last human: the dialog escalates to break-glass.
      api.fail.revokePrincipal = {
        status: 409,
        body: {
          error: {
            code: "last_active_principal",
            message: "last_active_principal",
            details: "last_active_principal",
          },
        },
      };
      await dialog.getByRole("button", { name: "Revoke access" }).click();
      const confirm = page.getByRole("button", {
        name: "Allow lockout and revoke",
      });
      await expect(confirm).toBeDisabled();
      await expectCleanLayout(page, "break glass", bothEnds);

      api.fail = {};
      await dialog
        .getByRole("textbox", { name: /to confirm$/ })
        .fill("agent-second-human");
      await page
        .getByLabel("Lockout reason")
        .fill("Recovery via bootstrap token held by ops. ".repeat(3));
      await expect(confirm).toBeEnabled();
      await confirm.click();
      await expect(confirm).toHaveCount(0);
      expect(api.calls).toContain("revokePrincipal");
      await expectCleanLayout(page, "break glass done", bothEnds);
    });

    test("tour arrival banner", async ({ page }) => {
      await installAccessApi(page, {
        principals: [HUMANS[0]],
        hosts: [],
        pending: [],
        invites: [],
        audit: [],
        agents: [],
      });
      await page.goto(`${ACCESS_PATH}?from=tour#hosts`);
      await expect(
        page.getByText("Last step: connect the machine your agents run on"),
      ).toBeVisible();
      await expectCleanLayout(page, "tour banner", bothEnds);
    });
  });
}
