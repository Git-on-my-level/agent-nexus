import { mkdir } from "node:fs/promises";

import { expect, test } from "@playwright/test";

import { nextPaint, waitForAppReady } from "../helpers/pageReady.js";
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
const DOC_ID = "dashboard";
const NOW = "2026-10-05T12:00:00Z";
const PR_URL = "https://github.com/Git-on-my-level/agent-nexus/pull/246";

/** Hours relative to the fixed clock, as an instant. */
function iso(hours) {
  return new Date(Date.parse(NOW) + hours * 3_600_000).toISOString();
}

function agentRow(name, state, overrides = {}) {
  return {
    id: `agent-${name}`,
    actor_id: `actor-${name}`,
    ref: `agent:${name}`,
    host_id: "host-1",
    host_slug: "workstation-a",
    name,
    handle: `${name}.workstation-a`,
    display_name: `${name} on workstation-a`,
    identity_kind: "derived",
    state,
    bridge_online: false,
    current_card_ref: null,
    current_card_title: null,
    last_progress_note: null,
    last_progress_at: null,
    active_run: null,
    open_asks_count: 0,
    waiting_ask: null,
    last_signal_at: iso(-0.1),
    revoked_at: null,
    ...overrides,
  };
}

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
  dashboard: {
    status: "ok",
    pinned_ref: `document:${DOC_ID}`,
    has_more: false,
    reports: [],
  },
  agents: { status: "ok", items: [] },
  work: { status: "ok", total: 5, human_count: 1, items: [] },
};

/**
 * A roster with one of each kind of quiet: an agent working, one waiting on a
 * human, one idle, one silent while holding a card, two silent holding nothing,
 * and two identities nobody has ever run.
 *
 * Core seeds every roster row with `state: "stale"` and only overwrites it with
 * waiting / working / idle (`commandcenter/roster.go`), so the last five arrive
 * as "stale" — which is exactly the lump this change splits.
 */
const ROSTER = [
  agentRow("codex", "working", {
    current_card_ref: "card:release-b",
    current_card_title: "Release B",
    last_progress_note: "Parry window at 120 ms, testing input buffer",
    last_progress_at: iso(-0.1),
    active_run: {
      run_id: "run-1",
      adapter: "codex",
      model: "sol",
      duration_seconds: 840,
    },
    bridge_online: true,
  }),
  agentRow("claude", "waiting_on_human", {
    open_asks_count: 1,
    waiting_ask: {
      id: "evt-ask",
      inbox_item_id: "inbox:ask:thread-1:evt-ask:evt-ask",
      title: "Confirm the renderer vocabulary",
      severity: "high",
      created_at: iso(-3.2),
      kind: "ask",
      subject_ref: "card:release-b",
      subject_title: "Release B",
      requester_actor_id: "actor-claude",
      requester_agent_id: "agent-claude",
    },
  }),
  agentRow("reviewer", "idle", { last_signal_at: iso(-2) }),
  // Silent two days with a card in hand: the one silence worth a warning.
  agentRow("builder", "stale", {
    last_signal_at: iso(-50),
    current_card_ref: "card:dashboards",
    current_card_title: "Live dashboards",
  }),
  agentRow("packager", "stale", { last_signal_at: iso(-60) }),
  agentRow("cutter", "stale", { last_signal_at: iso(-96) }),
  agentRow("release-bot", "stale", { last_signal_at: null }),
  agentRow("migration-bot", "stale", { last_signal_at: null }),
];

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

/**
 * The pinned dashboard, as the Overview embeds it and the Docs page renders
 * it. One panel per thing this pass changed: a bound chart whose author
 * declared stacked bars (core materializes bound charts as lines on a time
 * axis, so this is the panel that used to ignore the declaration and print
 * `anx-prs-merged repo=oss` in its legend), and a live panel beside it.
 */
const SERIES_START = Date.parse(NOW) - 6 * 86_400_000;
const seriesPoints = (values) =>
  values.map((value, index) => [SERIES_START + index * 86_400_000, value]);

