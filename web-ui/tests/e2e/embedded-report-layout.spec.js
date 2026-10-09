import { mkdir } from "node:fs/promises";

import { expect, test } from "@playwright/test";

import { AUDIT_VIEWPORTS, expectCleanLayout } from "../helpers/layoutAudit.js";
import {
  expectNoClippedContent,
  installWorkspaceApi,
} from "../helpers/workspaceApiMock.js";

/**
 * A report embedded in a narrower container than it was designed for.
 *
 * The Overview pins a dashboard below the initiatives, and that dashboard is a
 * full report rendered into a column that is already sharing the page with a
 * sidebar. A `live-initiatives` panel inside it carries whole plans — a tech
 * tree eight layers wide, step titles a sentence long, a dependency list that
 * names every predecessor. On the initiative page those fit, because the plan
 * has the page to itself. Embedded, they ran off the right edge: nodes and
 * connector lines cut mid-stroke, the "View plan steps" rows truncated by the
 * shell's own `overflow: hidden`, and no scrollbar anywhere to say so.
 *
 * The rule this pins: **over-tall beats clipped**. Content either fits the
 * width it is given, or scrolls inside a box that shows it can be scrolled.
 * Nothing is silently cut, and the page itself never scrolls sideways.
 */

const WORKSPACE = "/o/local/w/local";
const OVERVIEW = `${WORKSPACE}/overview`;
const DOC_ID = "wide-dashboard";
const NOW = "2026-10-06T12:00:00Z";

/** A step title long enough to need wrapping or truncation in any column. */
const LONG_TITLE =
  "Release A: live panels, report CLI, Overview, agent host enrollment (v0.12.5)";

/** Eight layers deep: wider than the embedded column at every audit width. */
const WIDE_PLAN_STEPS = [
  { id: "release-a", title: LONG_TITLE, after: [] },
  {
    id: "plans-on-cards",
    title: "Initiative plans on cards + computed state",
    after: ["release-a"],
  },
  {
    id: "live-series",
    title: "Live series + declared adapters",
    after: ["plans-on-cards"],
  },
  {
    id: "first-class-inbox",
    title: "First-class inbox + batched answer wake",
    after: ["live-series"],
  },
  {
    id: "cross-workspace",
    title: "Cross-workspace move",
    after: ["first-class-inbox"],
  },
  {
    id: "hosted-parity",
    title: "Hosted agent parity for agentctl",
    after: ["cross-workspace"],
  },
  {
    id: "self-update",
    title: "anx CLI self-update + agentctl parity",
    after: ["hosted-parity"],
  },
  {
    id: "ship-it",
    title: "Release B shipped to every workspace",
    after: ["self-update"],
  },
];

const WIDE_PLAN_STATE = {
  steps: [
    { id: "release-a", status: "done" },
    { id: "plans-on-cards", status: "done" },
    { id: "live-series", status: "done" },
    { id: "first-class-inbox", status: "active" },
    { id: "cross-workspace", status: "not_started" },
    { id: "hosted-parity", status: "not_started" },
    { id: "self-update", status: "not_started" },
    { id: "ship-it", status: "not_started" },
  ],
  progress: { done: 3, total: 8 },
  critical_path: ["first-class-inbox", "cross-workspace", "ship-it"],
  next_steps: ["first-class-inbox"],
  shape: "dag",
  health: "on_track",
  last_movement_at: "2026-10-06T01:00:00Z",
};

/** A panel that came back with nothing: present, named, and nearly empty. */
const LIVE_DECISIONS = {
  type: "live-asks",
  status: "ok",
  observed_at: NOW,
  data: { items: [] },
};

