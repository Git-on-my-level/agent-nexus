import { mkdir } from "node:fs/promises";

import { expect, test } from "@playwright/test";

import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";

/**
 * Before / after screenshots for a review, from one seeded state.
 *
 * Off unless `REVIEW_CAPTURES` is set: it asserts nothing, so it has no place
 * in a CI run. It exists so a reviewer — or the next person to change these
 * surfaces — can recapture the same two pages at the same two widths from the
 * same fixture rather than from whatever their workspace happens to hold.
 *
 * `REVIEW_CAPTURES=before` and `REVIEW_CAPTURES=after` only change the
 * filename. The fixture is identical in both, which is the point: run it on
 * this branch for `after`, and in a worktree of the base commit for `before`.
 * Every field the newer UI reads (`plan_health`, `next_step`, external ref
 * rows) is present in both runs, *and* so is the older `health {status}` the
 * previous code read — otherwise the before image would be missing health
 * badges for want of a field rather than for want of a design, and the
 * comparison would flatter the change.
 *
 *   REVIEW_CAPTURES=after PLAYWRIGHT_PORT=4291 pnpm exec playwright test \
 *     tests/e2e/review-captures.spec.js --project=default --workers=1
 *
 * Output lands in `web-ui/.screenshots/review/`, which is gitignored.
 */

const LABEL = process.env.REVIEW_CAPTURES || "";
/**
 * Gitignored. Review binaries do not ship in this repo: a reviewer runs the
 * capture, looks at the PNGs, and attaches them wherever the review lives.
 */
const OUT = ".screenshots/review";
const WORKSPACE = "/o/local/w/local";
const CARD_REF = "card:release-b";
const NOW = "2026-10-05T12:00:00Z";
const PR_URL = "https://github.com/Git-on-my-level/agent-nexus/pull/246";

const WIDTHS = [
  { label: "desktop", width: 1440, height: 1200 },
  { label: "390", width: 390, height: 1400 },
];

const CARD_BODY = [
  "**Goal:** one renderer, one vocabulary, a dashboard a CEO can read.",
  "",
  "<!-- fleet-sync:evidence:v1 -->",
  "",
  "- [x] shared markdown renderer",
  "- [x] compact badges",
  "- [ ] overview redesign",
  "",
  "| surface | state |",
  "| --- | --- |",
  "| docs | done |",
  "| reports | done |",
  "",
  `Blocked by card:contracts until the shape lands; landed in ${PR_URL}.`,
  "",
  "<details><summary>Why one renderer</summary>",
  "",
  "Because five of them disagreed about `<details>`.",
  "",
  "</details>",
].join("\n");

const plan = {
  steps: [
    {
      id: "contracts",
      title: "Shared report contracts",
      after: [],
      ref: "card:contracts",
    },
    {
      id: "renderer",
      title: "One markdown renderer",
      after: ["contracts"],
      ref: PR_URL,
    },
    { id: "badges", title: "Compact badges", after: ["contracts"] },
    {
      id: "overview",
      title: "Overview redesign",
      after: ["renderer", "badges"],
    },
    { id: "ship", title: "Ship the release", after: ["overview"] },
  ],
};

const planState = {
  steps: [
    { id: "contracts", status: "done" },
    { id: "renderer", status: "done" },
    { id: "badges", status: "active" },
    { id: "overview", status: "blocked" },
    { id: "ship", status: "not_started" },
  ],
  progress: { done: 2, total: 5 },
  critical_path: ["badges", "overview", "ship"],
  next_steps: ["badges", "overview"],
  shape: "dag",
  health: "blocked",
  last_movement_at: "2026-10-05T04:00:00Z",
};

const geometry = (statuses) => ({
  shape: "chain",
  total_nodes: statuses.length,
  collapsed_nodes: 0,
  nodes: statuses.map(([id, status], index) => ({
    id,
    status,
    layer: index,
    after: index ? [statuses[index - 1][0]] : [],
  })),
});

const chainPlan = (statuses) => ({
  steps: statuses.map(([id, status]) => ({ id, status })),
  progress: {
    done: statuses.filter(([, status]) => status === "done").length,
    total: statuses.length,
  },
  critical_path: statuses.map(([id]) => id),
  next_steps: statuses
    .filter(([, status]) => status !== "done")
    .map(([id]) => id),
  shape: "chain",
  last_movement_at: "2026-10-05T07:00:00Z",
});

