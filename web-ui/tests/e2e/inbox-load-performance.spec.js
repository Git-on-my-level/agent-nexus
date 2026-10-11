import { expect, test } from "@playwright/test";
import { getExpectedCommandRegistryDigest } from "../../src/lib/commandRegistryDigest.js";
import { EXPECTED_SCHEMA_VERSION } from "../../src/lib/config.js";
import { expectPerf } from "../helpers/e2ePerf.js";
import { holdOpenStream } from "../helpers/openStream.js";
import { nextPaint } from "../helpers/pageReady.js";

/**
 * The "+" on a mailbox count says the number is a lower bound; this is
 * the glyph that explains it. It replaced a sentence printed in the
 * middle of the mailbox row.
 */
const lowerBoundTip = (page) =>
  page.getByRole("button", { name: /Why the counts end in \+/ });

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
    expireWorkAfterFirst = false,
    completedRecords = [],
    openRecords = [ASK],
    inboxPages,
    actionPages,
  } = {},
) {
  const digest = await getExpectedCommandRegistryDigest();
  /** @type {{ path: string, at: number }[]} */
  const calls = [];
  const started = Date.now();
  const state = {
    responded: false,
    respondStarted: false,
    historyReads: 0,
    archived: [],
    inboxPages,
  };
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
    if (path.startsWith("/stream/")) return holdOpenStream(page, route);

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
      state.respondStarted = true;
      if (gates[path]) await gates[path].promise;
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
        return (
          state.inboxPages?.[
            `${url.searchParams.get("status") || "open"}:${url.searchParams.get("cursor") || ""}`
          ] || {
            items:
              url.searchParams.get("status") === "completed" || state.responded
                ? state.responded
                  ? []
                  : completedRecords
                : openRecords,
            status: url.searchParams.get("status") || "open",
          }
        );
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
      if (path === "/pm/actions")
        return (
          actionPages?.[url.searchParams.get("cursor") || ""] || {
            items: [],
            has_more: false,
          }
        );
      if (path === "/artifacts") return { artifacts: [] };
      if (path === "/docs") return { documents: [] };
      if (path === "/topics") return { topics: [] };
      if (path === "/threads") return { threads: [] };
      if (path.endsWith("/messages")) return { messages: [] };
      if (path.endsWith("/context")) return { recent_events: [] };
      if (path === "/events") return { events: [], page_info: {} };
      return null;
    })();

    const cursor = url.searchParams.get("cursor") || "";
    const status = url.searchParams.get("status") || "";
    calls.push({
      path,
      cursor,
      status,
      at: Date.now() - started,
      wallAt: Date.now(),
    });
    const gate = gates[`${path}:${status}:${cursor}`] || gates[path];
    if (gate) await gate.promise;
    await new Promise((resolve) => setTimeout(resolve, latency));
    if (path === "/work" && cursor && expireWorkAfterFirst)
      return route.fulfill({
        status: 401,
        contentType: "application/json",
        body: JSON.stringify({
          error: { code: "invalid_token", message: "Session ended" },
        }),
      });
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
test("Inbox paints first pages without waiting for eight scale-work pages", async ({
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
  // The first ranked paint waits for first pages only. The millisecond cap
  // is host-sensitive, so only the advisory performance job enforces it.
  expectPerf(() => {
    expect(interactiveMs).toBeLessThan(1500);
  });
  expect(earlyWorkPages).toBeLessThanOrEqual(3);
  await expect
    .poll(() => calls.filter((call) => call.path === "/work").length)
    .toBe(8);
  await expect(lowerBoundTip(page)).toBeVisible();
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

async function observeRefreshRows(page) {
  await page.evaluate(() => {
    window.__refreshRows = [];
    const capture = () =>
      window.__refreshRows.push({
        ids: [...document.querySelectorAll("[data-inbox-row]")].map(
          (row) => row.dataset.inboxRow,
        ),
        selected: document.querySelector(
          '[data-inbox-row][aria-current="page"]',
        )?.dataset.inboxRow,
        detail: [...document.querySelectorAll("h2,h3")].map((heading) =>
          heading.textContent.trim(),
        ),
      });
    capture();
    new MutationObserver(capture).observe(document.body, {
      subtree: true,
      childList: true,
      attributes: true,
    });
  });
}

test("partial archive refresh preserves a validated task, detail, and ordering", async ({
  page,
}) => {
  const gates = {};
  await installScaleCore(page, {
    latency: 0,
    gates,
    openRecords: [],
    workRecords: [
      blockedTask("before", { movedDays: 1 }),
      blockedTask("survivor", { movedDays: 1 }),
      blockedTask("stale"),
    ],
  });
  await page.goto(`${ROOT}/inbox?item=task:card:survivor`);
  const selected = page.getByTestId("inbox-row-task:card:survivor");
  await expect(selected).toHaveAttribute("aria-current", "page");
  await expect(lowerBoundTip(page)).toHaveCount(0);
  const before = await page
    .locator("[data-inbox-row]")
    .evaluateAll((rows) => rows.map((row) => row.dataset.inboxRow));
  gates["/inbox:completed:"] = deferred();
  await page.getByRole("button", { name: "Stale (1)", exact: true }).click();
  await observeRefreshRows(page);
  await page
    .getByRole("button", { name: "Archive Blocked stale", exact: true })
    .click();
  await expect(lowerBoundTip(page)).toBeVisible();
  await expect(selected).toHaveAttribute("aria-current", "page");
  await expect(
    page.getByRole("heading", { name: "Blocked survivor", exact: true }),
  ).toBeVisible();
  gates["/inbox:completed:"].resolve();
  await expect(lowerBoundTip(page)).toHaveCount(0);
  await expect(selected).toHaveAttribute("aria-current", "page");
  expect(
    await page
      .locator("[data-inbox-row]")
      .evaluateAll((rows) => rows.map((row) => row.dataset.inboxRow)),
  ).toEqual(before);
  const snapshots = await page.evaluate(() => window.__refreshRows);
  expect(
    snapshots.every(
      (snapshot) =>
        snapshot.ids.includes("task:card:survivor") &&
        JSON.stringify(
          snapshot.ids.filter((id) => id !== "task:card:stale"),
        ) === JSON.stringify(before) &&
        snapshot.selected === "task:card:survivor" &&
        snapshot.detail.includes("Blocked survivor"),
    ),
  ).toBe(true);
});

test("committing one answer does not retain another answer whose Undo is pending", async ({
  page,
}) => {
  const second = {
    ...ASK,
    id: "inbox:second",
    title: "Second ask",
    subject_ref: "card:second",
    related_refs: ["card:second", "thread:second"],
    thread_id: "second",
  };
  const gate = deferred();
  const calls = await installScaleCore(page, {
    latency: 0,
    gates: { [`/inbox/${ASK.id}/respond`]: gate },
    openRecords: [ASK, second],
    workRecords: [],
    failOpenAfterAnswer: true,
  });
  await page.clock.install();
  await page.goto(`${ROOT}/inbox?item=${encodeURIComponent(ASK.id)}`);
  await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toBeVisible();
  await page.getByTestId(`inbox-row-${ASK.id}`).click();
  await expect(
    page.getByRole("heading", { name: ASK.title, exact: true }),
  ).toBeVisible();
  await page
    .getByRole("textbox", { name: "Reply", exact: true })
    .fill("Answer A");
  await page.getByRole("button", { name: "Send reply", exact: true }).click();
  await page.clock.runFor(5_001);
  await expect.poll(() => calls.state.respondStarted).toBe(true);
  await page.getByTestId(`inbox-row-${second.id}`).click();
  await expect(
    page.getByRole("heading", { name: second.title, exact: true }),
  ).toBeVisible();
  await page
    .getByRole("textbox", { name: "Reply", exact: true })
    .fill("Answer B");
  await page.getByRole("button", { name: "Send reply", exact: true }).click();
  await page.clock.pauseAt(
    new Date(await page.evaluate(() => Date.now() + 1_000)),
  );
  const confirmed = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      response.url().includes(`/inbox/${encodeURIComponent(ASK.id)}/respond`),
  );
  gate.resolve();
  await expect.poll(() => calls.state.responded).toBe(true);
  // responded flips before the body is released. Wait for that response, then
  // one turn, so the refresh timer exists before the clock jumps. Animation
  // frames do not run while the clock is paused.
  await confirmed;
  await page.evaluate(() => {});
  await page.clock.runFor(1_000);
  await expect.poll(() => calls.state.historyReads).toBeGreaterThan(1);
  await page.clock.resume();
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await expect(page.getByTestId(`inbox-row-${second.id}`)).toBeVisible();
  await expect(page.getByRole("button", { name: /^1 Proceed/ })).toBeEnabled();
  await page.clock.fastForward(61_000);
  await expect(page.getByTestId(`inbox-row-${second.id}`)).toBeVisible();
  await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toHaveCount(0);
});

for (const completedStillPartial of [true, false]) {
  test(`refresh retains known answers while open is pending and completed is ${completedStillPartial ? "partial" : "complete"}`, async ({
    page,
  }) => {
    const gates = {};
    const answered = blockedTask("scale-0", { movedDays: 1 });
    answered.updated_at = stamp(6);
    const calls = await installScaleCore(page, {
      latency: 0,
      gates,
      workRecords: [answered, blockedTask("stale")],
      inboxPages: {
        "completed:": { items: [], next_cursor: "next" },
        "completed:next": {
          items: [
            {
              ...ASK,
              id: "answer",
              inbox_item_id: ASK.id,
              status: "completed",
              responded_at: stamp(1),
            },
          ],
        },
      },
    });
    await page.goto(`${ROOT}/inbox`);
    await expect(
      page.getByRole("button", { name: "Stale (1)", exact: true }),
    ).toBeVisible();
    await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toHaveCount(0);
    await expect(lowerBoundTip(page)).toHaveCount(0);
    gates["/inbox:open:"] = deferred();
    if (completedStillPartial) gates["/inbox:completed:next"] = deferred();
    else
      calls.state.inboxPages = {
        "completed:": { items: [] },
        "open:": { items: [] },
      };
    await page.getByRole("button", { name: "Stale (1)", exact: true }).click();
    await observeRefreshRows(page);
    await page
      .getByRole("button", { name: "Archive Blocked stale", exact: true })
      .click();
    await expect(lowerBoundTip(page)).toBeVisible();
    await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toHaveCount(0);
    await expect(page.getByTestId("inbox-row-task:card:scale-0")).toHaveCount(
      0,
    );
    if (completedStillPartial) gates["/inbox:completed:next"].resolve();
    // Keep the open snapshot pending: even a finished completed read cannot
    // discard known answers until the newer open snapshot can replace it.
    await nextPaint(page);
    expect(
      (await page.evaluate(() => window.__refreshRows)).every(
        (snapshot) =>
          !snapshot.ids.includes(ASK.id) &&
          !snapshot.ids.includes("task:card:scale-0"),
      ),
    ).toBe(true);
    gates["/inbox:open:"].resolve();
    await expect(lowerBoundTip(page)).toHaveCount(0);
    await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toHaveCount(0);
    if (!completedStillPartial)
      await expect(
        page.getByTestId("inbox-row-task:card:scale-0"),
      ).toBeVisible();
  });
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

test("late work and decisions append below the shown ask and preserve selection and j/k", async ({
  page,
}) => {
  const gates = { "/work": deferred(), "/pm/decisions": deferred() };
  await installScaleCore(page, {
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
  const ask = page.getByTestId(`inbox-row-${ASK.id}`);
  await expect(ask).toBeVisible();
  await ask.click();
  const before = await ask.boundingBox();
  gates["/work"].resolve();
  await expect(page.getByTestId("inbox-row-task:card:recent")).toBeVisible();
  gates["/pm/decisions"].resolve();
  const rows = page.locator("[data-inbox-row]");
  await expect(rows).toHaveCount(3);
  expect(
    await rows.evaluateAll((list) => list.map((row) => row.dataset.inboxRow)),
  ).toEqual([ASK.id, "task:card:recent", "decision:priority-decision"]);
  await expect(ask).toHaveAttribute("aria-current", "page");
  expect((await ask.boundingBox()).y).toBe(before.y);
  await expect(page.locator("[data-inbox-more-loaded]")).toHaveText(
    "3 more loaded",
  );
  await page.locator("body").click({ position: { x: 5, y: 5 } });
  await page.keyboard.press("j");
  await expect(page.getByTestId("inbox-row-task:card:recent")).toHaveAttribute(
    "aria-current",
    "page",
  );
  await page.keyboard.press("k");
  await expect(ask).toHaveAttribute("aria-current", "page");
  await expect(
    page.getByRole("button", { name: "Stale (1)", exact: true }),
  ).toHaveAttribute("aria-expanded", "false");
});

test("a never-resolving unread feed cannot hold asks or revive an answered task", async ({
  page,
}) => {
  test.setTimeout(30_000);
  const answered = blockedTask("answered", { movedDays: 1 });
  answered.updated_at = stamp(6);
  const calls = await installScaleCore(page, {
    latency: 0,
    gates: { "/home/unread": { promise: new Promise(() => {}) } },
    workRecords: [answered],
    completedRecords: [
      {
        id: "completed:answered",
        status: "completed",
        kind: "ask",
        subject_ref: answered.ref,
        responded_at: stamp(1),
      },
    ],
  });
  await page.clock.install();
  await page.goto(`${ROOT}/inbox`);
  await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toBeVisible();
  await expect(page.getByRole("button", { name: /^1 Proceed/ })).toBeEnabled();
  const enabled = await page.evaluate(() => window.__inboxInteractive);
  const sourceStart = calls.find((call) => call.path === "/inbox").wallAt;
  expectPerf(() => {
    expect(enabled.wallAt - sourceStart).toBeLessThan(1500);
  });
  await expect(page.getByTestId("inbox-row-task:card:answered")).toHaveCount(0);
  await expect(
    page.getByText("Inbox loading timed out", { exact: false }),
  ).toHaveCount(0);
  // The old five-second deadline must no longer terminate a cold read.
  // Advance the page clock instead of sleeping through wall time.
  await expect(page.locator("[data-inbox-loading]")).toHaveCount(0);
  await page.clock.fastForward(5_200);
  await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toBeVisible();
  await expect(page.getByTestId("inbox-row-task:card:answered")).toHaveCount(0);
  await expect(lowerBoundTip(page)).toBeVisible();
});

test("stalled completed history keeps work suppressed even after the final deadline", async ({
  page,
}) => {
  test.setTimeout(70_000);
  const gate = { promise: new Promise(() => {}) };
  await installScaleCore(page, {
    latency: 0,
    gates: { "/inbox:completed:": gate },
    workRecords: [blockedTask("possibly-answered", { movedDays: 1 })],
  });
  await page.goto(`${ROOT}/inbox?item=task:card:possibly-answered`);
  await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toBeVisible();
  await expect(
    page.getByText("Loading requested item…", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByTestId("inbox-row-task:card:possibly-answered"),
  ).toHaveCount(0);
  await expect(
    page.getByText(
      "This item could not be loaded. Retry or open the task directly.",
    ),
  ).toBeVisible({ timeout: 55_000 });
  await expect(
    page.getByTestId("inbox-row-task:card:possibly-answered"),
  ).toHaveCount(0);
  await expect(
    page.getByText("This item is not in the loaded mailbox."),
  ).toHaveCount(0);
});

test("a slow second work page appends below the first page and an expanded stale selection", async ({
  page,
}) => {
  const gate = deferred();
  const workRecords = [
    blockedTask("stale"),
    ...Array.from({ length: 49 }, (_, i) => ({
      ...CARD,
      ref: `card:filler-${i}`,
    })),
    { ...blockedTask("late-priority", { movedDays: 1 }), priority: "p0" },
  ];
  await installScaleCore(page, {
    latency: 0,
    gates: { "/work::50": gate },
    workRecords,
  });
  await page.goto(`${ROOT}/inbox?item=task:card:stale`);
  const selected = page.getByTestId("inbox-row-task:card:stale");
  await expect(selected).toHaveAttribute("aria-current", "page");
  await expect(page.getByRole("button", { name: /^1 Proceed/ })).toHaveCount(0);
  const before = await selected.boundingBox();
  gate.resolve();
  const late = page.getByTestId("inbox-row-task:card:late-priority");
  await expect(late).toBeVisible();
  expect((await selected.boundingBox()).y).toBe(before.y);
  expect((await late.boundingBox()).y).toBeGreaterThan(before.y);
  await expect(selected).toHaveAttribute("aria-current", "page");
  await expect(page.locator("[data-inbox-more-loaded]")).toHaveText(
    "1 more loaded",
  );
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
    test.setTimeout(65_000);
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
    ).toBeVisible({ timeout: failWork ? 45_000 : 10_000 });
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
    ).toBeVisible({ timeout: failWork ? 45_000 : 10_000 });
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

test("an arrived verified receipt stays handled while the next action page stalls", async ({
  page,
}) => {
  test.setTimeout(70_000);
  await installScaleCore(page, {
    latency: 0,
    decisions: [
      {
        ...slowDecision,
        id: "known-receipt",
        status: "answered",
        action_id: "verified-action",
      },
    ],
    actionPages: {
      "": {
        items: [
          {
            id: "verified-action",
            decision_id: "known-receipt",
            status: "verified",
            receipt: { independently_verified: true },
          },
        ],
        next_cursor: "slow",
      },
    },
    gates: { "/pm/actions::slow": { promise: new Promise(() => {}) } },
    workRecords: [],
  });
  await page.goto(`${ROOT}/inbox`);
  await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toBeVisible();
  await expect(
    page.getByTestId("inbox-row-decision:known-receipt"),
  ).toHaveCount(0);
  await page.getByRole("link", { name: /^Handled/ }).click();
  await expect(
    page.getByTestId("inbox-row-decision:known-receipt"),
  ).toBeVisible();
  await page.getByRole("link", { name: /^Needs you/ }).click();
  await expect(
    page.getByRole("button", { name: "Retry", exact: true }),
  ).toBeVisible({ timeout: 55_000 });
  await expect(
    page.getByTestId("inbox-row-decision:known-receipt"),
  ).toHaveCount(0);
});

test("a never-resolving requested decision leaves loading with an incomplete-result message", async ({
  page,
}) => {
  await installScaleCore(page, {
    latency: 0,
    workRecords: [],
    gates: { "/pm/decisions/missing": { promise: new Promise(() => {}) } },
  });
  await page.goto(`${ROOT}/inbox?item=decision:missing`);
  await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toBeVisible();
  await expect(
    page.getByText("Loading requested item…", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("This item is not in the loaded mailbox."),
  ).toHaveCount(0);
  await expect(
    page.getByText(
      "This item could not be loaded. Retry or open the task directly.",
    ),
  ).toBeVisible({ timeout: 10_000 });
  await expect(
    page.getByText("Loading requested item…", { exact: true }),
  ).toHaveCount(0);
});

test("an expired session on a later work page removes rows and offers sign-in recovery", async ({
  page,
}) => {
  await installScaleCore(page, { latency: 0, expireWorkAfterFirst: true });
  await page.goto(`${ROOT}/inbox`);
  await expect(
    page.getByRole("button", { name: "Sign in again", exact: true }),
  ).toBeVisible();
  await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toHaveCount(0);
});

for (const status of [401, 403]) {
  test(`Inbox purges cached rows after ${status}`, async ({ page }) => {
    await installScaleCore(page, { latency: 0, workRecords: [] });
    await page.goto(`${ROOT}/inbox`);
    await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toBeVisible();
    await expect
      .poll(() =>
        page.evaluate(() =>
          localStorage.getItem("anx.workspace-views.v1")?.includes("scale-ask"),
        ),
      )
      .toBe(true);
    await page.route("**/inbox?**", (route) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify({ error: { message: "Read permission denied" } }),
      }),
    );
    await page.reload();
    await expect(page.getByRole("alert").first()).toBeVisible();
    await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toHaveCount(0);
    await expect
      .poll(() =>
        page.evaluate(
          () =>
            localStorage
              .getItem("anx.workspace-views.v1")
              ?.includes("scale-ask") || false,
        ),
      )
      .toBe(false);
  });
}

test("a confirmed answer stays gone after reload with failing reads", async ({
  page,
}) => {
  const calls = await installScaleCore(page, {
    latency: 0,
    failOpenAfterAnswer: true,
    workRecords: [SCALE_WORK[0]],
  });
  await page.goto(`${ROOT}/inbox`);
  await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(() =>
        localStorage.getItem("anx.workspace-views.v1")?.includes("scale-ask"),
      ),
    )
    .toBe(true);
  await page.getByRole("button", { name: /^1 Proceed/ }).click();
  await expect
    .poll(() => calls.state.responded, { timeout: 15_000 })
    .toBe(true);
  await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toHaveCount(0);
  // A real reload loses the response overlay and hydrates persisted sources.
  await page.reload();
  await expect(
    page.getByText("History temporarily unavailable").first(),
  ).toBeVisible();
  await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toHaveCount(0);
  await expect(
    page.locator('[data-inbox-row="task:card:scale-0"]'),
  ).toHaveCount(0);
  await expect(page.locator("[data-inbox-nav-count]")).toHaveCount(0);
  await expect(page.getByRole("button", { name: /^1 Proceed/ })).toHaveCount(0);
});

test("a selected decision denial revokes cached Inbox rows", async ({
  page,
}) => {
  await installScaleCore(page, { latency: 0, workRecords: [] });
  await page.goto(`${ROOT}/inbox`);
  await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(() =>
        localStorage.getItem("anx.workspace-views.v1")?.includes("scale-ask"),
      ),
    )
    .toBe(true);
  await page.route("**/pm/decisions/private-decision", (route) =>
    route.fulfill({
      status: 403,
      contentType: "application/json",
      body: JSON.stringify({
        error: { message: "Decision permission denied" },
      }),
    }),
  );
  await page.goto(`${ROOT}/inbox?item=decision:private-decision`);
  await expect(page.getByRole("alert").first()).toBeVisible();
  await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toHaveCount(0);
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          localStorage
            .getItem("anx.workspace-views.v1")
            ?.includes("scale-ask") || false,
      ),
    )
    .toBe(false);
});

test("a persisted visit paints immediately while Inbox is slow", async ({
  page,
}) => {
  await installScaleCore(page, { latency: 0, workRecords: [] });
  await page.goto(`${ROOT}/inbox`);
  await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toBeVisible();
  await expect
    .poll(() =>
      page.evaluate(() =>
        localStorage.getItem("anx.workspace-views.v1")?.includes("scale-ask"),
      ),
    )
    .toBe(true);
  const revalidation = deferred();
  await page.route("**/inbox?**", async (route) => {
    await revalidation.promise;
    await route.fallback();
  });
  await page.reload();
  await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toBeVisible();
  await expect(page.getByText("Refreshing…", { exact: true })).toBeVisible();
  revalidation.resolve();
});

test("a cold Inbox keeps the known badge as one skeleton until confirmed", async ({
  page,
}) => {
  await installScaleCore(page, { latency: 0, workRecords: [] });
  await page.goto(`${ROOT}/tasks`);
  await expect(page.locator("[data-inbox-nav-count]").first()).toHaveText("1");
  // Simulate a badge without a cached payload (for example storage was denied).
  await page.evaluate(() => localStorage.removeItem("anx.workspace-views.v1"));
  const slowInbox = deferred();
  await page.route("**/inbox?**", async (route) => {
    await slowInbox.promise;
    await route.fallback();
  });
  // A full reload clears memory; inject just the last count through the existing store.
  await page.evaluate(async () => {
    const cache = await import("/src/lib/workspaceViewCache.js");
    cache.clearWorkspaceViews();
  });
  await page.getByRole("link", { name: "Inbox", exact: true }).first().click();
  await expect(page.locator("[data-inbox-loading]")).toBeVisible();
  await expect(page.locator("[data-inbox-loading] > div > div")).toHaveCount(1);
  await expect(
    page.getByText("No items loaded yet.", { exact: true }),
  ).toHaveCount(0);
  await expect(page.locator("[data-inbox-nav-count]").first()).toHaveText("1");
  slowInbox.resolve();
});

test("a 504 followed by success reconnects without a red error", async ({
  page,
}) => {
  await installScaleCore(page, { latency: 0, workRecords: [] });
  let attempts = 0;
  await page.route("**/inbox?**", async (route) => {
    if (
      new URL(route.request().url()).searchParams.get("status") === "open" &&
      attempts++ === 0
    ) {
      await route.fulfill({
        status: 504,
        contentType: "application/json",
        body: JSON.stringify({ error: { message: "gateway unavailable" } }),
      });
    } else await route.fallback();
  });
  await page.goto(`${ROOT}/inbox`);
  await expect(page.getByText("Reconnecting…", { exact: true })).toBeVisible();
  await expect(page.getByTestId(`inbox-row-${ASK.id}`)).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Retry", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByText("No items loaded yet.", { exact: true }),
  ).toHaveCount(0);
});

test("sustained 504 failures eventually offer Retry while keeping skeletons", async ({
  page,
}) => {
  test.setTimeout(65_000);
  await installScaleCore(page, { latency: 0, workRecords: [] });
  await page.route("**/inbox?**", (route) =>
    route.fulfill({
      status: 504,
      contentType: "application/json",
      body: JSON.stringify({ error: { message: "gateway unavailable" } }),
    }),
  );
  await page.goto(`${ROOT}/inbox`);
  await expect(page.getByText("Reconnecting…", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Retry", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Retry", exact: true }),
  ).toBeVisible({ timeout: 45_000 });
  await expect(page.locator("[data-inbox-loading]")).toBeVisible();
  await expect(page.getByText("You're clear.", { exact: true })).toHaveCount(0);
});
