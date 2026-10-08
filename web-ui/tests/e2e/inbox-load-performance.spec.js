import { expect, test } from "@playwright/test";
import { getExpectedCommandRegistryDigest } from "../../src/lib/commandRegistryDigest.js";
import { EXPECTED_SCHEMA_VERSION } from "../../src/lib/config.js";

const ROOT = "/o/local/w/local";
const LATENCY_MS = 400;
const ASK = {
  id: "inbox:scale-ask",
  kind: "ask",
  title: "Which rollout should ship?",
  subject_ref: "card:scale-0",
  related_refs: ["card:scale-0", "thread:scale-ask"],
  thread_id: "scale-ask",
  requester_actor_id: "agent-builder",
  response_proposals: ["Proceed", "Wait"],
  source_event_time: stamp(24),
  notification_target_status: { resolvable: false },
};
const CARD = {
  ref: "card:scale-card",
  title: "Scale card",
  phase: "in_progress",
  source: { authority: "nexus" },
  updated_at: stamp(2),
};
const SCALE_WORK = Array.from({ length: 4096 }, (_, index) => ({
  ...CARD,
  ref: `card:scale-${index}`,
  title: `Scale task ${index}`,
}));
SCALE_WORK[0] = {
  ...SCALE_WORK[0],
  phase: "blocked",
  next_actor: "human:operator",
};
function stamp(hours) {
  return new Date(Date.now() - hours * 3_600_000).toISOString();
}

const SELF = {
  agent_id: "agent-operator",
  actor_id: "actor-operator",
  username: "operator",
  display_name: "Operator",
  principal_kind: "human",
};

/**
 * The bounded Inbox feeds and selected-item context, each answering after
 * `LATENCY_MS`. Returns the recorded request log.
 */