/** The live panel body, as `report.render` materializes it. */
const LIVE_INITIATIVES = {
  // `type` matters: `VisualReport` replaces a rendered panel whose type does
  // not match the authored one, so a mock without it renders "unavailable".
  type: "live-initiatives",
  status: "ok",
  observed_at: NOW,
  data: {
    items: [
      {
        ref: "card:release-b",
        title: "Release B: initiative plans, live dashboards, agent ergonomics",
        // Markdown on purpose: a report panel must render it, never print it.
        // The ref is here and nowhere else in the report: an authored live
        // panel is a query, so this is the only place it can be collected
        // from.
        summary:
          "**Goal:** a workspace overview where you can see each initiative's progress and state at a glance and drill into it. Blocked behind card:adapter-contract.",
        priority: "p1",
        phase: "in_progress",
        health: { status: "on_track", reason: "Work is progressing." },
        progress: { done: 3, total: 8 },
        plan: { steps: WIDE_PLAN_STEPS },
        plan_state: WIDE_PLAN_STATE,
      },
    ],
  },
};

const REPORT = {
  kind: "anx.visual-report",
  schema_version: 1,
  title: "Dashboard — ANX",
  summary: "Where things stand.",
  generated_at: NOW,
  projects: [
    {
      id: "anx",
      title: "ANX",
      summary: "The agent workspace.",
      outcome: "A dashboard a CEO can read.",
    },
  ],
  sources: [],
  panels: [
    {
      id: "standing",
      project_id: "anx",
      type: "explanation",
      title: "Where things stand",
      author: "claude",
      provenance: "reported",
      observed_at: NOW,
      freshness: "current",
      source_ids: [],
      data: {
        text: "**v0.12.12** is live on all workspaces. Release B is in review.",
      },
    },
    {
      id: "decisions",
      project_id: "anx",
      type: "live-asks",
      title: "Needs a decision",
      author: "claude",
      provenance: "reported",
      observed_at: NOW,
      freshness: "current",
      source_ids: [],
      data: { limit: 5 },
    },
    {
      id: "initiatives",
      project_id: "anx",
      type: "live-initiatives",
      title: "Initiatives",
      author: "claude",
      provenance: "reported",
      observed_at: NOW,
      freshness: "current",
      source_ids: [],
      data: { limit: 5 },
    },
  ],
};

const DOCUMENT = {
  id: DOC_ID,
  handle: DOC_ID,
  ref: `document:${DOC_ID}`,
  segment: DOC_ID,
  title: "Dashboard — ANX",
  state: "active",
  updated_at: NOW,
  revision_ref: `document_revision:${DOC_ID}-r1`,
};

const SNAPSHOT = {
  generated_at: NOW,
  needs_you: { status: "ok", count: 0, rows: [], href: "/inbox" },
  initiatives: { status: "ok", count: 0, items: [] },
  dashboard: {
    status: "ok",
    pinned_ref: DOCUMENT.ref,
    has_more: false,
    reports: [{ ...DOCUMENT, report: REPORT }],
  },
  agents: { status: "ok", items: [] },
  work: { status: "ok", total: 0, human_count: 0, items: [] },
};

/** What core would answer for the refs this report names. */
const RESOLVED = {
  "card:adapter-contract": {
    kind: "card",
    title: "Approve the adapter contract",
    status: "blocked",
    resolvable: true,
  },
  "card:release-b": {
    kind: "card",
    title: "Release B: initiative plans, live dashboards, agent ergonomics",
    status: "in_progress",
    resolvable: true,
  },
};

/** Every `refs` array the page sent, so a test can see what was asked for. */
let resolveCalls = [];

async function installEmbeddedReport(page) {
  resolveCalls = [];
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
    if (path === `/docs/${DOC_ID}/report`) {
      return json({
        document_ref: DOCUMENT.ref,
        revision_ref: DOCUMENT.revision_ref,
        observed_at: NOW,
        panels: [
          { id: "initiatives", ...LIVE_INITIATIVES },
          { id: "decisions", ...LIVE_DECISIONS },
        ],
      });
    }
    if (path === "/refs/resolve" && request.method() === "POST") {
      const asked = JSON.parse(request.postData() || "{}").refs ?? [];
      resolveCalls.push(asked);
      return json({
        items: asked
          .filter((ref) => RESOLVED[ref])
          .map((ref) => ({ ref, ...RESOLVED[ref] })),
      });
    }
    return route.fallback();
  });
}