const REPORT = {
  kind: "anx.visual-report",
  schema_version: 1,
  title: "Dashboard",
  summary: "How fast this workspace is shipping, and what is waiting on you.",
  generated_at: NOW,
  projects: [
    {
      id: "delivery",
      title: "Delivery",
      outcome: "On track",
      summary: "Shipping daily.",
    },
  ],
  sources: [],
  panels: [
    {
      id: "asks",
      project_id: "delivery",
      type: "live-asks",
      title: "Needs an answer",
      author: "actor-operator",
      provenance: "reported",
      observed_at: NOW,
      freshness: "current",
      source_ids: [],
      data: { limit: 5 },
    },
    {
      id: "merged",
      project_id: "delivery",
      type: "chart",
      title: "PRs merged per day",
      author: "actor-operator",
      provenance: "reported",
      observed_at: NOW,
      freshness: "current",
      source_ids: [],
      source: { series: "prs-merged", range: "7d", agg: "sum" },
      data: {},
      fallback: {
        as_of: "2026-10-01T00:00:00Z",
        data: {
          palette: "ocean",
          option: {
            xAxis: { type: "category", data: ["Mon", "Tue"] },
            yAxis: { type: "value", name: "PRs" },
            series: [
              { type: "bar", name: "OSS", stack: "repos", data: [4, 6] },
              { type: "bar", name: "SaaS", stack: "repos", data: [1, 2] },
            ],
          },
        },
      },
    },
  ],
};

const DASHBOARD_DOCUMENT = {
  id: DOC_ID,
  handle: DOC_ID,
  ref: `document:${DOC_ID}`,
  segment: DOC_ID,
  title: "Dashboard",
  state: "active",
  head_revision_id: "dashboard-revision-1",
  head_revision_number: 1,
  updated_at: NOW,
  updated_by: "actor-operator",
  created_at: NOW,
  created_by: "actor-operator",
};

const DASHBOARD_REVISION = {
  document_id: DOC_ID,
  revision_id: DASHBOARD_DOCUMENT.head_revision_id,
  ref: "document_revision:dashboard-r1",
  revision_number: 1,
  content_type: "text",
  content_hash: "dashboard-content-hash",
  revision_hash: "dashboard-revision-hash",
  created_at: NOW,
  created_by: "actor-operator",
  content: JSON.stringify(REPORT),
};

/** What `report.render` answers: every panel, with the live ones resolved. */
const RENDERED_REPORT = {
  revision_ref: DASHBOARD_REVISION.ref,
  panels: [
    {
      id: "asks",
      type: "live-asks",
      status: "ok",
      observed_at: NOW,
      truncated: false,
      data: {
        items: [
          {
            ref: "event:ask-wording",
            title: "Approve the rollback wording",
            asked_by: "Codex Sol",
            age_seconds: 3.2 * 3600,
            ts: iso(-3.2),
            href: "/inbox?mailbox=needs-you&item=inbox%3Arollback",
          },
        ],
      },
    },
    {
      id: "merged",
      type: "chart",
      status: "ok",
      observed_at: NOW,
      truncated: false,
      provenance: {
        adapter: "anx-dev",
        host: "workstation-a",
        last_push: NOW,
        expected_interval_seconds: 3600,
        resolution: "daily",
        series: "prs-merged",
        labels: {},
      },
      data: {
        option: {
          xAxis: { type: "time" },
          yAxis: { type: "value", name: "PRs" },
          series: [
            {
              name: "prs-merged repo=oss",
              type: "line",
              data: seriesPoints([1, 4, 7, 5, 6, 4, 2]),
            },
            {
              name: "prs-merged repo=saas",
              type: "line",
              data: seriesPoints([0, 2, 3, 2, 1, 2, 1]),
            },
          ],
        },
      },
    },
  ],
};

/** Two asks and an update: enough for the Inbox to have a shape. */
const INBOX_ITEMS = [
  {
    id: "inbox:rollback",
    kind: "ask",
    mailbox: "needs-you",
    title: "Approve the rollback wording",
    body: "Should the banner name the release, or just the date?",
    severity: "high",
    state: "open",
    created_at: iso(-3.2),
    requester: { id: "actor-codex", name: "Codex Sol" },
    related_refs: [CARD_REF],
    subject: { ref: CARD_REF, title: "Release B" },
  },
  {
    id: "inbox:renderer",
    kind: "ask",
    mailbox: "needs-you",
    title: "Confirm the renderer vocabulary",
    body: "One word for a card's state everywhere, or per surface?",
    severity: "normal",
    state: "open",
    created_at: iso(-20),
    requester: { id: "actor-claude", name: "Claude" },
    related_refs: [CARD_REF],
    subject: { ref: CARD_REF, title: "Release B" },
  },
  {
    id: "inbox:merged",
    kind: "update",
    mailbox: "watching",
    title: "One shared markdown renderer merged",
    state: "open",
    created_at: iso(-6),
    requester: { id: "actor-luna", name: "Codex Luna" },
    related_refs: [CARD_REF],
  },
];