async function installScaleCore(
  page,
  {
    latency = LATENCY_MS,
    failOpenAfterAnswer = false,
    workRecords = SCALE_WORK,
    decisions = [],
    gates = {},
    failWork = false,
  } = {},
) {
  const digest = await getExpectedCommandRegistryDigest();
  /** @type {{ path: string, at: number }[]} */
  const calls = [];
  const started = Date.now();
  const state = { responded: false, historyReads: 0, archived: [] };
  calls.state = state;

  await page.addInitScript(() => {
    const observer = new MutationObserver(() => {
      const button = document.querySelector('[data-inbox-proposal="1"]');
      if (!button || button.disabled || !button.getClientRects().length) return;
      window.__inboxInteractive = { at: performance.now(), wallAt: Date.now() };
      observer.disconnect();
    });
    observer.observe(document, {
      childList: true,
      subtree: true,
      attributes: true,
    });
    localStorage.setItem("workspaceTourSeen.local", "1");
    localStorage.setItem("workspaceTourSeen.local:local", "1");
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

  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = decodeURIComponent(url.pathname);

    if (
      request.isNavigationRequest() ||
      path.startsWith("/o/") ||
      path.startsWith("/@") ||
      path.startsWith("/src/") ||
      path.startsWith("/node_modules/") ||
      path.startsWith("/.svelte-kit/") ||
      path.startsWith("/_app/") ||
      path.includes("__data.json") ||
      /\.(js|css|svg|png|json|ico|woff2?)$/.test(path)
    ) {
      return route.continue();
    }

    // The event streams must not be delayed or counted: they stay open.
    if (path.startsWith("/stream/")) {
      return route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        body: ": keepalive\n\n",
      });
    }

    if (
      path.startsWith("/cards/") &&
      path.endsWith("/archive") &&
      request.method() === "POST"
    ) {
      const id = path.split("/")[2];
      state.archived.push(`card:${id}`);
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ card: { id, archived_at: stamp(0) } }),
      });
    }
    if (path.endsWith("/respond") && request.method() === "POST") {
      state.responded = true;
      return route.fulfill({
        status: 201,
        contentType: "application/json",
        body: JSON.stringify({
          event: { id: "answer", ts: new Date().toISOString() },
          notify: { queued: false },
        }),
      });
    }
    if (path === "/inbox" && url.searchParams.get("status") === "completed") {
      state.historyReads += 1;
      if (state.responded)
        return route.fulfill({
          status: 500,
          contentType: "application/json",
          body: JSON.stringify({
            error: { message: "History temporarily unavailable" },
          }),
        });
    }
    if (
      path === "/inbox" &&
      url.searchParams.get("status") !== "completed" &&
      state.responded &&
      failOpenAfterAnswer
    )
      return route.fulfill({
        status: 500,
        contentType: "application/json",
        body: JSON.stringify({
          error: { message: "History temporarily unavailable" },
        }),
      });
    const body = (() => {
      if (path === "/meta/handshake" || path === "/version") {
        return {
          schema_version: EXPECTED_SCHEMA_VERSION,
          command_registry_digest: digest,
          api_version: "v1",
          core_version: "synthetic-ui-test",
          dev_actor_mode: false,
          human_auth_mode: "workspace_local",
        };
      }
      if (path === "/auth/session") return { authenticated: true, agent: SELF };
      if (path === "/auth/bootstrap/status")
        return { bootstrap_required: false };
      if (path === "/actors")
        return {
          actors: [
            { id: "actor-operator", display_name: "Operator", tags: ["human"] },
          ],
        };
      if (path === "/auth/principals")
        return { principals: [], next_cursor: "" };
      if (path === "/agents") return { agents: [] };
      if (path === "/boards")
        return { boards: [{ id: "board-1", title: "Delivery" }] };
      if (path === "/inbox")
        return {
          items:
            url.searchParams.get("status") === "completed" || state.responded
              ? []
              : [ASK],
          status: url.searchParams.get("status") || "open",
        };
      if (path === "/home/unread")
        return {
          groups: [],
          unread_count: 0,
          group_count: 0,
          generated_at: stamp(0),
        };
      if (path === "/work/capabilities")
        return { capabilities: { refresh_executor_configured: true } };
      if (path === "/work") {
        const start = Number(url.searchParams.get("cursor") || 0);
        const limit = Number(url.searchParams.get("limit") || 50);
        return {
          work: workRecords
            .filter((item) => !state.archived.includes(item.ref))
            .slice(start, start + limit),
          archived_refs: state.archived,
          next_cursor:
            start + limit < workRecords.length ? String(start + limit) : "",
        };
      }
      if (/^\/work\/[^/]+\/observations$/.test(path))
        return { observations: [], next_cursor: "" };
      if (/^\/work\/[^/]+$/.test(path)) return { work: CARD };
      if (/^\/cards\/[^/]+\/plan$/.test(path)) return { plan: null };
      if (path === "/refs/resolve") return { refs: {} };
      if (path === "/pm/decisions")
        return { items: decisions, has_more: false };
      if (path === "/pm/actions") return { items: [], has_more: false };
      if (path === "/artifacts") return { artifacts: [] };
      if (path === "/docs") return { documents: [] };
      if (path === "/topics") return { topics: [] };
      if (path === "/threads") return { threads: [] };
      if (path.endsWith("/messages")) return { messages: [] };
      if (path.endsWith("/context")) return { recent_events: [] };
      if (path === "/events") return { events: [], page_info: {} };
      return null;
    })();

    calls.push({ path, at: Date.now() - started, wallAt: Date.now() });
    if (gates[path]) await gates[path].promise;
    await new Promise((resolve) => setTimeout(resolve, latency));
    if (path === "/work" && failWork)
      return route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ error: { message: "Work feed unavailable" } }),
      });
    if (!body) {
      return route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({ error: { message: `Unmocked ${path}` } }),
      });
    }
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(body),
    });
  });

  return calls;
}