async function openOverview(page, width, height) {
  await installEmbeddedReport(page);
  await page.setViewportSize({ width, height });
  await page.goto(OVERVIEW);
  // The live panel arrives after the snapshot, so wait for the plan itself.
  await expect(page.locator("[data-report-initiative]")).toBeVisible({
    timeout: 60_000,
  });
  await expect(page.locator("[data-plan-shape]")).toBeVisible();
  await page.evaluate(() => document.fonts?.ready);
}

for (const viewport of AUDIT_VIEWPORTS) {
  test(`an embedded report with a wide plan does not clip @ ${viewport.name}`, async ({
    page,
  }) => {
    test.setTimeout(120_000);
    await openOverview(page, viewport.width, viewport.height);

    // The page itself never scrolls sideways, at any width.
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > window.innerWidth + 1,
      ),
    ).toBe(false);

    // Nothing is cut by an overflow-hidden ancestor with no way to reach it.
    // The shell clips its own main column, so this is the check that sees it.
    await expectNoClippedContent(page, `embedded report @ ${viewport.name}`);

    await expectCleanLayout(page, `embedded report @ ${viewport.name}`, {
      scrollPositions: ["current", "bottom"],
    });
  });
}

/**
 * Placement comes from the width available and what each panel carries.
 *
 * The widths are the ones a laptop and a desktop actually give an embedded
 * report: 1280 and 1100 leave the dashboard column far narrower than the
 * window, which is exactly where a hard two-column grid put a plan graph and
 * a nearly empty panel side by side and clipped the graph.
 */
for (const width of [1280, 1100, 390]) {
  test(`wide content takes its own row @ ${width}`, async ({ page }) => {
    test.setTimeout(120_000);
    await openOverview(page, width, 1400);

    const cell = (id) =>
      page.locator(`[data-report-cell]:has([data-report-panel="${id}"])`);
    // The plan graph claims the row; the empty panel never does.
    await expect(cell("initiatives")).toHaveAttribute(
      "data-report-cell",
      "full",
    );
    await expect(cell("decisions")).toHaveAttribute(
      "data-report-cell",
      "column",
    );

    const grid = page.locator('[data-report-layout="grid"]').first();
    const boxes = await grid.evaluate((node) => {
      const inner =
        node.getBoundingClientRect().width -
        parseFloat(getComputedStyle(node).paddingLeft || "0") -
        parseFloat(getComputedStyle(node).paddingRight || "0");
      const cells = [...node.querySelectorAll(":scope > [data-report-cell]")];
      return {
        inner,
        cells: cells.map((item) => {
          const rect = item.getBoundingClientRect();
          const panel = item.querySelector("[data-report-panel]");
          return {
            id: panel?.dataset.reportPanel ?? "",
            kind: item.dataset.reportCell,
            top: Math.round(rect.top),
            width: Math.round(rect.width),
          };
        }),
      };
    });
    const plan = boxes.cells.find((item) => item.id === "initiatives");
    // Full means full: the graph gets every pixel the grid has.
    expect(plan.width).toBeGreaterThanOrEqual(Math.floor(boxes.inner) - 1);
    // And nothing shares its row.
    expect(boxes.cells.filter((item) => item.top === plan.top)).toHaveLength(1);

    const rows = new Set(boxes.cells.map((item) => item.top));
    if (width === 390) {
      // One column: a row per panel, in reading order.
      expect(rows.size).toBe(boxes.cells.length);
    } else {
      // Wider, the two narrow panels pair up — both still clear their own
      // minimum useful width, which is the only reason a second column exists.
      expect(rows.size).toBeLessThan(boxes.cells.length);
      for (const item of boxes.cells.filter((entry) => entry.top !== plan.top))
        expect(item.width).toBeGreaterThanOrEqual(260);
    }
  });
}

