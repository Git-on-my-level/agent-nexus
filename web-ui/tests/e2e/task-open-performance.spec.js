import { expect, test } from "@playwright/test";

import { getExpectedCommandRegistryDigest } from "../../src/lib/commandRegistryDigest.js";
import { EXPECTED_SCHEMA_VERSION } from "../../src/lib/config.js";
import { expectPerf } from "../helpers/e2ePerf.js";
import { holdOpenStream } from "../helpers/openStream.js";
import { nextPaint } from "../helpers/pageReady.js";

/**
 * How long it takes to open a task card, and how many round trips it costs.
 *
 * Opening a card used to be slow for a structural reason rather than a slow
 * endpoint: the page re-read `/auth/session` before it would ask for anything,
 * then read the card, then the plan, then the refs — four hops deep, with a
 * blank page for all of them, for a card whose title the reader had been
 * looking at a second earlier in the list.
 *
 * Every mocked endpoint here answers after the same fixed delay, so what the
 * numbers measure is the *shape* of the waterfall — how many hops the reader
 * waits through — rather than this machine's speed. The budget is stated in
 * hops, and the wall-clock time is printed so a change in either is visible.
 *
 * Run it on its own to see the figures:
 *   pnpm exec playwright test --project=default tests/e2e/task-open-performance.spec.js
 */

const ROOT = "/o/local/w/local";
const TASKS = `${ROOT}/tasks`;

/**
 * One simulated round trip. Every endpoint costs exactly this.
 *
 * Deliberately larger than the framework's own cost on a dev server, so a hop
 * saved is visible in the figure rather than lost in compile and mount noise.
 */
const LATENCY_MS = 400;

const CARD = {
  ref: "card:tune-combat",
  handle: "tune-combat",
  id: "0199a1e1-0000-7000-8000-000000000001",
  title: "Tune core combat loop for the vertical slice",
  phase: "in_progress",
  priority: "p1",
  board_ref: "board-1",
  owner: "actor-operator",
  summary: "The parry window needs a pass before the slice is playable.",
  next_action: "Measure the input buffer at 120 ms",
  definition_of_done: ["The parry window is measured and recorded"],
  blockers: [],
  relations: [],
  executions: [],
  labels: [],
  source: { authority: "nexus" },
  freshness: { status: "fresh", last_observed_at: stamp(0.2) },
  updated_at: stamp(0.2),
  created_at: stamp(72),
};

/** A second card, so the list is a list and the click has a target to pick. */
const OTHER = {
  ...CARD,
  ref: "card:lock-hub",
  handle: "lock-hub",
  id: "0199a1e1-0000-7000-8000-000000000002",
  title: "Lock hub quest path",
};

function stamp(hours) {
  return new Date(
    Date.parse("2026-10-07T12:00:00.000Z") - hours * 3_600_000,
  ).toISOString();
}

const SELF = {
  agent_id: "agent-operator",
  actor_id: "actor-operator",
  username: "operator",
  display_name: "Operator",
  principal_kind: "human",
};

/**
 * The core endpoints a Tasks list and a task card touch, each answering after
 * `LATENCY_MS`. Returns the recorded request log.
 */
