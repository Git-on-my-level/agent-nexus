import { mkdir } from "node:fs/promises";

import AxeBuilder from "@axe-core/playwright";
import { expect, test } from "@playwright/test";

import { AUDIT_VIEWPORTS, expectCleanLayout } from "../helpers/layoutAudit.js";
import {
  expectNoClippedContent,
  installWorkspaceApi,
} from "../helpers/workspaceApiMock.js";

/**
 * Live, hand-written and overdue, side by side.
 *
 * Before this, all three rendered as the same bordered box with a title: a
 * reader could not tell a panel computed one second ago from a snapshot an
 * agent typed nine days earlier, and the hand-written one looked exactly as
 * authoritative. Panels now carry their provenance in their header — in words,
 * so the signal does not depend on the amber rendering.
 *
 * Checked on both surfaces that embed a report, because the compact Overview
 * embed drops the panel footer: it was the context where a stale snapshot was
 * least distinguishable from a live read, and the only one where the old
 * author line was not shown at all.
 */

const WORKSPACE = "/o/local/w/local";
const DOC_ID = "provenance-dashboard";
const DOC_PATH = `${WORKSPACE}/docs/${DOC_ID}`;
const OVERVIEW = `${WORKSPACE}/overview`;
const NOW = "2026-10-06T12:00:00Z";
const ago = (days) =>
  new Date(Date.parse(NOW) - days * 86_400_000).toISOString();

/**
 * An author label at the contract's 200-character limit: author is
 * author-supplied text, and at 390px it has to wrap inside the header rather
 * than run off the edge.
 */
const LONG_AUTHOR =
  "A long principal label, the kind a report generator produces when it names " +
  "a pipeline rather than a person, right up to the two hundred character " +
  "limit the contract sets for an author";

/** Written three days ago: hand-written, but inside its review window. */
const RECENT = ago(3);
/** Written nine days ago: past the seven-day default review deadline. */
const OVERDUE = ago(9);

const REPORT = {
  kind: "anx.visual-report",
  schema_version: 1,
  title: "Dashboard",
  summary: "Live reads beside the notes a reader has to date themselves.",
  generated_at: NOW,
  projects: [
    {
      id: "delivery",
      title: "Delivery",
      summary: "What is shipping.",
      outcome: "A dashboard that dates its own claims.",
    },
  ],
  sources: [],
  panels: [
    {
      id: "asks",
      project_id: "delivery",
      type: "live-asks",
      title: "Needs an answer",
      author: "claude",
      provenance: "reported",
      observed_at: NOW,
      freshness: "current",
      source_ids: [],
      data: { limit: 5 },
    },
    {
      id: "standing",
      project_id: "delivery",
      type: "explanation",
      title: "Where things stand",
      author: "claude",
      provenance: "reported",
      observed_at: RECENT,
      // The contract's own fields: when it was written, and when someone
      // undertook to look at it again. A duration is measured from the
      // writing, so this one is good for another four weeks.
      authored_at: RECENT,
      review_by: "30d",
      // Not declared stale: an author who writes that into the document is
      // warning the reader, and the panel says so. This one is simply older
      // than the 24-hour evidence window, which is not the same claim.
      freshness: "current",
      source_ids: [],
      data: {
        text: "Release B is in review. The adapter contract is still open.",
      },
    },
    {
      id: "legacy",
      project_id: "delivery",
      type: "explanation",
      // The author label is an author-supplied string up to 200 characters.
      // At 390px it has to wrap inside the header, not run off the edge.
      author: LONG_AUTHOR,
      provenance: "reported",
      observed_at: ago(2),
      authored_at: ago(2),
      review_by: "90d",
      freshness: "current",
      title: "Background",
      source_ids: [],
      data: { text: "Why this programme exists. Nobody has revisited it." },
    },
    {
      id: "cards",
      project_id: "delivery",
      type: "live-cards",
      title: "In review",
      author: "claude",
      provenance: "reported",
      observed_at: NOW,
      freshness: "current",
      source_ids: [],
      data: { status: "in_review", role: "design", limit: 5, sort: "updated" },
    },
    {
      id: "releases",
      project_id: "delivery",
      type: "live-timeline",
      title: "Releases",
      author: "claude",
      provenance: "reported",
      observed_at: NOW,
      freshness: "current",
      source_ids: [],
      data: {},
      source: { series: "releases", range: "30d" },
    },
    {
      id: "milestones",
      project_id: "delivery",
      type: "milestone-timeline",
      title: "Milestones",
      author: "claude",
      provenance: "reported",
      observed_at: OVERDUE,
      authored_at: OVERDUE,
      // Seven days after writing, nine days ago: overdue, and core says so.
      review_by: "7d",
      freshness: "stale",
      source_ids: [],
      data: {
        items: [
          {
            label: "Live panels shipped",
            date: ago(20),
            status: "complete",
            detail: "Dashboards read the workspace rather than a snapshot.",
            source_ids: [],
          },
          {
            label: "Provenance badges",
            date: null,
            status: "unknown",
            detail: "A hand-maintained row nobody has revisited since.",
            source_ids: [],
          },
        ],
      },
    },
  ],
};