test("a plan too wide to fit scrolls inside its own box, and says so", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await openOverview(page, 1024, 900);

  const scroller = page.locator("[data-report-initiative] .plan-tree-scroll");
  await expect(scroller).toBeVisible();

  // Either it fits, or it scrolls — and if it scrolls, the box says so rather
  // than letting the diagram disappear off the edge.
  const box = await scroller.evaluate((node) => ({
    scrollable: node.scrollWidth > node.clientWidth + 1,
    clientWidth: node.clientWidth,
    scrollWidth: node.scrollWidth,
    parentWidth: node.parentElement.getBoundingClientRect().width,
  }));
  expect(box.clientWidth).toBeLessThanOrEqual(Math.ceil(box.parentWidth) + 1);
  if (box.scrollable) {
    await expect(scroller).toHaveAttribute("data-plan-scrollable", "true");
  }

  // And the diagram is reachable: scrolling it right brings the last column in.
  if (box.scrollable) {
    await scroller.evaluate((node) => {
      node.scrollLeft = node.scrollWidth;
    });
    await expect(page.locator('[data-plan-node="ship-it"]')).toBeInViewport();
  }
});

test("report panel text renders markdown rather than printing it", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await openOverview(page, 1440, 1000);

  const initiative = page.locator("[data-report-initiative]");
  // `**Goal:**` is bold, not four asterisks.
  await expect(initiative.locator("strong").first()).toHaveText("Goal:");
  await expect(initiative).not.toContainText("**Goal:**");

  // And the explanation panel above it, too.
  const explanation = page.locator("[data-report-panel='standing']");
  await expect(explanation.locator("strong").first()).toHaveText("v0.12.12");
  await expect(explanation).not.toContainText("**v0.12.12**");
});

test("a ref written only in a live summary resolves", async ({ page }) => {
  test.setTimeout(120_000);
  await openOverview(page, 1440, 1000);

  const chip = page.locator(
    "[data-report-initiative] [data-anx-ref='card:adapter-contract']",
  );
  await expect(chip).toBeVisible();

  // Asked for: collection reads the observation, not the authored query.
  await expect
    .poll(() => resolveCalls.flat())
    .toContain("card:adapter-contract");

  // And answered: the chip carries core's title rather than rendering dashed
  // and "not found", which is what a ref no one requested looks like.
  await expect(chip).toContainText("Approve the adapter contract");
  await expect(chip).not.toContainText("not found");
});

/**
 * Before / after for a review. Off unless `REVIEW_CAPTURES` is set — it
 * asserts nothing, so it has no place in a CI run. Output lands in the
 * gitignored `web-ui/.screenshots/review/`; review binaries are not committed.
 *
 *   REVIEW_CAPTURES=after pnpm exec playwright test \
 *     tests/e2e/embedded-report-layout.spec.js --project=default --workers=1
 */
const CAPTURE = process.env.REVIEW_CAPTURES || "";
const CAPTURE_OUT = ".screenshots/review";

for (const { label, width, height } of [
  { label: "desktop", width: 1440, height: 1600 },
  { label: "1280", width: 1280, height: 1600 },
  { label: "laptop-1100", width: 1100, height: 1600 },
  { label: "390", width: 390, height: 1800 },
]) {
  test(`capture embedded report @ ${label}`, async ({ page }, testInfo) => {
    test.skip(!CAPTURE, "set REVIEW_CAPTURES=before|after to capture");
    test.setTimeout(120_000);
    await openOverview(page, width, height);
    await mkdir(CAPTURE_OUT, { recursive: true });
    const file = `${CAPTURE_OUT}/${CAPTURE}-embedded-report-${label}.png`;
    await page.screenshot({
      path: file,
      animations: "disabled",
      fullPage: true,
    });
    await testInfo.attach(`${CAPTURE}-embedded-report-${label}`, {
      path: file,
      contentType: "image/png",
    });
  });
}