async function installSlowCore(page) {
  const digest = await getExpectedCommandRegistryDigest();
  /** @type {{ path: string, at: number }[]} */
  const calls = [];
  const started = Date.now();

  await page.addInitScript(() => {
    localStorage.setItem("workspaceTourSeen.local", "1");
    localStorage.setItem("workspaceTourSeen.local:local", "1");
  });
  // Gate inside fetch, before a socket opens. Holding the network response
  // instead fills the per-host connection pool, and the rest of the same
  // wave then cannot be sent until something finishes — which looks like a
  // second hop.
  await page.addInitScript(() => {
    const original = window.fetch.bind(window);
    const sent = [];
    let held = false;
    /** @type {Array<() => void>} */
    const waiters = [];

    /**
     * @param {RequestInfo | URL} input
     */
    function pathname(input) {
      const raw =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.href
            : input?.url;
      if (!raw) return "";
      try {
        return decodeURIComponent(new URL(raw, location.origin).pathname);
      } catch {
        return "";
      }
    }

    /**
     * @param {string} path
     */
    function isApi(path) {
      if (!path.startsWith("/")) return false;
      if (path.startsWith("/o/")) return false;
      if (path.startsWith("/@")) return false;
      if (path.startsWith("/src/")) return false;
      if (path.startsWith("/node_modules/")) return false;
      if (path.startsWith("/.svelte-kit/")) return false;
      if (path.startsWith("/_app/")) return false;
      if (path.startsWith("/stream/")) return false;
      if (path.includes("__data.json")) return false;
      if (/\.(js|css|svg|png|json|ico|woff2?)$/.test(path)) return false;
      return true;
    }

    window.__taskOpenGate = {
      sent,
      hold() {
        held = true;
      },
      release() {
        held = false;
        for (const resolve of waiters.splice(0)) resolve();
      },
    };

    window.fetch = async (input, init) => {
      const path = pathname(input);
      if (isApi(path)) {
        sent.push(path);
        if (held) await new Promise((resolve) => waiters.push(resolve));
      }
      return original(input, init);
    };
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

    if (request.isNavigationRequest()) {
      calls.push({ path, at: Date.now() - started });
      return route.continue();
    }

    if (
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
      if (path === "/inbox") return { items: [], total: 0 };
      if (path === "/home/unread")
        return {
          groups: [],
          unread_count: 0,
          group_count: 0,
          generated_at: stamp(0),
        };
      if (path === "/work/capabilities")
        return { capabilities: { refresh_executor_configured: true } };
      if (path === "/work") return { work: [CARD, OTHER], next_cursor: "" };
      if (/^\/work\/[^/]+\/observations$/.test(path))
        return { observations: [], next_cursor: "" };
      if (/^\/work\/[^/]+$/.test(path)) return { work: CARD };
      if (/^\/cards\/[^/]+\/plan$/.test(path)) return { plan: null };
      if (path === "/refs/resolve") return { refs: {} };
      if (path === "/pm/decisions") return { items: [], has_more: false };
      if (path === "/pm/actions") return { items: [], has_more: false };
      if (path === "/artifacts") return { artifacts: [] };
      if (path === "/docs") return { documents: [] };
      if (path === "/topics") return { topics: [] };
      if (path === "/threads") return { threads: [] };
      if (path === "/events") return { events: [], page_info: {} };
      return null;
    })();

    const call = { path, at: Date.now() - started, done: false };
    calls.push(call);
    await new Promise((resolve) => setTimeout(resolve, LATENCY_MS));
    if (!body) {
      await route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({ error: { message: `Unmocked ${path}` } }),
      });
    } else {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    }
    call.done = true;
  });

  return calls;
}

/**
 * Yield until fetches issued while the gate is closed stop arriving.
 * A hop that needs a response cannot call fetch until the gate opens.
 * @param {import("@playwright/test").Page} page
 */
async function settleHeldFetches(page) {
  let quiet = 0;
  for (let turn = 0; turn < 20; turn += 1) {
    const seen = await page.evaluate(() => window.__taskOpenGate.sent.length);
    await page.evaluate(
      () =>
        new Promise((resolve) => {
          requestAnimationFrame(() => setTimeout(resolve, 0));
        }),
    );
    const now = await page.evaluate(() => window.__taskOpenGate.sent.length);
    if (now !== seen) {
      quiet = 0;
      continue;
    }
    quiet += 1;
    if (quiet >= 2) return;
  }
}