const DOCUMENT = {
  id: DOC_ID,
  handle: DOC_ID,
  ref: `document:${DOC_ID}`,
  segment: DOC_ID,
  title: REPORT.title,
  state: "active",
  head_revision_id: `${DOC_ID}-revision-1`,
  head_revision_number: 1,
  revision_ref: `document_revision:${DOC_ID}-r1`,
  updated_at: NOW,
  updated_by: "actor-operator",
  created_at: NOW,
  created_by: "actor-operator",
};

const REVISION = {
  document_id: DOC_ID,
  revision_id: DOCUMENT.head_revision_id,
  ref: DOCUMENT.revision_ref,
  revision_number: 1,
  content_type: "text",
  content_hash: `${DOC_ID}-content-hash`,
  revision_hash: `${DOC_ID}-revision-hash`,
  created_at: NOW,
  created_by: "actor-operator",
  content: JSON.stringify(REPORT),
};

/**
 * The rendered report, as core answers it now: every panel, not only the live
 * ones. Core resolves each authored panel's class and deadline against its own
 * clock and returns `review_due`, so the reader's clock never decides whether
 * an author has been reminded.
 */
const authoredPanel = (id, type, authoredAt, reviewBy, due, data) => ({
  id,
  type,
  status: "ok",
  observed_at: authoredAt,
  truncated: false,
  data,
  provenance_class: "authored",
  author: id === "legacy" ? LONG_AUTHOR : "claude",
  authored_at: authoredAt,
  review_by: reviewBy,
  review_by_defaulted: false,
  review_due: due,
});