const initiative = (overrides) => ({
  priority: "p1",
  phase: "in_progress",
  needs: [],
  board_ref: "board:release",
  updated_at: "2026-10-05T07:00:00Z",
  ...overrides,
});

const INITIATIVES = [
  initiative({
    ref: "card:ergonomics",
    title: "Agent ergonomics",
    summary: "**Goal:** fewer traps in the CLI and the skills.",
    plan_health: {
      state: "stale",
      reason: "Nothing has moved for 9 days.",
      since: "2026-09-26T09:00:00Z",
    },
    health: { status: "stalled", reason: "Nothing has moved for 9 days." },
    next_step: { id: "cli-inbox", title: "First-class Inbox in the CLI" },
    progress: { done: 1, total: 3 },
    plan_state: chainPlan([
      ["audit", "done"],
      ["cli-inbox", "not_started"],
      ["skills", "not_started"],
    ]),
    geometry: geometry([
      ["audit", "done"],
      ["cli-inbox", "not_started"],
      ["skills", "not_started"],
    ]),
  }),
  initiative({
    ref: CARD_REF,
    title: "Release B",
    summary: "**Goal:** one renderer, one vocabulary, a readable dashboard.",
    plan_health: {
      state: "blocked",
      reason: "The overview redesign is waiting on a decision.",
      since: "2026-10-04T09:00:00Z",
    },
    health: {
      status: "blocked",
      reason: "The overview redesign is waiting on a decision.",
    },
    next_step: { id: "badges", title: "Compact badges" },
    needs: ["Overview redesign decision"],
    progress: { done: 2, total: 5 },
    plan_state: planState,
    geometry: {
      shape: "dag",
      total_nodes: 5,
      collapsed_nodes: 0,
      nodes: [
        { id: "contracts", status: "done", layer: 0, after: [] },
        { id: "renderer", status: "done", layer: 1, after: ["contracts"] },
        { id: "badges", status: "active", layer: 1, after: ["contracts"] },
        {
          id: "overview",
          status: "blocked",
          layer: 2,
          after: ["renderer", "badges"],
        },
        { id: "ship", status: "not_started", layer: 3, after: ["overview"] },
      ],
    },
  }),
  initiative({
    ref: "card:dashboards",
    title: "Live dashboards",
    summary: "Panels that never go stale.",
    plan_health: { state: "on_track", reason: "Work is progressing." },
    health: { status: "on_track", reason: "Work is progressing." },
    next_step: { id: "adopt", title: "Adopt the dashboard" },
    progress: { done: 4, total: 5 },
    plan_state: chainPlan([
      ["contracts", "done"],
      ["panels", "done"],
      ["queries", "done"],
      ["publish", "done"],
      ["adopt", "active"],
    ]),
    geometry: geometry([
      ["contracts", "done"],
      ["panels", "done"],
      ["queries", "done"],
      ["publish", "done"],
      ["adopt", "active"],
    ]),
  }),
  initiative({
    ref: "card:schema-freeze",
    title: "Freeze the schema",
    summary: "Shipped in v0.12.0.",
    phase: "done",
    plan_health: { state: "done", reason: "Every step is done." },
    health: { status: "on_track", reason: "Every step is done." },
    progress: { done: 2, total: 2 },
    plan_state: chainPlan([
      ["agree", "done"],
      ["ship", "done"],
    ]),
    geometry: geometry([
      ["agree", "done"],
      ["ship", "done"],
    ]),
  }),
  initiative({
    ref: "card:dogfood-notes",
    title: "Collect dogfood notes",
    summary: "No plan written yet.",
    phase: "backlog",
    progress: { done: 0, total: 0 },
    plan_state: null,
    geometry: null,
  }),
];

const SNAPSHOT = {
  generated_at: NOW,
  needs_you: {
    status: "ok",
    count: 2,
    href: "/inbox?mailbox=needs-you",
    rows: [
      {
        id: "decision:launch",
        title: "Choose the launch date",
        source: "Ask PM",
        href: "/inbox?mailbox=needs-you&item=decision%3Alaunch",
      },
      {
        id: "inbox:rollback",
        title: "Approve the rollback wording",
        source: "Codex Sol",
        href: "/inbox?mailbox=needs-you&item=inbox%3Arollback",
      },
    ],
  },
  initiatives: { status: "ok", count: INITIATIVES.length, items: INITIATIVES },
  since_you_last_looked: {
    since: "2026-10-04T12:00:00Z",
    generated_at: NOW,
    items: [
      { kind: "initiative_blocked", ref: CARD_REF, title: "Release B" },
      {
        kind: "step_completed",
        ref: CARD_REF,
        title: "One markdown renderer",
        step_id: "renderer",
      },
    ],
    truncated: false,
  },
  dashboard: { status: "ok", pinned_ref: "", has_more: false, reports: [] },
  agents: { status: "ok", items: [] },
  work: { status: "ok", total: 5, human_count: 1, items: [] },
};

