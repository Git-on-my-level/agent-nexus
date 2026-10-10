import { expect, test } from "@playwright/test";
import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";
import { expectCleanLayout } from "../helpers/layoutAudit.js";

const ROOT = "/o/local/w/local";
async function setup(page) {
  const now = Date.now();
  const agent = {
    id: "agent-one",
    actor_id: "actor-one",
    handle: "reviewer.local",
    name: "reviewer",
    display_name: "Review agent",
    identity_kind: "standalone",
    state: "working",
    bridge_online: false,
    current_card_ref: "card:release",
    current_card_title: "Check the release evidence",
    last_signal_at: new Date(now).toISOString(),
  };
  const work = {
    ref: "card:release",
    title: "Check the release evidence",
    summary:
      "Two sessions can contribute without taking ownership of this task.",
    source: { authority: "nexus" },
    phase: "review",
    definition_of_done: ["A human reviews the acceptance evidence"],
    next_action: "Review the linked test results",
    freshness: { status: "unknown" },
  };
  const record = (id) => ({
    participant_id: id,
    agent_id: agent.id,
    actor_id: agent.actor_id,
    activity: "active",
    active: true,
    last_seen_at: new Date(now - 30_000).toISOString(),
    expires_at: new Date(now + 120_000).toISOString(),
    session_id: "PRIVATE_SESSION",
    native_session_id: "/private/provider/transcript.json",
  });
  const state = {
    participants: [record("p1"), record("p2")],
    failed: false,
    calls: [],
  };
  await installWorkspaceApi(page, {
    agents: [agent],
    actors: [{ id: agent.actor_id, display_name: agent.display_name }],
    work: [work],
  });
  await page.route("**/*", async (route) => {
    const request = route.request();
    if (!["fetch", "xhr"].includes(request.resourceType()))
      return route.fallback();
    const path = decodeURIComponent(new URL(request.url()).pathname);
    const reply = (body, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    if (path.startsWith("/work/") || path.startsWith("/sessions"))
      state.calls.push({ path, method: request.method() });
    if (path === "/work/card:release/participants")
      return state.failed
        ? reply(
            {
              error: {
                code: "not_found",
                message: "PRIVATE_SESSION /private/provider/transcript.json",
              },
            },
            404,
          )
        : reply({ participants: state.participants, next_cursor: "" });
    if (path === "/work/card:release/observations")
      return reply({
        observations: [
          {
            id: "obs1",
            actor_id: agent.actor_id,
            reader_id: "test-reader",
            status: "reported",
            verification: "reported",
            observed_at: new Date(now - 60_000).toISOString(),
            evidence: [
              {
                kind: "check_run",
                url: "https://example.com/test-results",
                summary: "Acceptance tests passed",
              },
            ],
          },
        ],
        next_cursor: "",
      });
    if (path === "/work/card:release") return reply({ work });
    if (path === "/agents/agent-one" || path === "/agents/reviewer.local")
      return reply({
        agent,
        recent_cards: [{ ref: work.ref, title: work.title }],
        recent_runs: [
          {
            id: "run1",
            ref: "run:one",
            state: "completed",
            adapter: "generic",
            started_at: new Date(now - 180_000).toISOString(),
            ended_at: new Date(now - 120_000).toISOString(),
            card_ref: work.ref,
          },
        ],
        recent_notes: [],
        open_asks: [],
      });
    return route.fallback();
  });
  return state;
}

for (const viewport of [
  { width: 1440, height: 1000 },
  { width: 900, height: 1000 },
  { width: 375, height: 812 },
]) {
  test(`task and agent participation states @ ${viewport.width}`, async ({
    page,
  }, testInfo) => {
    test.setTimeout(60_000);
    await page.setViewportSize(viewport);
    const state = await setup(page);
    await page.goto(`${ROOT}/tasks/card%3Arelease`);
    const panel = page.getByRole("region", {
      name: "Participation",
      exact: true,
    });
    await expect(
      panel.getByText("2 sessions participating · 2 currently active"),
    ).toBeVisible();
    await expect(panel.getByText("Session 1")).toBeVisible();
    await expect(panel.getByText("Session 2")).toBeVisible();
    await expect(
      page.getByLabel("Evidence for handoff").getByText("Reported claim"),
    ).toBeVisible();
    await expect(page.locator("body")).not.toContainText("PRIVATE_SESSION");
    await expect(page.locator("body")).not.toContainText("/private/provider");
    await expectCleanLayout(page, "task concurrent participation");
    await panel.screenshot({
      path: testInfo.outputPath(`task-participation-${viewport.width}.png`),
    });
    await page.screenshot({
      path: testInfo.outputPath(`task-page-${viewport.width}.png`),
      fullPage: true,
    });

    state.participants[0].expires_at = new Date(
      Date.now() - 1_000,
    ).toISOString();
    state.participants[1].activity = "closed";
    await panel.getByRole("button", { name: "Refresh activity" }).click();
    await expect(
      panel.getByText("1 session participating · 0 currently active"),
    ).toBeVisible();
    await expect(panel.getByText("Stale · activity unknown")).toBeVisible();
    await expect(panel.getByText("Session closed")).toBeVisible();
    await expectCleanLayout(page, "stale and closed sessions");

    state.failed = true;
    await panel.getByRole("button", { name: "Refresh activity" }).click();
    await expect(panel.getByText(/Participation unavailable/)).toBeVisible();
    await expect(panel.locator("[data-participation-summary]")).toHaveCount(0);
    await expect(page.locator("body")).not.toContainText("PRIVATE_SESSION");
    await expectCleanLayout(page, "private or unavailable participation");
    state.failed = false;
    state.participants[0].expires_at = new Date(
      Date.now() + 120_000,
    ).toISOString();
    state.participants[1].activity = "active";

    await page.goto(`${ROOT}/agents/agent-one`);
    const agentPanel = page.getByRole("region", {
      name: "Recent task participation",
      exact: true,
    });
    await expect(
      agentPanel.getByText("2 task participations · 2 currently active"),
    ).toBeVisible();
    // The coverage bound is on the heading's tip now, not a paragraph under
    // it: a sentence a reader needs once should not be on screen every visit.
    await expect(
      agentPanel.getByRole("button", { name: /What participation means/ }),
    ).toHaveAttribute("data-tooltip", /private sessions are not included/);
    await expect(
      agentPanel.getByRole("link", { name: workTitle() }),
    ).toBeVisible();
    await expectCleanLayout(page, "agent task-scoped participation");
    await agentPanel.screenshot({
      path: testInfo.outputPath(`agent-participation-${viewport.width}.png`),
    });
    await page.screenshot({
      path: testInfo.outputPath(`agent-page-${viewport.width}.png`),
      fullPage: true,
    });
    expect(state.calls.every((call) => call.method === "GET")).toBe(true);
    expect(state.calls.some((call) => call.path.startsWith("/sessions"))).toBe(
      false,
    );
  });
}
function workTitle() {
  return "Check the release evidence";
}