const RENDERED = {
  document_ref: DOCUMENT.ref,
  revision_ref: DOCUMENT.revision_ref,
  observed_at: NOW,
  panels: [
    {
      id: "asks",
      type: "live-asks",
      status: "ok",
      observed_at: NOW,
      truncated: false,
      provenance_class: "live",
      data: {
        items: [
          {
            id: "ask-launch",
            title: "Which launch theme?",
            status: "open",
            age_seconds: 7200,
          },
        ],
      },
    },
    {
      id: "cards",
      type: "live-cards",
      status: "ok",
      observed_at: NOW,
      truncated: false,
      provenance_class: "live",
      data: {
        items: [
          {
            ref: "card:provenance",
            title: "Provenance badges",
            summary: "Say which panels are computed.",
            priority: "p1",
            phase: "in_review",
            board_ref: "board:delivery",
            updated_at: ago(1),
            needs: [],
          },
        ],
      },
    },
    {
      id: "releases",
      type: "live-timeline",
      status: "ok",
      observed_at: NOW,
      truncated: false,
      provenance_class: "live",
      data: {
        items: [
          { at: ago(1), label: "channel=stable", value: "v0.12.13" },
          { at: ago(4), label: "channel=stable", value: "v0.12.12" },
        ],
      },
      provenance: {
        adapter: "releases",
        host: "builder",
        last_push: ago(1),
        resolution: "raw",
      },
    },
    authoredPanel("standing", "explanation", RECENT, ago(-27), false, {
      text: "Release B is in review. The adapter contract is still open.",
    }),
    authoredPanel("legacy", "explanation", ago(2), ago(-88), false, {
      text: "Why this programme exists. Nobody has revisited it.",
    }),
    authoredPanel("milestones", "milestone-timeline", OVERDUE, ago(2), true, {
      items: [],
    }),
  ],
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

async function installDashboard(page) {
  await page.clock.setFixedTime(new Date(NOW));
  await installWorkspaceApi(page, { documents: [DOCUMENT] });
  await page.route("**/*", async (route) => {
    const request = route.request();
    if (!["fetch", "xhr"].includes(request.resourceType()))
      return route.fallback();
    const path = decodeURIComponent(new URL(request.url()).pathname);
    const json = (body) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    if (path === `/docs/${DOC_ID}` && request.method() === "GET")
      return json({ document: DOCUMENT, revision: REVISION });
    if (path === `/docs/${DOC_ID}/report`) return json(RENDERED);
    if (path === "/overview" || path === "/workspace/dashboard")
      return json(SNAPSHOT);
    return route.fallback();
  });
}

const reportRegion = (page) =>
  page.getByRole("region", { name: "Visual report", exact: true });
const chip = (scope, panelId) =>
  scope.locator(`[data-report-panel="${panelId}"] [data-anx-provenance]`);

async function open(page, path, width = 1440, height = 1100) {
  await installDashboard(page);
  await page.setViewportSize({ width, height });
  await page.goto(path);
  // The live panel's observation arrives after the document. Waiting on the
  // ask itself rather than on a provenance chip keeps this helper usable for a
  // before/after capture against a build that has no chips.
  await expect(page.getByText("Which launch theme?")).toBeVisible({
    timeout: 60_000,
  });
  await expect(page.locator('[data-report-panel="milestones"]')).toBeVisible();
  await page.evaluate(() => document.fonts?.ready);
}

for (const [surface, path] of [
  ["the document report view", DOC_PATH],
  ["the Overview's embedded dashboard", OVERVIEW],
]) {
  test(`every panel says whether it is live or hand-written on ${surface}`, async ({
    page,
  }) => {
    test.setTimeout(120_000);
    await open(page, path);
    const report = reportRegion(page);

    // Computed at read time: the age is the age of the read.
    await expect(chip(report, "asks")).toHaveText(/^Live · updated just now$/);
    await expect(report.locator('[data-report-panel="asks"]')).toHaveAttribute(
      "data-provenance-class",
      "live",
    );

    // Hand-written and inside its review window: quiet, and dated.
    await expect(chip(report, "standing")).toHaveText(
      /^Written by claude · 3d ago$/,
    );
    await expect(
      report.locator('[data-report-panel="standing"]'),
    ).toHaveAttribute("data-provenance-state", "authored");

    // Past the review deadline: distrust it at a glance, in words.
    await expect(chip(report, "milestones")).toHaveText(
      /^May be stale · written 9d ago$/,
    );
    await expect(
      report.locator('[data-report-panel="milestones"]'),
    ).toHaveAttribute("data-provenance-state", "due-for-review");

    // The read time is in the header now, not a line under the panel body.
    await expect(report).not.toContainText("Live as of");

    // The two panel types core added land with live styling and a live line,
    // and the timeline shows each published event rather than a binned chart.
    await expect(chip(report, "cards")).toHaveText(/^Live · updated just now$/);
    await expect(report.locator('[data-report-panel="cards"]')).toContainText(
      "Provenance badges",
    );
    await expect(chip(report, "releases")).toHaveText(
      /^Live · updated just now$/,
    );
    const releases = report.locator('[data-report-panel="releases"]');
    await expect(releases).toContainText("v0.12.13");
    await expect(releases).toContainText("v0.12.12");
  });
}

test("the exact instant is one hover away, and reaches a screen reader", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await open(page, DOC_PATH);
  const report = reportRegion(page);

  const live = chip(report, "asks");
  await expect(live).toHaveAttribute("title", /Read .*Computed from workspace/);
  const authored = chip(report, "milestones").locator("time");
  await expect(authored).toHaveAttribute("datetime", OVERDUE);
  const label = await authored.getAttribute("aria-label");
  expect(label).toContain("Written");
  // The author set this deadline, so the tooltip must not claim it defaulted.
  expect(label).toContain("Review was due");
  expect(label).not.toContain("defaulted to 7 days");
});

