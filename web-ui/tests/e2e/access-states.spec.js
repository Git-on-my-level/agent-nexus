import { expect, test } from "@playwright/test";

import { AUDIT_VIEWPORTS, expectCleanLayout } from "../helpers/layoutAudit.js";

/**
 * Walks the Access page through every UI state it can reach (loading, errors,
 * each step of the invite / revoke flows, popovers, banners) at several
 * viewport sizes and runs the geometry audit after each transition.
 */

const ACCESS_PATH = "/o/local/w/local/access";
const LONG_HASH =
  "1fb951be68b4aa395611181d1d7af40857ea6c038e26393eab6433516f2ee888";
const SELF_AGENT_ID = `agent_ext_${LONG_HASH}`;

function wakeRouting(state, handle) {
  const summaries = {
    online: `Online as @${handle}.`,
    offline: `Offline. @${handle} is still taggable; wake notifications queue until the bridge checks in again.`,
    unregistered: `@${handle} has not registered for wake routing in this workspace yet.`,
  };
  return {
    applicable: true,
    handle,
    taggable: state !== "unregistered",
    online: state === "online",
    state,
    summary: summaries[state],
  };
}

function principal(overrides) {
  return {
    actor_id: `actor-${overrides.agent_id}`,
    principal_kind: "agent",
    auth_method: "public_key",
    created_at: "2026-03-01T10:00:00Z",
    last_seen_at: "2026-03-20T11:15:00Z",
    updated_at: "2026-03-28T10:00:00Z",
    revoked: false,
    ...overrides,
  };
}

const SELF_PRINCIPAL = principal({
  agent_id: SELF_AGENT_ID,
  // Hosted humans arrive with a synthesized, very long username.
  username: `external.${LONG_HASH.slice(0, 54)}`,
  principal_kind: "human",
  auth_method: "external_grant",
});

const POPULATED_PRINCIPALS = [
  SELF_PRINCIPAL,
  principal({
    agent_id: "agent-hermes",
    username: "m4-hermes",
    wake_routing: wakeRouting("online", "m4-hermes"),
  }),
  principal({
    agent_id: "agent-offline-with-a-really-long-identifier-0123456789abcdef",
    username: "offline-agent-with-a-very-long-username-that-keeps-going",
    wake_routing: wakeRouting(
      "offline",
      "offline-agent-with-a-very-long-username-that-keeps-going",
    ),
  }),
  principal({
    agent_id: "agent-second-human",
    username: "riley@example.com",
    principal_kind: "human",
    auth_method: "passkey",
  }),
  principal({
    agent_id: "agent-revoked",
    username: "retired-bot",
    revoked: true,
  }),
  // No username: the row falls back to the raw agent id.
  principal({ agent_id: `agent_${LONG_HASH}` }),
  principal({
    agent_id: "agent-unregistered",
    username: "fresh-agent",
    wake_routing: wakeRouting("unregistered", "fresh-agent"),
  }),
];

const POPULATED_INVITES = [
  {
    id: "invite_61ab15c4-e615-4c7d-8116-3a212e1fe301",
    kind: "agent",
    created_at: "2026-03-28T10:00:00Z",
  },
  {
    id: "invite_consumed-0000-4c7d-8116-3a212e1fe302",
    kind: "human",
    created_at: "2026-03-20T10:00:00Z",
    consumed_at: "2026-03-21T10:00:00Z",
  },
  {
    id: "invite_revoked-0000-4c7d-8116-3a212e1fe303",
    kind: "any",
    created_at: "2026-03-19T10:00:00Z",
    revoked_at: "2026-03-19T12:00:00Z",
  },
];

const AUDIT_EVENT_TYPES = [
  "invite_created",
  "invite_consumed",
  "invite_revoked",
  "principal_registered",
  "principal_revoked",
  "principal_self_revoked",
  "principal_human_lockout_revoked",
  "bootstrap_consumed",
  "some_future_event_type",
];