async function installFixture(page) {
  await page.clock.setFixedTime(new Date(NOW));
  await installWorkspaceApi(page, { documents: [DASHBOARD_DOCUMENT] });
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
      return json({
        ...SNAPSHOT,
        agents: { status: "ok", items: ROSTER },
        dashboard: {
          ...SNAPSHOT.dashboard,
          reports: [{ ...DASHBOARD_DOCUMENT, report: REPORT }],
        },
      });
    }
    if (path === "/workspace/dashboard/reports") {
      return json({
        ...SNAPSHOT.dashboard,
        reports: [{ ...DASHBOARD_DOCUMENT, report: REPORT }],
      });
    }
    if (path === `/docs/${DOC_ID}/report`) return json(RENDERED_REPORT);
    if (path === `/docs/${DOC_ID}` && request.method() === "GET") {
      return json({
        document: DASHBOARD_DOCUMENT,
        revision: DASHBOARD_REVISION,
      });
    }
    if (path === "/inbox") {
      return json({ items: INBOX_ITEMS, total: INBOX_ITEMS.length });
    }
    if (path === "/agents" && request.method() === "GET") {
      return json({ agents: ROSTER });
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
    // The live stream stays open, so the network never goes idle.
    await waitForAppReady(page);
    await nextPaint(page);
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

  test(`capture agents @ ${label}`, async ({ page }, testInfo) => {
    test.skip(!LABEL, "set REVIEW_CAPTURES=before|after to capture");
    test.setTimeout(120_000);
    await installFixture(page);
    await page.setViewportSize({ width, height });
    await page.goto(`${WORKSPACE}/agents`);
    await expect(page.getByRole("heading", { name: "Agents" })).toBeVisible({
      timeout: 60_000,
    });
    await waitForAppReady(page);
    await nextPaint(page);
    await page.evaluate(() => document.fonts?.ready);
    await mkdir(OUT, { recursive: true });
    const file = `${OUT}/${LABEL}-agents-${label}.png`;
    await page.screenshot({
      path: file,
      animations: "disabled",
      fullPage: true,
    });
    await testInfo.attach(`${LABEL}-agents-${label}`, {
      path: file,
      contentType: "image/png",
    });
  });

  test(`capture dashboard document @ ${label}`, async ({ page }, testInfo) => {
    test.skip(!LABEL, "set REVIEW_CAPTURES=before|after to capture");
    test.setTimeout(120_000);
    await installFixture(page);
    await page.setViewportSize({ width, height });
    await page.goto(`${WORKSPACE}/docs/${DOC_ID}`);
    await expect(
      page.getByRole("heading", { name: "PRs merged per day" }),
    ).toBeVisible({ timeout: 60_000 });
    await waitForAppReady(page);
    await nextPaint(page);
    await page.evaluate(() => document.fonts?.ready);
    await mkdir(OUT, { recursive: true });
    const file = `${OUT}/${LABEL}-dashboard-${label}.png`;
    await page.screenshot({
      path: file,
      animations: "disabled",
      fullPage: true,
    });
    await testInfo.attach(`${LABEL}-dashboard-${label}`, {
      path: file,
      contentType: "image/png",
    });
  });

  test(`capture inbox @ ${label}`, async ({ page }, testInfo) => {
    test.skip(!LABEL, "set REVIEW_CAPTURES=before|after to capture");
    test.setTimeout(120_000);
    await installFixture(page);
    await page.setViewportSize({ width, height });
    await page.goto(`${WORKSPACE}/inbox`);
    await expect(page.getByRole("heading", { name: "Inbox" })).toBeVisible({
      timeout: 60_000,
    });
    await waitForAppReady(page);
    await nextPaint(page);
    await page.evaluate(() => document.fonts?.ready);
    await mkdir(OUT, { recursive: true });
    const file = `${OUT}/${LABEL}-inbox-${label}.png`;
    await page.screenshot({
      path: file,
      animations: "disabled",
      fullPage: true,
    });
    await testInfo.attach(`${LABEL}-inbox-${label}`, {
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
    await waitForAppReady(page);
    await nextPaint(page);
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