test("a live panel whose read fails says so, and is not also badged", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await installDashboard(page);
  // Installed after the dashboard so this route wins: the rendered report is
  // forbidden, which is what a reader without access to the query sees.
  await page.route(`**/docs/${DOC_ID}/report`, (route) =>
    route.fulfill({
      status: 403,
      contentType: "application/json",
      body: JSON.stringify({ error: { code: "forbidden" } }),
    }),
  );
  await page.goto(DOC_PATH);
  const report = reportRegion(page);
  await expect(chip(report, "asks")).toHaveText(/^Live · read failed$/);
  // `withLiveObservation` calls a failed read `unavailable`, and the panel
  // used to print that as a second badge beside a line already saying it.
  await expect(
    report.locator('[data-report-panel="asks"] .report-state'),
  ).toHaveCount(0);
  await expect(report).toContainText("Live data unavailable");
});

test("provenance survives without colour, and passes an accessibility scan", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await open(page, DOC_PATH);
  const report = reportRegion(page);

  // Greyscale: the three states still read differently, because the words do
  // the work. Colour only repeats them.
  await page.addStyleTag({ content: "html { filter: grayscale(1); }" });
  for (const [panelId, text] of [
    ["asks", "Live · updated"],
    ["standing", "Written by claude"],
    ["milestones", "May be stale"],
  ])
    await expect(chip(report, panelId)).toContainText(text);

  const scan = await new AxeBuilder({ page })
    .include('[aria-label="Visual report"]')
    .analyze();
  expect(scan.violations).toEqual([]);
});

for (const viewport of AUDIT_VIEWPORTS) {
  test(`provenance headers do not clip @ ${viewport.name}`, async ({
    page,
  }) => {
    test.setTimeout(120_000);
    await open(page, OVERVIEW, viewport.width, viewport.height);

    // A header that wraps is fine. A header that is cut off is not: the chip
    // is the one thing on the panel the reader must be able to finish reading.
    await expect(chip(page, "milestones")).toContainText("May be stale");
    // A long author label wraps inside the header rather than being cut.
    await expect(chip(page, "legacy")).toContainText("Written by A long");
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > window.innerWidth + 1,
      ),
    ).toBe(false);
    await expectNoClippedContent(page, `provenance @ ${viewport.name}`);
    await expectCleanLayout(page, `provenance @ ${viewport.name}`, {
      scrollPositions: ["current", "bottom"],
    });
  });
}

/**
 * Before / after for a review. Off unless `REVIEW_CAPTURES` is set — it
 * asserts nothing, so it has no place in a CI run. Output lands in the
 * gitignored `web-ui/.screenshots/review/`; review binaries are not committed.
 *
 *   REVIEW_CAPTURES=after pnpm exec playwright test \
 *     tests/e2e/report-provenance.spec.js --project=default --workers=1
 */
const CAPTURE = process.env.REVIEW_CAPTURES || "";
const CAPTURE_OUT = ".screenshots/review";

for (const { label, width, height } of [
  { label: "desktop", width: 1440, height: 1400 },
  { label: "390", width: 390, height: 1700 },
]) {
  for (const [surface, path] of [
    ["dashboard", DOC_PATH],
    ["overview", OVERVIEW],
  ]) {
    test(`capture ${surface} provenance @ ${label}`, async ({
      page,
    }, testInfo) => {
      test.skip(!CAPTURE, "set REVIEW_CAPTURES=before|after to capture");
      test.setTimeout(120_000);
      await open(page, path, width, height);
      await mkdir(CAPTURE_OUT, { recursive: true });
      const file = `${CAPTURE_OUT}/${CAPTURE}-provenance-${surface}-${label}.png`;
      await page.screenshot({
        path: file,
        animations: "disabled",
        fullPage: true,
      });
      await testInfo.attach(`${CAPTURE}-provenance-${surface}-${label}`, {
        path: file,
        contentType: "image/png",
      });
    });
  }
}