const RESOLVED = {
  items: [
    {
      ref: "card:contracts",
      kind: "card",
      title: "Shared report contracts",
      status: "done",
      owner_display: "Codex Luna",
      progress: { done: 5, total: 5 },
      resolvable: true,
    },
    {
      ref: PR_URL,
      kind: "external",
      authority: "github",
      native_id: "Git-on-my-level/agent-nexus#246",
      title: "One shared markdown renderer",
      url: PR_URL,
      status: "merged",
      resolvable: true,
    },
  ],
};

async function installFixture(page) {
  await page.clock.setFixedTime(new Date(NOW));
  await installWorkspaceApi(page, {});
  await page.route("**/*", async (route) => {
    const request = route.request();
    if (!["fetch", "xhr"].includes(request.resourceType())) {
      return route.fallback();
    }
    const path = decodeURIComponent(new URL(request.url()).pathname);
    const json = (body) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(body),
      });

    if (path === "/overview" || path === "/workspace/dashboard") {
      return json(SNAPSHOT);
    }
    if (path === "/refs/resolve" && request.method() === "POST") {
      return json(RESOLVED);
    }
    if (path.endsWith("/plan") && request.method() === "GET") {
      return json({ plan, plan_state: planState });
    }
    if (path.endsWith("/observations") && request.method() === "GET") {
      return json({ observations: [], next_cursor: "" });
    }
    if (path.endsWith("/participants") && request.method() === "GET") {
      return json({ participants: [], next_cursor: "" });
    }
    if (path.startsWith("/work/") && request.method() === "GET") {
      return json({
        work: {
          ref: CARD_REF,
          handle: "release-b",
          title: "Release B",
          summary: CARD_BODY,
          phase: "in_progress",
          priority: "high",
          plan_health: {
            state: "blocked",
            reason: "The overview redesign is waiting on a decision.",
          },
          next_step: { id: "badges", title: "Compact badges" },
          definition_of_done: [
            "One renderer everywhere",
            "No badge truncated to `On tr…`",
            "The critical path fits on screen",
          ],
          next_action: "Decide the overview layout",
          source: { authority: "nexus" },
        },
      });
    }
    return route.fallback();
  });
}

for (const { label, width, height } of WIDTHS) {
  test(`capture overview @ ${label}`, async ({ page }, testInfo) => {
    test.skip(!LABEL, "set REVIEW_CAPTURES=before|after to capture");
    test.setTimeout(120_000);
    await installFixture(page);
    await page.setViewportSize({ width, height });
    await page.goto(`${WORKSPACE}/overview`);
    await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible({
      timeout: 60_000,
    });
    // Let the tiles and the band settle before the shutter.
    await page.waitForLoadState("networkidle").catch(() => {});
    await page.evaluate(() => document.fonts?.ready);
    await mkdir(OUT, { recursive: true });
    const file = `${OUT}/${LABEL}-overview-${label}.png`;
    await page.screenshot({
      path: file,
      animations: "disabled",
      fullPage: true,
    });
    await testInfo.attach(`${LABEL}-overview-${label}`, {
      path: file,
      contentType: "image/png",
    });
  });

  test(`capture initiative page @ ${label}`, async ({ page }, testInfo) => {
    test.skip(!LABEL, "set REVIEW_CAPTURES=before|after to capture");
    test.setTimeout(120_000);
    await installFixture(page);
    await page.setViewportSize({ width, height });
    await page.goto(`${WORKSPACE}/tasks/${encodeURIComponent(CARD_REF)}`);
    await expect(page.getByRole("heading", { name: "Release B" })).toBeVisible({
      timeout: 60_000,
    });
    await page.waitForLoadState("networkidle").catch(() => {});
    await page.evaluate(() => document.fonts?.ready);
    await mkdir(OUT, { recursive: true });
    const file = `${OUT}/${LABEL}-initiative-${label}.png`;
    await page.screenshot({
      path: file,
      animations: "disabled",
      fullPage: true,
    });
    await testInfo.attach(`${LABEL}-initiative-${label}`, {
      path: file,
      contentType: "image/png",
    });
  });
}
