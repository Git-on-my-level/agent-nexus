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
  { latency = LATENCY_MS, failOpenAfterAnswer = false } = {},
) {
  const digest = await getExpectedCommandRegistryDigest();
  /** @type {{ path: string, at: number }[]} */
  const calls = [];
  const started = Date.now();
  const state = { responded: false, historyReads: 0 };
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
          work: SCALE_WORK.slice(start, start + limit),
          next_cursor:
            start + limit < SCALE_WORK.length ? String(start + limit) : "",
        };
      }
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
      if (path.endsWith("/messages")) return { messages: [] };
      if (path.endsWith("/context")) return { recent_events: [] };
      if (path === "/events") return { events: [], page_info: {} };
      return null;
    })();

    calls.push({ path, at: Date.now() - started, wallAt: Date.now() });
    await new Promise((resolve) => setTimeout(resolve, latency));
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
test("Inbox asks are interactive before the bounded scale history finishes", async ({
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
    expect(interactiveMs).toBeLessThan(1000);
    expect(earlyWorkPages).toBeLessThan(8);
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