test("opening a task card costs one round trip, not four", async ({
  page,
}, testInfo) => {
  test.setTimeout(120_000);
  const calls = await installSlowCore(page);

  /*
   * Warm the dev server's on-demand compile of the card route first. It costs
   * seconds on a cold hit and is not what this measures — it does not exist in
   * a built bundle, and it would swamp the figure either way.
   */
  await page.goto(`${ROOT}/tasks/${encodeURIComponent(OTHER.ref)}`);
  await expect(page.getByRole("heading", { name: CARD.title })).toBeVisible({
    timeout: 60_000,
  });

  await page.goto(TASKS);
  const row = page.locator('[data-work-ref="card:tune-combat"] a').first();
  await expect(row.first()).toBeVisible({ timeout: 60_000 });
  await expect(page.getByText("Loading tasks…")).toHaveCount(0);

  const navBefore = calls.length;
  const callMark = calls.length;
  const clickedAt = Date.now();
  const fetchBefore = await page.evaluate(() => {
    window.__taskOpenGate.hold();
    return window.__taskOpenGate.sent.length;
  });
  await row.click();

  // The reader's own definition of "open": the card's title is on screen.
  const heading = page.getByRole("heading", { name: CARD.title });
  await expect(heading).toBeVisible({ timeout: 30_000 });
  const titleMs = Date.now() - clickedAt;
  /*
   * These reads used to wait on each other: session, then the card, then the
   * plan and the PM lists. Each one has to be issued while every response is
   * still held. A read that needs another response cannot show up here, so
   * the poll times out instead of calling that chain one wave.
   */
  await expect
    .poll(
      async () => {
        const sent = await page.evaluate(
          (start) => window.__taskOpenGate.sent.slice(start),
          fetchBefore,
        );
        return {
          card: sent.some((path) => /^\/work\/[^/]+$/.test(path)),
          plan: sent.some((path) => /^\/cards\/[^/]+\/plan$/.test(path)),
          observations: sent.some((path) => /\/observations$/.test(path)),
          participants: sent.some((path) => /\/participants$/.test(path)),
          decisions: sent.includes("/pm/decisions"),
          actions: sent.includes("/pm/actions"),
        };
      },
      { timeout: 15_000 },
    )
    .toEqual({
      card: true,
      plan: true,
      observations: true,
      participants: true,
      decisions: true,
      actions: true,
    });
  const concurrent = await page.evaluate(
    (start) => window.__taskOpenGate.sent.slice(start),
    fetchBefore,
  );

  // And "loaded": the page has stopped saying it is still reading.
  await page.evaluate(() => window.__taskOpenGate.release());
  await expect(page.getByText("Reading the card…")).toHaveCount(0, {
    timeout: 30_000,
  });
  const readMs = Date.now() - clickedAt;
  // Chip resolution waits on the plan response. Snapshot only after that
  // response is delivered, or a follow-up /refs/resolve is still pending
  // and the check below never sees it.
  await expect
    .poll(() =>
      calls
        .slice(callMark)
        .some((call) => /^\/cards\/[^/]+\/plan$/.test(call.path) && call.done),
    )
    .toBe(true);
  await settleHeldFetches(page);
  const afterPlan = await page.evaluate(
    (start) => window.__taskOpenGate.sent.slice(start),
    fetchBefore + concurrent.length,
  );
  const cardReadAfterResponse = afterPlan.filter(
    (path) =>
      /^\/work\/[^/]+$/.test(path) ||
      /^\/cards\/[^/]+\/plan$/.test(path) ||
      /\/observations$/.test(path) ||
      /\/participants$/.test(path) ||
      path === "/refs/resolve",
  );

  const report = [
    `simulated latency per request: ${LATENCY_MS}ms`,
    `click → title visible:        ${titleMs}ms`,
    `click → card read:            ${readMs}ms`,
    `concurrent requests:          ${concurrent.length}`,
    `card reads that waited:       ${cardReadAfterResponse.length}`,
    `paths: ${concurrent.join(", ")}`,
    `next: ${afterPlan.join(", ")}`,
  ].join("\n");
  testInfo.annotations.push({ type: "task-open-profile", description: report });
  console.log(`\n--- task open profile ---\n${report}\n`);

  /*
   * A shell refresh can still be scheduled after the gate opens. That is not
   * the card waterfall. A card read that waited for a response lands here,
   * and the poll above has already required the first copy while the gate
   * was closed.
   */
  expect(cardReadAfterResponse).toEqual([]);

  /*
   * And the title is on screen inside that one wave, because it is painted
   * from the row the list already had rather than waited for. The millisecond
   * caps include Playwright scheduling, so only the advisory job enforces them.
   */
  expectPerf(() => {
    expect(titleMs).toBeLessThan(LATENCY_MS * 2);
    expect(readMs).toBeLessThan(LATENCY_MS * 3);
  });

  // Client-side navigation: no document request for the card page.
  expect(
    calls.slice(navBefore).filter((call) => call.path.startsWith("/o/")),
  ).toHaveLength(0);
});

test("pointing at a row reads the card before the click", async ({ page }) => {
  test.setTimeout(120_000);
  const calls = await installSlowCore(page);

  await page.goto(TASKS);
  const row = page.locator('[data-work-ref="card:tune-combat"] a').first();
  await expect(row).toBeVisible({ timeout: 60_000 });
  await expect(page.getByText("Loading tasks…")).toHaveCount(0);

  const before = calls.length;
  await row.hover();
  await expect
    .poll(() =>
      calls.slice(before).some((call) => /^\/work\/[^/]+$/.test(call.path)),
    )
    .toBe(true);

  const prefetched = calls.length;
  await row.click();
  await expect(page.getByRole("heading", { name: CARD.title })).toBeVisible({
    timeout: 30_000,
  });

  // A second hover adds nothing: the card is cached, not re-read.
  // The watcher stays up through hover and the turns that follow it. A
  // one-second timeout started beforehand expires while hover is still
  // waiting to be actionable, and a real prefetch then goes unseen.
  await page.goBack();
  await expect(row).toBeVisible({ timeout: 30_000 });
  let cardReads = 0;
  const onRequest = (request) => {
    const path = decodeURIComponent(new URL(request.url()).pathname);
    if (/^\/work\/[^/]+$/.test(path)) cardReads += 1;
  };
  page.on("request", onRequest);
  await row.hover();
  await nextPaint(page);
  await page.evaluate(() => new Promise((resolve) => setTimeout(resolve, 0)));
  page.off("request", onRequest);
  expect(cardReads).toBe(0);
  expect(prefetched).toBeGreaterThan(before);
});