const POPULATED_AUDIT = AUDIT_EVENT_TYPES.flatMap((event_type, i) => [
  // Long raw ids on both sides (no usernames).
  {
    event_id: `authevt_91e4b6b0-b80f-4a1b-a9fa-8d67634bf6a${i}`,
    event_type,
    occurred_at: "2026-03-28T10:00:00Z",
    actor_agent_id: SELF_AGENT_ID,
    subject_agent_id: `agent_${LONG_HASH}`,
    invite_id: "invite_61ab15c4-e615-4c7d-8116-3a212e1fe301",
  },
  // Usernames present, no invite id.
  {
    event_id: `authevt_named_${i}`,
    event_type,
    occurred_at: "2026-03-27T10:00:00Z",
    actor_username: "riley@example.com",
    actor_agent_id: "agent-second-human",
    subject_username: "m4-hermes",
    subject_agent_id: "agent-hermes",
  },
]);

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
    principals: POPULATED_PRINCIPALS,
    principalsNextCursor: "",
    activeHumans: 2,
    invites: POPULATED_INVITES,
    audit: POPULATED_AUDIT,
    auditNextCursor: "",
    createdToken: "oinv_yeJecICpb-7U8xbmFtTzivIiQg2TI32f",
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

  await page.route(/\/auth\/principals(\?.*)?$/, (route) => {
    const cursor = new URL(route.request().url()).searchParams.get("cursor");
    return respond(route, cursor ? "principalsMore" : "principals", () => ({
      principals: cursor
        ? api.principals.slice(0, 3).map((p, i) => ({
            ...p,
            agent_id: `${p.agent_id}-page2-${i}`,
            username: p.username ? `${p.username}-2` : "",
          }))
        : api.principals,
      active_human_principal_count: api.activeHumans,
      next_cursor: cursor ? "" : api.principalsNextCursor,
    }));
  });

  await page.route(/\/auth\/invites$/, (route) => {
    if (route.request().method() === "POST") {
      return respond(route, "createInvite", () => {
        const kind = route.request().postDataJSON()?.kind ?? "agent";
        api.invites = [
          {
            id: `invite_created-${api.invites.length}-4c7d-8116-3a212e1fe3ff`,
            kind,
            created_at: "2026-03-29T10:00:00Z",
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
          ? { ...invite, revoked_at: "2026-03-29T11:00:00Z" }
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
  await expect(page.getByRole("heading", { name: "Access" })).toBeVisible();
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
      api.hold.principals = deferred();
      api.hold.invites = deferred();
      api.hold.audit = deferred();
      await page.goto(ACCESS_PATH);
      await expect(page.getByRole("heading", { name: "Access" })).toBeVisible();
      await expectCleanLayout(page, "initial loading");

      // Sections resolve one at a time.
      api.hold.invites.resolve();
      api.hold.principals.resolve();
      await expectCleanLayout(page, "partially loaded");
      api.hold.audit.resolve();
      api.hold = {};
      await expect(page.getByText("m4-hermes", { exact: true })).toBeVisible();
      await expectCleanLayout(page, "populated", bothEnds);

      await page.getByRole("button", { name: /Show 2 resolved/ }).click();
      await expectCleanLayout(page, "resolved invites shown", bothEnds);
      await page.getByRole("button", { name: "Hide resolved" }).click();

      // Refresh while populated: content must stay put and clean.
      api.hold.principals = deferred();
      await page.getByRole("button", { name: /Refresh/ }).click();
      await expectCleanLayout(page, "refreshing over populated data");
      api.hold.principals.resolve();
      api.hold = {};

      api.fail = {
        principals: { message: "principals backend unavailable ".repeat(6) },
        invites: { message: "invites backend unavailable" },
        audit: { message: "audit backend unavailable" },
      };
      await page.getByRole("button", { name: /Refresh/ }).click();
      await expect(page.getByText("invites backend unavailable")).toBeVisible();
      await expectCleanLayout(page, "all sections failed", bothEnds);

      api.fail = {};
      api.principals = [SELF_PRINCIPAL];
      api.invites = [];
      api.audit = [];
      await page.getByRole("button", { name: /Refresh/ }).click();
      await expect(page.getByText("No invites yet.")).toBeVisible();
      await expectCleanLayout(page, "empty workspace", bothEnds);
    });

    test("paginated principals and audit events", async ({ page }) => {
      const api = await installAccessApi(page, {
        principalsNextCursor: "cursor-2",
        auditNextCursor: "cursor-2",
      });
      await page.goto(ACCESS_PATH);
      const loadMore = page.getByRole("button", { name: "Load more" });
      await expect(loadMore).toHaveCount(2);
      await expectCleanLayout(page, "load more available", bothEnds);

      api.hold.principalsMore = deferred();
      await loadMore.first().click();
      await expectCleanLayout(page, "loading more principals");
      api.hold.principalsMore.resolve();
      await expect(loadMore).toHaveCount(1);

      api.fail.auditMore = { message: "audit page two failed" };
      await loadMore.click();
      await expect(page.getByText("audit page two failed")).toBeVisible();
      await expectCleanLayout(page, "load more failed", bothEnds);
    });

    test("create invite flow (self-hosted form)", async ({ page }) => {
      const api = await installAccessApi(page);
      await page.goto(ACCESS_PATH);
      const create = page.getByRole("button", { name: "Create invite" });
      await expect(create).toBeVisible();

      await page.getByLabel(/Agent profile name/).fill("Claude Code");
      await expect(page.getByLabel(/Agent username/)).toHaveValue(
        "claude-code",
      );
      await expectCleanLayout(page, "form filled");

      api.fail.createInvite = {
        message: "invite quota exceeded for this workspace ".repeat(4),
      };
      await create.click();
      await expect(page.getByText(/invite quota exceeded/)).toBeVisible();
      await expectCleanLayout(page, "create invite failed");

      api.fail = {};
      api.hold.createInvite = deferred();
      await create.click();
      await expect(
        page.getByRole("button", { name: "Creating..." }),
      ).toBeVisible();
      await expectCleanLayout(page, "creating invite");
      api.hold.createInvite.resolve();
      api.hold = {};

      await expect(page.getByText("Invite created successfully")).toBeVisible();
      await expect(page.getByText(api.createdToken)).toBeVisible();
      await expectCleanLayout(page, "agent invite created", bothEnds);
      // The form stays usable while the token is on screen.
      await page.evaluate(() => window.scrollTo(0, 0));
      await expect(create).toBeEnabled();
      await create.click({ trial: true });

      await page.getByRole("button", { name: "Copy instructions" }).click();
      await expect(
        page.getByRole("button", { name: "Instructions copied" }),
      ).toBeVisible();
      expect(
        await page.evaluate(() => navigator.clipboard.readText()),
      ).toContain(api.createdToken);
      await expectCleanLayout(page, "instructions copied", bothEnds);

      await page.getByRole("button", { name: "Dismiss token banner" }).click();
      await expect(page.getByText("Invite created successfully")).toHaveCount(
        0,
      );
      await expectCleanLayout(page, "token dismissed");

      // Agent invite without a username: placeholder copy is longer.
      await create.click();
      await expect(page.getByText(/username placeholder/)).toBeVisible();
      await expectCleanLayout(page, "agent invite without username", bothEnds);

      // A second invite while the first token is showing replaces it.
      await page.getByLabel("Kind").selectOption("human");
      await expectCleanLayout(page, "human kind selected");
      await create.click();
      await expect(
        page.getByRole("button", { name: "Copy token" }),
      ).toBeVisible();
      await expectCleanLayout(page, "human invite created", bothEnds);
      await page.getByRole("button", { name: "Copy token" }).click();
      await expect(
        page.getByRole("button", { name: "Token copied" }),
      ).toBeVisible();
      await expectCleanLayout(page, "token copied");

      await page.getByLabel("Kind").selectOption("any");
      await expectCleanLayout(page, "any kind selected over banner");
      await create.click();
      await expect(
        page.getByRole("button", { name: "Copy instructions" }),
      ).toBeVisible();
      await expectCleanLayout(page, "any invite created", bothEnds);

      // A failure after a success must not leave a stale token on screen.
      api.fail.createInvite = { message: "second create failed" };
      await create.click();
      await expect(page.getByText("second create failed")).toBeVisible();
      await expect(page.getByText("Invite created successfully")).toHaveCount(
        0,
      );
      await expectCleanLayout(page, "failure after success");
    });

    test("create invite flow (hosted form) with a very long token", async ({
      page,
    }) => {
      const api = await installAccessApi(page, {
        createdToken: `oinv_${"yeJecICpb7U8xbmFtTzivIiQg2TI32f".repeat(6)}`,
      });
      await gotoAccessAsHosted(page);
      await expect(
        page.getByRole("link", { name: "your Organizations" }),
      ).toBeVisible();
      await expect(page.getByLabel("Kind").locator("option")).toHaveCount(1);
      await expectCleanLayout(page, "hosted form", bothEnds);

      await page.getByLabel(/Agent username/).fill("claude-code");
      await page.getByRole("button", { name: "Create invite" }).click();
      await expect(page.getByText("Invite created successfully")).toBeVisible();
      await expect(page.getByText(api.createdToken)).toBeVisible();
      await expectCleanLayout(page, "hosted invite created", bothEnds);

      await page.getByRole("button", { name: "Copy instructions" }).click();
      await expectCleanLayout(page, "hosted instructions copied", bothEnds);
    });

    test("revoke invite flow", async ({ page }) => {
      const api = await installAccessApi(page);
      await page.goto(ACCESS_PATH);
      const revoke = page
        .getByRole("main")
        .getByRole("button", { name: "Revoke", exact: true })
        .first();
      await revoke.click();
      const dialog = page.getByRole("dialog", { name: "Revoke invite" });
      await expect(dialog).toBeVisible();
      await expectCleanLayout(page, "revoke invite modal");

      await dialog.getByRole("button", { name: "Cancel" }).click();
      await expect(dialog).toHaveCount(0);
      await expectCleanLayout(page, "revoke invite cancelled");

      api.fail.revokeInvite = { message: "invite already consumed" };
      await revoke.click();
      await dialog.getByRole("button", { name: "Revoke" }).click();
      await expect(page.getByText("invite already consumed")).toBeVisible();
      await expectCleanLayout(page, "revoke invite failed");

      api.fail = {};
      api.hold.revokeInvite = deferred();
      await revoke.click();
      await dialog.getByRole("button", { name: "Revoke" }).click();
      await expect(
        page.getByRole("button", { name: "Revoking..." }),
      ).toBeVisible();
      await expectCleanLayout(page, "revoking invite");
      api.hold.revokeInvite.resolve();
      api.hold = {};
      await expect(page.getByText("No pending invites.")).toBeVisible();
      await expectCleanLayout(page, "invite revoked", bothEnds);
    });

    test("revoke principal flow", async ({ page }) => {
      const api = await installAccessApi(page);
      await page.goto(ACCESS_PATH);
      const row = page
        .getByRole("main")
        .locator("div.group\\/row")
        .filter({ hasText: "m4-hermes" });
      await row.getByRole("button", { name: "Revoke" }).click();
      const revokeDialog = page.getByRole("dialog", {
        name: "Revoke principal",
      });
      await expect(revokeDialog).toBeVisible();
      await expectCleanLayout(page, "confirm principal revoke", bothEnds);

      await page.getByRole("button", { name: "Cancel" }).click();
      await expect(revokeDialog).toHaveCount(0);
      await expectCleanLayout(page, "principal revoke cancelled");

      api.fail.revokePrincipal = {
        message: "revocation rejected by policy ".repeat(5),
      };
      await row.getByRole("button", { name: "Revoke" }).click();
      await page.getByRole("button", { name: "Confirm revoke" }).click();
      await expect(page.getByText(/revocation rejected/)).toBeVisible();
      await expectCleanLayout(page, "principal revoke failed", bothEnds);

      api.fail = {};
      api.hold.revokePrincipal = deferred();
      await page.getByRole("button", { name: "Confirm revoke" }).click();
      await expect(
        page.getByRole("button", { name: "Revoking…" }),
      ).toBeVisible();
      await expectCleanLayout(page, "revoking principal");
      api.hold.revokePrincipal.resolve();
      api.hold = {};
      await expect(revokeDialog).toHaveCount(0);
      await expectCleanLayout(page, "principal revoked", bothEnds);

      // Server reports last-human lockout: escalates to break-glass.
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
      const humanRow = page
        .getByRole("main")
        .locator("div.group\\/row")
        .filter({ hasText: "riley@example.com" });
      await humanRow.getByRole("button", { name: "Revoke" }).click();
      await page.getByRole("button", { name: "Confirm revoke" }).click();
      await expect(
        page.getByRole("dialog", { name: "Last active human principal" }),
      ).toBeVisible();
      await expectCleanLayout(page, "break glass escalation", bothEnds);
    });

    test("break-glass revoke of the last human", async ({ page }) => {
      const otherHuman = principal({
        agent_id: `agent_human_${LONG_HASH}`,
        username: "only-other-human@example.com",
        principal_kind: "human",
        auth_method: "passkey",
      });
      const api = await installAccessApi(page, {
        self: {
          agent_id: "agent-hermes",
          actor_id: "a",
          username: "m4-hermes",
        },
        principals: [POPULATED_PRINCIPALS[1], otherHuman],
        activeHumans: 1,
      });
      await page.goto(ACCESS_PATH);
      await page.getByRole("button", { name: "Break glass" }).click();
      const confirm = page.getByRole("button", {
        name: "Allow human lockout and revoke",
      });
      await expect(confirm).toBeDisabled();
      await expectCleanLayout(page, "break glass empty", bothEnds);

      await page
        .getByRole("dialog")
        .getByRole("textbox", { name: /to confirm$/ })
        .fill(otherHuman.agent_id);
      await page
        .getByLabel("Human lockout reason")
        .fill("Recovery via bootstrap token held by ops. ".repeat(3));
      await expect(confirm).toBeEnabled();
      await expectCleanLayout(page, "break glass ready", bothEnds);

      api.fail.revokePrincipal = { message: "lockout revoke refused" };
      await confirm.click();
      await expect(page.getByText("lockout revoke refused")).toBeVisible();
      await expectCleanLayout(page, "break glass failed", bothEnds);

      api.fail = {};
      await confirm.click();
      await expect(confirm).toHaveCount(0);
      await expectCleanLayout(page, "break glass done", bothEnds);
    });

    test("wake routing popovers", async ({ page }) => {
      await installAccessApi(page);
      await page.goto(ACCESS_PATH);
      const main = page.getByRole("main");

      await main.getByText("How wake routing works").click();
      await expectCleanLayout(page, "wake routing help expanded");

      for (const label of ["Online", "Offline", "Unregistered"]) {
        const badge = main.getByRole("button", { name: label, exact: true });
        await badge.scrollIntoViewIfNeeded();
        await badge.click();
        await expect(main.getByRole("tooltip")).toBeVisible();
        await expectCleanLayout(page, `${label} popover open`);
        if (label === "Unregistered") {
          await page
            .getByRole("button", { name: "Copy registration steps" })
            .click();
          await expect(
            page.getByRole("button", { name: "Copied", exact: true }),
          ).toBeVisible();
          await expectCleanLayout(page, "wake registration copied");
        }
        await page.keyboard.press("Escape");
        await expect(main.getByRole("tooltip")).toHaveCount(0);
      }

      // Popover and the revoke confirmation can be open together.
      await main.getByRole("button", { name: "Online", exact: true }).click();
      await main
        .locator("div.group\\/row")
        .filter({ hasText: "m4-hermes" })
        .getByRole("button", { name: "Revoke" })
        .click();
      await expectCleanLayout(page, "popover with revoke confirmation");
    });

    test("tour arrival banner", async ({ page }) => {
      const api = await installAccessApi(page, {
        principals: [SELF_PRINCIPAL],
        invites: [],
        audit: [],
      });
      await page.goto(`${ACCESS_PATH}?invite=agent&from=tour`);
      await expect(
        page.getByText("Last step: connect your first agent"),
      ).toBeVisible();
      await expect(page).toHaveURL(/\/access$/);
      await expectCleanLayout(page, "tour banner", bothEnds);

      await page.getByRole("button", { name: "Create invite" }).click();
      await expect(page.getByText("Invite created successfully")).toBeVisible();
      await expect(
        page.getByText("Last step: connect your first agent"),
      ).toHaveCount(0);
      expect(api.calls).toContain("createInvite");
      await expectCleanLayout(page, "tour invite created", bothEnds);
    });
  });
}