// Measure from the first source request, excluding cold Vite compilation and
// shell authentication. The fixed network delay measures the client waterfall.
test("Inbox paints one complete bounded snapshot without a scale-history reshuffle", async ({
  page,
}, testInfo) => {
  test.setTimeout(120_000);
  const calls = await installScaleCore(page);
  await page.goto(`${ROOT}/inbox`);
  const row = page.getByTestId(`inbox-row-${ASK.id}`);
  await expect(row).toBeVisible();
  await expect(page.getByRole("button", { name: /^1 Proceed/ })).toBeEnabled();
  const inboxStart = calls.find((call) => call.path === "/inbox").at;
  const enabled = await page.evaluate(() => window.__inboxInteractive);
  // Measure the actual enabled DOM control, not the return of Playwright's
  // host-side assertions (which can lag when other checks occupy the machine).
  const resources = await page.evaluate(() =>
    performance
      .getEntriesByType("resource")
      .filter((entry) => new URL(entry.name).pathname === "/inbox")
      .map((entry) => entry.startTime),
  );
  const interactiveMs = Math.round(enabled.at - Math.min(...resources));
  const earlyWorkPages = calls.filter(
    (call) => call.path === "/work" && call.wallAt <= enabled.wallAt,
  ).length;
  const report = {
    fixtureWorkRecords: SCALE_WORK.length,
    latencyMs: LATENCY_MS,
    interactiveMs,
    earlyWorkPages,
    inboxStart,
  };
  console.log(`Inbox scale profile: ${JSON.stringify(report)}`);
  await testInfo.attach("inbox-scale-profile", {
    body: JSON.stringify(report, null, 2),
    contentType: "application/json",
  });
  if (!process.env.ANX_INBOX_PERF_BASELINE) {
    // Eight sequential 400ms work pages remain bounded. The skeleton stays
    // visible until classification is complete; parallel feeds add no waterfall.
    expect(interactiveMs).toBeLessThan(6000);
    expect(earlyWorkPages).toBe(8);
  }
  await expect
    .poll(() => calls.filter((call) => call.path === "/work").length)
    .toBe(8);
  await expect(
    page.getByText("Not everything is loaded; the counts are lower bounds."),
  ).toBeVisible();
  expect(calls.filter((call) => call.path === "/home/unread")).toHaveLength(1);
  expect(calls.filter((call) => call.path === "/pm/decisions")).toHaveLength(1);
});

for (const [answer, failOpenAfterAnswer] of [
  ["proposal", false],
  ["custom", false],
  ["proposal", true],
  ["custom", true],
]) {
  test(`an answered ask and linked card stay out after a ${answer} answer and unavailable ${failOpenAfterAnswer ? "Inbox feeds" : "history"}`, async ({
    page,
  }) => {
    test.setTimeout(60_000);
    const calls = await installScaleCore(page, {
      latency: 0,
      failOpenAfterAnswer,
    });
    await page.clock.install();
    await page.goto(`${ROOT}/inbox`);
    await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toBeVisible();
    if (answer === "proposal")
      await page.getByRole("button", { name: /^1 Proceed/ }).click();
    else {
      await page
        .getByRole("textbox", { name: "Reply", exact: true })
        .fill("Use the smaller rollout first");
      await page
        .getByRole("button", { name: "Send reply", exact: true })
        .click();
    }
    await expect
      .poll(() => calls.state.responded, { timeout: 15_000 })
      .toBe(true);
    await expect.poll(() => calls.state.historyReads).toBeGreaterThan(1);
    await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toHaveCount(0);
    await expect(
      page.locator('[data-inbox-row="task:card:scale-0"]'),
    ).toHaveCount(0);
    await expect(
      page
        .getByText("History temporarily unavailable", { exact: false })
        .first(),
    ).toBeVisible();
    await page.getByRole("link", { name: /^Handled/ }).click();
    await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toBeVisible();
    await page.getByRole("link", { name: /^Needs you/ }).click();
    // A prolonged history outage must not resurrect the card after the
    // bounded local answer overlay expires.
    await page.clock.fastForward(61_000);
    await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toHaveCount(0);
    await expect(
      page.locator('[data-inbox-row="task:card:scale-0"]'),
    ).toHaveCount(0);
  });
}

function deferred() {
  let resolve;
  const promise = new Promise((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function blockedTask(
  id,
  { owner = "", movedDays = 127, source = "nexus" } = {},
) {
  return {
    ref: `card:${id}`,
    title: `Blocked ${id}`,
    phase: "blocked",
    owner,
    source: { authority: source },
    updated_at: stamp(0),
    work_summary: {
      status: {
        state: "blocked",
        label: "Blocked",
        reason: "A step is blocked",
      },
      age: 127 * 86400,
      created_at: stamp(127 * 24),
      last_movement_at: stamp(movedDays * 24),
      ...(owner ? { owner } : {}),
    },
  };
}

const slowDecision = {
  id: "priority-decision",
  status: "awaiting_answer",
  can_answer: true,
  instruction: "Approve release",
  work_ref: "card:decision",
  created_at: stamp(1),
  updated_at: stamp(1),
};

test("slow work and decisions keep skeleton slots until the first ranked rows and selection are stable", async ({
  page,
}) => {
  const gates = { "/work": deferred(), "/pm/decisions": deferred() };
  const calls = await installScaleCore(page, {
    latency: 0,
    gates,
    decisions: [slowDecision],
    workRecords: [
      blockedTask("recent", { movedDays: 1 }),
      blockedTask("stale"),
      { ...CARD, ref: "card:decision", priority: "p0" },
    ],
  });
  await page.goto(`${ROOT}/inbox`);
  await expect(page.locator("[data-inbox-loading]")).toBeVisible();
  await expect
    .poll(() => calls.some((call) => call.path === "/pm/decisions"))
    .toBe(true);
  await expect(page.locator("[data-inbox-row]")).toHaveCount(0);
  gates["/work"].resolve();
  await expect(page.locator("[data-inbox-row]")).toHaveCount(0);
  gates["/pm/decisions"].resolve();
  const rows = page.locator("[data-inbox-row]");
  await expect(rows).toHaveCount(3);
  await expect(rows.first()).toHaveAttribute(
    "data-inbox-row",
    "decision:priority-decision",
  );
  const firstPaint = await rows.evaluateAll((list) =>
    list.map((row) => row.dataset.inboxRow),
  );
  await page.getByTestId(`inbox-row-${ASK.id}`).click();
  await expect(page.getByRole("button", { name: /^1 Proceed/ })).toBeEnabled();
  await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toHaveAttribute(
    "aria-current",
    "page",
  );
  expect(
    await rows.evaluateAll((list) => list.map((row) => row.dataset.inboxRow)),
  ).toEqual(firstPaint);
  await expect(
    page.getByRole("button", { name: "Stale (1)", exact: true }),
  ).toHaveAttribute("aria-expanded", "false");
});

test("a blocked-task deep link loads before resolving and reveals its collapsed stale group", async ({
  page,
}) => {
  const gate = deferred();
  await installScaleCore(page, {
    latency: 0,
    gates: { "/work": gate },
    workRecords: [blockedTask("stale")],
  });
  await page.goto(`${ROOT}/inbox?item=task:card:stale`);
  await expect(
    page.getByText("Loading requested item…", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("This item is not in the loaded mailbox."),
  ).toHaveCount(0);
  gate.resolve();
  await expect(
    page.getByRole("heading", { name: "Blocked stale", exact: true }),
  ).toBeVisible();
  await expect(page.getByTestId("inbox-row-task:card:stale")).toHaveAttribute(
    "aria-current",
    "page",
  );
  await expect(
    page.getByRole("button", { name: "Stale (1)", exact: true }),
  ).toHaveAttribute("aria-expanded", "true");
  await expect(page.getByText("Task age 127d", { exact: false })).toBeVisible();
  await expect(
    page.getByText("This item is not in the loaded mailbox."),
  ).toHaveCount(0);
});

test("only inactive unowned blockers fold below asks, with one-click native archive", async ({
  page,
}) => {
  const calls = await installScaleCore(page, {
    latency: 0,
    workRecords: [
      blockedTask("stale"),
      blockedTask("owned", { owner: "actor:builder" }),
      blockedTask("moved", { movedDays: 1 }),
      blockedTask("external", { source: "external" }),
    ],
  });
  await page.goto(`${ROOT}/inbox`);
  await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toBeVisible();
  await expect(page.getByTestId("inbox-row-task:card:owned")).toBeVisible();
  await expect(page.getByTestId("inbox-row-task:card:moved")).toBeVisible();
  await expect(page.getByTestId("inbox-row-task:card:stale")).toHaveCount(0);
  await page.getByRole("button", { name: "Stale (2)", exact: true }).click();
  await expect(page.getByTestId("inbox-row-task:card:stale")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Archive Blocked external", exact: true }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "Archive Blocked stale", exact: true })
    .click();
  await expect.poll(() => calls.state.archived).toEqual(["card:stale"]);
  await expect(page.getByTestId("inbox-row-task:card:stale")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Stale (1)", exact: true }),
  ).toBeVisible();
  await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toHaveAttribute(
    "aria-current",
    "page",
  );
});

for (const failWork of [false, true]) {
  test(`an absent deep link reports ${failWork ? "an incomplete load" : "absence after loading"}`, async ({
    page,
  }) => {
    const gate = deferred();
    await installScaleCore(page, {
      latency: 0,
      gates: { "/work": gate },
      workRecords: [],
      failWork,
    });
    await page.goto(`${ROOT}/inbox?item=task:card:absent`);
    await expect(
      page.getByText("Loading requested item…", { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText("This item is not in the loaded mailbox."),
    ).toHaveCount(0);
    gate.resolve();
    await expect(
      page.getByText(
        failWork
          ? "This item could not be loaded. Retry or open the task directly."
          : "This item is not in the loaded mailbox.",
        { exact: true },
      ),
    ).toBeVisible();
  });
}

for (const onlyStale of [false, true]) {
  test(`archiving the selected stale task ${onlyStale ? "clears the pin" : "selects a remaining row"} and keeps its confirmation`, async ({
    page,
  }) => {
    await installScaleCore(page, {
      latency: 0,
      workRecords: [blockedTask("stale")],
      decisions: [{ ...slowDecision, id: "same-work", work_ref: "card:stale" }],
    });
    if (onlyStale) {
      // Suppress the ordinary ask too, leaving just the folded blocker.
      await page.route(
        (url) => url.pathname === "/inbox",
        (route) =>
          route.fulfill({
            status: 200,
            contentType: "application/json",
            body: JSON.stringify({ items: [] }),
          }),
      );
    }
    await page.goto(`${ROOT}/inbox?item=task:card:stale`);
    await expect(
      page.getByRole("heading", { name: "Blocked stale", exact: true }),
    ).toBeVisible();
    await page
      .getByRole("button", { name: "Archive Blocked stale", exact: true })
      .click();
    await expect(
      page.getByText("Task archived. You can restore it from Archive.", {
        exact: true,
      }),
    ).toBeVisible();
    await expect(
      page.getByText("This item is not in the loaded mailbox."),
    ).toHaveCount(0);
    await expect(page.getByTestId("inbox-row-task:card:stale")).toHaveCount(0);
    if (onlyStale) {
      await expect(page).not.toHaveURL(/item=/);
      await expect(
        page.getByText("You're clear.", { exact: true }),
      ).toBeVisible();
    } else {
      await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toHaveAttribute(
        "aria-current",
        "page",
      );
      await expect(
        page.getByRole("button", { name: /^1 Proceed/ }),
      ).toBeEnabled();
    }
  });
}
