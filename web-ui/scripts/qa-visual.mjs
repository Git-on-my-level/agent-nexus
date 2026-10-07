#!/usr/bin/env node

import { spawn } from "node:child_process";
import http from "node:http";
import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { setTimeout as delay } from "node:timers/promises";

import { chromium } from "@playwright/test";
import pixelmatch from "pixelmatch";
import { PNG } from "pngjs";

import {
  QA_ACTORS,
  QA_ARTIFACTS,
  QA_ASK_ITEM,
  QA_AUTH_AUDIT,
  QA_AUTH_ADMINS,
  QA_AUTH_AGENT,
  QA_DOCUMENTS,
  QA_EVENTS,
  QA_FIXED_NOW_ISO,
  QA_INVITES,
  QA_AGENTS,
  QA_HOSTS,
  QA_HOST_ENROLLMENTS,
  QA_INBOX_POPULATED,
  QA_PRINCIPALS,
  QA_SECRETS,
  QA_TOPICS,
  QA_BOARDS,
  filterByQuery,
} from "../tests/fixtures/qa-seed.js";
import { auditLayout, formatViolations } from "../tests/helpers/layoutAudit.js";
import { getExpectedCommandRegistryDigest } from "../src/lib/commandRegistryDigest.js";
import { EXPECTED_SCHEMA_VERSION } from "../src/lib/config.js";

import {
  extensionScenes,
  routeExtensionRequest,
} from "../tests/helpers/uiExtensionQa.js";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const projectRoot = path.resolve(__dirname, "..");

const DEFAULT_VIEWPORT = { width: 1440, height: 900 };
const QA_AUDIT_MOBILE_VIEWPORT = { width: 375, height: 812 };
const DEFAULT_PORT = Number(process.env.QA_VISUAL_PORT ?? 4273);
const DEFAULT_CORE_PORT = Number(process.env.QA_VISUAL_CORE_PORT ?? 8000);
/** Baselines are often captured on macOS; CI runs Linux Chromium. Shared shell/font drift on dense pages landed ~1.6% in CI, so keep the bar above that and reserve a higher cap for access. */
const DEFAULT_THRESHOLD_RATIO = 0.018;
/** Git-tracked PNGs for `qa:diff` in CI (`.qa-baseline/` is gitignored for local captures). */
const QA_BASELINE_DIR = path.join(
  projectRoot,
  "tests",
  "fixtures",
  "qa-visual-baseline",
);
const QA_CURRENT_DIR = path.join(projectRoot, ".qa-current");
const QA_DIFF_DIR = path.join(projectRoot, ".qa-diff");
const QA_HOME_FEED_TYPES = new Set([
  "message_posted",
  "card_created",
  "card_moved",
  "card_closed",
  "card_resolved",
  "card_restored",
  "card_archived",
  "card_trashed",
  "topic_priority_changed",
  "topic_lifecycle_changed",
  "topic_updated",
  "topic_archived",
  "topic_restored",
  "topic_trashed",
  "human_attention_requested",
  "human_attention_responded",
  "document_created",
  "document_revision_created",
  "document_revised",
]);

export const QA_SCENES = [
  ...extensionScenes,
  {
    name: "workspace-home-handoff-first-run",
    path: "/o/local/w/local",
    workspaceMode: "home-first-run",
    waitFor: async (page) => {
      // One urgent band, not a second Inbox: it counts what is waiting and
      // links across, and an item's own title lives in the Inbox.
      await page.locator("[data-urgent-ask-count]").waitFor();
      await page.waitForSelector("text=No open initiatives.");
      await page.locator("[data-overview-detail]:not([open])").waitFor();
    },
  },
  {
    name: "workspace-home-handoff-populated",
    path: "/o/local/w/local",
    workspaceMode: "home-recent",
    waitFor: async (page) => {
      await page.waitForSelector(
        "[data-overview-report='doc-fleet-dashboard']",
      );
      // The worst initiative is first, and the collapsed tails are both there.
      await page
        .locator("[data-initiative-group='attention'] [data-tile-health]")
        .first()
        .waitFor();
      await page.locator("[data-initiative-group='done']").waitFor();
      await page.locator("[data-initiative-group='no-plan']").waitFor();
      await page.waitForSelector("text=Approve rollback wording");
    },
  },
  {
    name: "workspace-home-handoff-empty",
    path: "/o/local/w/local",
    workspaceMode: "home-empty",
    waitFor: async (page) => {
      await page.locator("[data-urgent-state='empty']").waitFor();
      await page.waitForSelector("text=Nothing is waiting on you.");
      await page.waitForSelector("text=No open initiatives.");
      await page.waitForSelector(
        "text=Pin a visual report to show your workspace dashboard here.",
      );
      await page.locator("[data-overview-detail]:not([open])").waitFor();
    },
  },
  {
    name: "workspace-inbox-empty",
    path: "/o/local/w/local/inbox",
    workspaceMode: "inbox-empty",
    waitFor: async (page) => {
      // Empty Needs you now reads "You're clear." plus a link to Watching.
      await page.waitForSelector("text=You're clear.");
    },
  },
  {
    name: "workspace-inbox-populated",
    path: "/o/local/w/local/inbox",
    workspaceMode: "inbox-populated",
    waitFor: async (page) => {
      await page.waitForSelector('[data-testid="inbox-row-inbox-ask-auth"]');
    },
  },
  {
    name: "workspace-inbox-loading",
    path: "/o/local/w/local/inbox",
    workspaceMode: "inbox-loading",
    waitFor: async (page) => {
      await page.waitForSelector("text=Loading inbox…");
    },
  },
  {
    name: "workspace-inbox-error",
    path: "/o/local/w/local/inbox",
    workspaceMode: "inbox-error",
    waitFor: async (page) => {
      await page.waitForSelector('[role="alert"]');
    },
  },
  {
    name: "workspace-capture-ui",
    path: "/o/local/w/local/inbox/inbox-ask-auth",
    workspaceMode: "capture-ui",
    thresholdRatio: 0.018,
    waitFor: async (page) => {
      await page.waitForSelector("text=Approve auth callback rollback window");
    },
  },
  {
    name: "workspace-capture-degraded",
    path: "/o/local/w/local/inbox/inbox-ask-auth",
    workspaceMode: "capture-degraded",
    waitFor: async (page) => {
      await page.waitForSelector('[role="alert"]');
      await page.waitForSelector("text=Failed to load inbox item");
    },
  },
  {
    name: "workspace-tasks",
    path: "/o/local/w/local/tasks",
    workspaceMode: "workspace-default",
    waitFor: async (page) => {
      await page.waitForSelector('h1:has-text("Tasks")');
    },
  },
  {
    name: "workspace-docs",
    path: "/o/local/w/local/docs",
    workspaceMode: "workspace-default",
    waitFor: async (page) => {
      await page.waitForSelector('h1:has-text("Docs")');
    },
  },
  {
    name: "workspace-doc-detail-comments-rail",
    path: "/o/local/w/local/docs/doc-launch-checklist",
    workspaceMode: "workspace-default",
    localStorage: {
      "discussion-drawer:doc-discussion:doc-launch-checklist": "1",
    },
    waitFor: async (page) => {
      await page.waitForSelector('h1:has-text("Launch checklist")');
      await page.waitForSelector("text=Operator-facing launch checklist");
      // Desktop (1440px) uses the document rail; the dock header is lg:hidden.
      // Do not use text=Discussion: it matches the hidden settings hint
      // "Workspace projects and discussions" first and never becomes visible.
      // Below lg the rail is replaced by the discussion dock.
      if ((page.viewportSize()?.width ?? 1440) >= 1024) {
        await page.waitForSelector("aside.dd-rail");
      }
      await page.waitForSelector("text=Check the OAuth callback copy");
      await page.waitForSelector("text=Keep this wording exact");
    },
  },
  {
    name: "workspace-settings",
    path: "/o/local/w/local/more",
    workspaceMode: "workspace-default",
    waitFor: async (page) => {
      await page.waitForSelector("text=Settings");
    },
  },
  {
    name: "workspace-access",
    path: "/o/local/w/local/access",
    workspaceMode: "workspace-default",
    thresholdRatio: 0.035,
    waitFor: async (page) => {
      await page.waitForSelector('[data-host-enrollment="henr_qa_ci"]');
      await page.waitForSelector("text=reviewer");
      await page
        .getByRole("button", { name: "Revoke administration", exact: true })
        .waitFor();
    },
  },
  {
    name: "workspace-agents",
    path: "/o/local/w/local/agents",
    workspaceMode: "workspace-default",
    waitFor: async (page) => {
      await page.waitForSelector("text=Waiting on you");
      // Identities that never checked in are folded into a counted group, so
      // the roster's first screen is the agents that actually exist.
      await page.waitForSelector('[data-agents-fold="inactive"]');
    },
  },
  {
    name: "workspace-secrets",
    path: "/o/local/w/local/secrets",
    workspaceMode: "workspace-default",
    waitFor: async (page) => {
      await page.waitForSelector("text=OPENAI_API_KEY");
    },
  },
  {
    name: "command-palette-open",
    path: "/o/local/w/local/inbox",
    workspaceMode: "inbox-populated",
    waitFor: async (page) => {
      await page.waitForSelector('[data-testid="inbox-row-inbox-ask-auth"]');
      // The sidebar trigger only exists on wide layouts; the shortcut works everywhere.
      if (await page.locator(".shell-search-trigger").isVisible()) {
        await page.click(".shell-search-trigger");
      } else {
        await page.keyboard.press("ControlOrMeta+k");
      }
      await page.fill(".cmd-input", "launch");
      await page.waitForSelector(".cmd-result-row");
    },
  },
  {
    name: "confirm-modal-open",
    path: "/o/local/w/local/docs",
    workspaceMode: "workspace-default",
    waitFor: async (page) => {
      await page.waitForSelector('h1:has-text("Docs")');
      await page.getByRole("button", { name: "Select" }).click();
      await page.locator('[aria-label="Select Launch checklist"]').click();
      await page.getByRole("button", { name: "Archive" }).click();
      await page.waitForSelector("text=Archive 1 documents");
    },
  },
];

function parseCliArgs(argv) {
  const args = argv.slice(2);
  const options = {
    mode: "diff",
    port: DEFAULT_PORT,
    thresholdRatio: DEFAULT_THRESHOLD_RATIO,
    outDir: QA_CURRENT_DIR,
    json: false,
    sceneNames: [],
  };

  for (let index = 0; index < args.length; index += 1) {
    const arg = args[index];
    switch (arg) {
      case "--baseline":
        options.mode = "baseline";
        options.outDir = QA_BASELINE_DIR;
        break;
      case "--diff":
        options.mode = "diff";
        options.outDir = QA_CURRENT_DIR;
        break;
      case "--port":
        options.port = Number(args[index + 1] ?? DEFAULT_PORT);
        index += 1;
        break;
      case "--out":
        options.outDir = path.resolve(args[index + 1]);
        index += 1;
        break;
      case "--threshold":
        options.thresholdRatio = Number(
          args[index + 1] ?? DEFAULT_THRESHOLD_RATIO,
        );
        index += 1;
        break;
      case "--json":
        options.json = true;
        break;
      case "--scene":
        options.sceneNames.push(String(args[index + 1] ?? ""));
        index += 1;
        break;
      case "--help":
      case "-h":
        printUsage();
        process.exit(0);
        break;
      default:
        throw new Error(`Unknown option: ${arg}`);
    }
  }

  return options;
}

function printUsage() {
  console.log(
    `
QA Visual Harness

Usage:
  node scripts/qa-visual.mjs --baseline
  node scripts/qa-visual.mjs --diff

Options:
  --baseline            Capture the canonical baseline into tests/fixtures/qa-visual-baseline/
  --diff                Capture current screenshots and diff against that baseline
  --out <dir>           Override the output directory for fresh captures
  --port <port>         Preview server port (default: ${DEFAULT_PORT})
  --threshold <ratio>   Max differing-pixel ratio before failing (default: ${DEFAULT_THRESHOLD_RATIO}; some scenes override)
  --json                Emit machine-readable JSON summary
  --scene <name>        Capture or diff one scene (repeatable). Baseline updates only those files.
`.trim(),
  );
}

function normalizePathname(pathname) {
  if (!pathname || pathname === "/") {
    return "/";
  }
  return pathname.endsWith("/") ? pathname.slice(0, -1) : pathname;
}

function getLimit(searchParams, fallback = 200) {
  const raw = Number.parseInt(String(searchParams.get("limit") ?? ""), 10);
  if (!Number.isFinite(raw) || raw <= 0) {
    return fallback;
  }
  return raw;
}

function sliceByLimit(items, searchParams) {
  return items.slice(0, getLimit(searchParams, items.length));
}

function qaBoardListRows(boards) {
  return boards.map((b) => {
    const { board_summary, projection_freshness, ...boardFields } = b;
    const cols = board_summary?.cards_by_column ?? {};
    const cardCount = Object.values(cols).reduce(
      (acc, n) => acc + Number(n ?? 0),
      0,
    );
    const docCount = Array.isArray(boardFields.document_refs)
      ? boardFields.document_refs.length
      : 0;
    return {
      board: { ...boardFields, projection_freshness },
      summary: {
        card_count: cardCount,
        cards_by_column: cols,
        unresolved_card_count: cardCount,
        resolved_card_count: 0,
        document_count: docCount,
        latest_activity_at: board_summary?.latest_activity_at ?? null,
        has_document_refs: docCount > 0,
      },
    };
  });
}

function qaHoursAgo(hours) {
  return new Date(
    Date.parse(QA_FIXED_NOW_ISO) - hours * 60 * 60 * 1000,
  ).toISOString();
}

function overviewFleetDocument() {
  return {
    id: "doc-fleet-dashboard",
    title: "Fleet Dashboard",
    state: "active",
    thread_id: "thread-launch-war-room",
    head_revision_number: 1,
    updated_at: qaHoursAgo(2),
    updated_by: "actor-jordan-human",
  };
}

function overviewVisualReport() {
  return {
    kind: "anx.visual-report",
    schema_version: 1,
    title: "Fleet Dashboard",
    summary:
      "A snapshot of work in flight. It does not establish that the fleet is healthy.",
    generated_at: QA_FIXED_NOW_ISO,
    projects: [
      {
        id: "studio",
        title: "Studio",
        summary: "Launch checklist and rollback wording.",
        outcome: "Snapshot only",
      },
    ],
    sources: [],
    panels: [
      {
        id: "note",
        project_id: "studio",
        type: "explanation",
        title: "Evidence boundary",
        author: "QA",
        provenance: "reported",
        observed_at: null,
        freshness: "unavailable",
        source_ids: [],
        data: {
          text: "No live observation is attached. This does not establish health.",
        },
      },
    ],
  };
}

function overviewFleetDetail() {
  const document = overviewFleetDocument();
  return {
    document: {
      ...document,
      summary: "Snapshot of work in flight.",
      subject_ref: "",
      created_at: document.updated_at,
      created_by: document.updated_by,
    },
    revision: {
      revision_id: "rev-doc-fleet-dashboard-1",
      document_id: document.id,
      revision_number: 1,
      content_type: "text",
      content_hash: "sha256-qa-fleet",
      revision_hash: "revhash-qa-fleet",
      created_at: document.updated_at,
      created_by: document.updated_by,
      content: JSON.stringify(overviewVisualReport()),
    },
  };
}

function overviewPopulatedWork() {
  return [
    {
      ref: "card:launch-checklist",
      title: "Finalize launch checklist",
      phase: "in_progress",
      source: { authority: "github", native_id: "100" },
      updated_at: qaHoursAgo(2),
      next_actor: "actor-zara-ops",
      freshness: {
        status: "fresh",
        last_observed_at: QA_FIXED_NOW_ISO,
        stale_after_seconds: 86_400,
      },
    },
    {
      ref: "card:rollback-wording",
      title: "Approve rollback wording",
      phase: "blocked",
      source: { authority: "github", native_id: "101" },
      updated_at: qaHoursAgo(5),
      next_actor: "actor-jordan-human",
      freshness: {
        status: "stale",
        last_observed_at: qaHoursAgo(48),
        stale_after_seconds: 3600,
      },
    },
    {
      ref: "card:cutover-note",
      title: "Record the cutover decision",
      phase: "ready",
      source: { authority: "nexus" },
      updated_at: qaHoursAgo(8),
      freshness: { status: "unknown" },
    },
  ];
}

/**
 * The initiative rows the Overview sorts.
 *
 * One of each state the attention sort cares about — blocked, stale, on track,
 * and one with no plan at all — so the QA scenes and the layout sweep see the
 * real order, the collapsed tails and the mini plans rather than four
 * identical planless tiles. The summaries are markdown on purpose: the tile
 * must show prose, never `**Goal:**`.
 */
function overviewInitiatives(work) {
  const planFor = (statuses, overrides = {}) => ({
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
    last_movement_at: qaHoursAgo(5),
    ...overrides,
  });
  const geometryFor = (statuses) => ({
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

  const byRef = {
    "card:rollback-wording": {
      summary: "**Goal:** agree the wording we roll back with.",
      plan_health: {
        state: "blocked",
        reason: "Waiting on a human decision since Tuesday.",
        since: qaHoursAgo(30),
      },
      next_step: {
        id: "agree-wording",
        title: "Agree the rollback wording",
        ref: "https://github.com/Git-on-my-level/agent-nexus/pull/246",
      },
      needs: ["Rollback wording decision"],
      statuses: [
        ["draft-wording", "done"],
        ["review-wording", "done"],
        ["agree-wording", "blocked"],
        ["publish", "not_started"],
      ],
    },
    "card:launch-checklist": {
      summary: "**Goal:** every launch step has an owner and a date.",
      plan_health: {
        state: "at_risk",
        reason: "Two steps slipped their due date.",
        since: qaHoursAgo(20),
      },
      next_step: { id: "assign-owners", title: "Assign the remaining owners" },
      statuses: [
        ["scope", "done"],
        ["assign-owners", "active"],
        ["dry-run", "not_started"],
      ],
    },
    "card:cutover-note": {
      summary: "Write down what we decided about the cutover window.",
      plan_health: { state: "on_track", reason: "Work is progressing." },
      next_step: { id: "write-it-up", title: "Write it up" },
      statuses: [
        ["decide", "done"],
        ["write-it-up", "active"],
      ],
    },
  };

  const items = work.map((item) => {
    const extra = byRef[item.ref];
    if (!extra) {
      return {
        ...item,
        summary: "",
        priority: "none",
        progress: { done: 0, total: 0 },
        needs: [],
        board_ref: "board:launch",
        plan_state: null,
        geometry: null,
      };
    }
    const { statuses, ...rest } = extra;
    return {
      ...item,
      priority: "none",
      needs: [],
      board_ref: "board:launch",
      plan_state: planFor(statuses),
      geometry: geometryFor(statuses),
      progress: planFor(statuses).progress,
      ...rest,
    };
  });

  // One finished initiative and one with no plan, so both collapsed tails are
  // exercised by the sweep.
  items.push({
    ref: "card:schema-freeze",
    title: "Freeze the schema",
    phase: "done",
    source: { authority: "nexus" },
    updated_at: qaHoursAgo(72),
    summary: "Shipped in v0.12.0.",
    priority: "none",
    needs: [],
    board_ref: "board:launch",
    plan_health: { state: "done", reason: "Every step is done." },
    plan_state: planFor([
      ["agree", "done"],
      ["ship", "done"],
    ]),
    geometry: geometryFor([
      ["agree", "done"],
      ["ship", "done"],
    ]),
    progress: { done: 2, total: 2 },
  });
  items.push({
    ref: "card:dogfood-notes",
    title: "Collect dogfood notes",
    phase: "backlog",
    source: { authority: "nexus" },
    updated_at: qaHoursAgo(96),
    summary: "No plan written yet.",
    priority: "none",
    needs: [],
    board_ref: "board:launch",
    plan_state: null,
    geometry: null,
    progress: { done: 0, total: 0 },
  });

  return items;
}

function overviewFirstRunInboxItem() {
  return {
    ...QA_INBOX_POPULATED[0],
    id: "inbox-first-run",
    title: "Enroll the machine your agents run on",
  };
}

// Overview reads the shared core projection, rather than assembling these
// legacy per-resource fixtures in the browser.
function overviewSnapshot(scenario) {
  const populated = scenario.overviewState === "populated";
  const work = populated ? overviewPopulatedWork() : [];
  const inbox =
    scenario.overviewState === "first-run"
      ? [overviewFirstRunInboxItem()]
      : populated
        ? QA_INBOX_POPULATED
        : [];
  const rows = inbox.map((item) => ({
    id: `inbox:${item.id}`,
    title: item.title,
    source: item.requester_label || "",
    href: `/inbox?mailbox=needs-you&item=${encodeURIComponent(`inbox:${item.id}`)}`,
  }));
  if (populated) {
    rows.push({
      id: "task:card:rollback-wording",
      title: "Approve rollback wording",
      source: "Waiting on you",
      href: "/tasks/rollback-wording",
      badge: { label: "Needs you", tone: "warn" },
    });
  }
  const document = overviewFleetDocument();
  return {
    generated_at: QA_FIXED_NOW_ISO,
    needs_you: {
      status: "ok",
      count: rows.length,
      rows,
      href: "/inbox?mailbox=needs-you",
    },
    work: {
      status: "ok",
      total: work.length,
      human_count: populated ? 1 : 0,
      items: work,
    },
    initiatives: {
      status: "ok",
      count: work.length,
      items: populated ? overviewInitiatives(work) : [],
    },
    dashboard: {
      status: "ok",
      pinned_ref: null,
      has_more: false,
      reports: populated
        ? [
            {
              ...document,
              ref: `document:${document.id}`,
              segment: document.id,
              revision_ref: `document_revision:${document.id}-r1`,
              report: overviewVisualReport(),
            },
          ]
        : [],
    },
    agents: { status: "ok", items: populated ? QA_AGENTS : [] },
  };
}

function qaDocumentDetail(documentId) {
  const document = QA_DOCUMENTS.find((item) => item.id === documentId);
  if (!document) return null;
  const revisionNumber = Number(document.head_revision_number ?? 1) || 1;
  const revisionId = `rev-${document.id}-${revisionNumber}`;
  return {
    document: {
      ...document,
      summary:
        document.summary ??
        "Operator-facing checklist for launch readiness and rollback review.",
      subject_ref:
        document.subject_ref ??
        (document.thread_id ? `thread:${document.thread_id}` : ""),
      created_at: document.created_at ?? document.updated_at,
      created_by: document.created_by ?? document.updated_by,
    },
    revision: {
      revision_id: revisionId,
      document_id: document.id,
      revision_number: revisionNumber,
      content_type: "text",
      content_hash: `sha256-qa-${document.id}`,
      revision_hash: `revhash-qa-${document.id}-${revisionNumber}`,
      created_at: document.updated_at,
      created_by: document.updated_by,
      content: [
        "## Operator-facing launch checklist",
        "",
        "Check the OAuth callback copy before the public beta switch flips.",
        "",
        "Keep this wording exact for support handoff and incident review.",
      ].join("\n"),
    },
  };
}

function qaDocumentDetailTimeline(threadId) {
  const detail = qaDocumentDetail("doc-launch-checklist");
  const document = detail?.document;
  const revision = detail?.revision;
  if (!document || !revision || threadId !== document.thread_id) {
    return null;
  }
  const documentRef = `document:${document.id}`;
  const revisionRef = `document_revision:${revision.revision_id}`;
  const baseRefs = [`thread:${threadId}`, documentRef];
  const commentAnchor = {
    document_id: document.id,
    revision_id: revision.revision_id,
    content_hash: revision.content_hash,
    selected_text: "Check the OAuth callback copy",
    context_before: "## Operator-facing launch checklist\n\n",
    context_after: " before the public beta switch flips.",
    start_offset: revision.content.indexOf("Check the OAuth callback copy"),
    end_offset:
      revision.content.indexOf("Check the OAuth callback copy") +
      "Check the OAuth callback copy".length,
    anchor_status: "current",
  };
  const events = [
    {
      id: "evt-doc-rail-note-1",
      ts: qaHoursAgo(3),
      type: "message_posted",
      actor_id: "actor-zara-ops",
      thread_id: threadId,
      refs: baseRefs,
      summary: "Message: The readiness wording is close.",
      payload: { text: "The readiness wording is close." },
    },
    {
      id: "evt-doc-rail-note-2",
      ts: qaHoursAgo(2),
      type: "message_posted",
      actor_id: "actor-jordan-human",
      thread_id: threadId,
      refs: baseRefs,
      summary: "Message: Keep support handoff visible in the intro.",
      payload: { text: "Keep support handoff visible in the intro." },
    },
    {
      id: "evt-doc-rail-anchor-1",
      ts: qaHoursAgo(1),
      type: "message_posted",
      actor_id: "actor-iris-docs",
      thread_id: threadId,
      refs: [...baseRefs, revisionRef],
      summary: "Message: This is the line that needs one more pass.",
      payload: {
        kind: "document_text_comment",
        text: "This is the line that needs one more pass.",
        document_comment: commentAnchor,
      },
    },
    {
      id: "evt-doc-rail-note-3",
      ts: qaHoursAgo(0.5),
      type: "message_posted",
      actor_id: "actor-jordan-human",
      thread_id: threadId,
      refs: baseRefs,
      summary: "Message: Keep this wording exact.",
      payload: { text: "Keep this wording exact." },
    },
  ];
  return {
    thread: { id: threadId, title: document.title },
    events,
    artifacts: {},
    topics: {},
    cards: {},
    documents: {
      [document.id]: document,
    },
    document_revisions: {
      [revision.revision_id]: revision,
    },
  };
}

function createWorkspaceScenario(mode) {
  switch (mode) {
    case "home-empty":
      return {
        inboxState: "populated",
        askState: "ok",
        homeState: "empty",
        overviewState: "empty",
      };
    case "home-recent":
      return {
        inboxState: "populated",
        askState: "ok",
        homeState: "recent",
        overviewState: "populated",
      };
    case "home-first-run":
      return {
        inboxState: "populated",
        askState: "ok",
        homeState: "all",
        overviewState: "first-run",
      };
    case "inbox-empty":
      return {
        inboxState: "empty",
        askState: "ok",
        homeState: "all",
        overviewState: "default",
      };
    case "inbox-populated":
      return {
        inboxState: "populated",
        askState: "ok",
        homeState: "all",
        overviewState: "default",
      };
    case "inbox-loading":
      return {
        inboxState: "loading",
        askState: "ok",
        homeState: "all",
        overviewState: "default",
      };
    case "inbox-error":
      return {
        inboxState: "error",
        askState: "ok",
        homeState: "all",
        overviewState: "default",
      };
    case "capture-ui":
      return {
        inboxState: "populated",
        askState: "ok",
        homeState: "all",
        overviewState: "default",
      };
    case "capture-degraded":
      return {
        inboxState: "populated",
        askState: "error",
        homeState: "all",
        overviewState: "default",
      };
    default:
      return {
        inboxState: "populated",
        askState: "ok",
        homeState: "all",
        overviewState: "default",
      };
  }
}

function homeGroupForEvent(event) {
  const refs = Array.isArray(event?.refs) ? event.refs : [];
  const topicRef = refs.find((ref) => String(ref).startsWith("topic:"));
  if (topicRef) {
    const topicId = topicRef.slice("topic:".length);
    const topic = QA_TOPICS.find((t) => t.id === topicId);
    if (topic) {
      return {
        group_ref: `topic:${topicId}`,
        group_type: "topic",
        display_name: topic.title || topicId,
        priority: topic.priority || "",
      };
    }
  }
  const threadId = String(event?.thread_id ?? "").trim();
  if (threadId) {
    const topic = QA_TOPICS.find((t) => t.thread_id === threadId);
    if (topic) {
      return {
        group_ref: `topic:${topic.id}`,
        group_type: "topic",
        display_name: topic.title || topic.id,
        priority: topic.priority || "",
      };
    }
    const board = QA_BOARDS.find((b) => b.thread_id === threadId);
    if (board) {
      return {
        group_ref: `board:${board.id}`,
        group_type: "board",
        display_name: board.title || board.id,
        priority: "",
      };
    }
  }
  const boardRef = refs.find((ref) => String(ref).startsWith("board:"));
  if (boardRef) {
    const boardId = boardRef.slice("board:".length);
    const board = QA_BOARDS.find((b) => b.id === boardId);
    if (board) {
      return {
        group_ref: `board:${boardId}`,
        group_type: "board",
        display_name: board.title || boardId,
        priority: "",
      };
    }
  }
  return null;
}

function qaHomeUnreadResponse(homeState) {
  const minTimestamp =
    homeState === "recent"
      ? Date.parse(QA_FIXED_NOW_ISO) - 4 * 60 * 60 * 1000
      : 0;
  const events =
    homeState === "empty"
      ? []
      : QA_EVENTS.filter((event) => {
          if (!QA_HOME_FEED_TYPES.has(String(event.type))) return false;
          if (Date.parse(event.ts) < minTimestamp) return false;
          return Boolean(homeGroupForEvent(event));
        });
  const groupMap = new Map();
  for (const event of events) {
    const groupMeta = homeGroupForEvent(event);
    if (!groupMeta) continue;
    const group = groupMap.get(groupMeta.group_ref) ?? {
      ...groupMeta,
      events: [],
      unread_count: 0,
      newest_event: null,
      read_cursor: null,
    };
    group.events.push(event);
    group.unread_count += 1;
    if (
      !group.newest_event ||
      Date.parse(event.ts) > Date.parse(group.newest_event.ts)
    ) {
      group.newest_event = event;
    }
    groupMap.set(groupMeta.group_ref, group);
  }
  const groups = Array.from(groupMap.values())
    .map((group) => ({
      group_ref: group.group_ref,
      group_type: group.group_type,
      display_name: group.display_name,
      priority: group.priority,
      events: group.events.sort((left, right) => {
        const tsDelta = Date.parse(right.ts) - Date.parse(left.ts);
        return tsDelta || String(right.id).localeCompare(String(left.id));
      }),
      unread_count: group.unread_count,
      newest_event: group.newest_event,
    }))
    .sort((left, right) => {
      const tsDelta =
        Date.parse(right.newest_event?.ts ?? "") -
        Date.parse(left.newest_event?.ts ?? "");
      return (
        tsDelta || String(left.group_ref).localeCompare(String(right.group_ref))
      );
    });
  return {
    groups,
    unread_count: groups.reduce(
      (count, group) => count + group.unread_count,
      0,
    ),
    group_count: groups.length,
    generated_at: QA_FIXED_NOW_ISO,
  };
}

function jsonResponse(status, body) {
  return {
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  };
}

async function waitForServer(baseUrl, timeoutMs = 30_000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(baseUrl, {
        signal: AbortSignal.timeout(2_000),
      });
      if (response.ok || response.status < 500) {
        return;
      }
    } catch {
      // retry until timeout
    }
    await delay(250);
  }
  throw new Error(`Timed out waiting for preview server at ${baseUrl}`);
}

async function runCommand(cmd, args, options = {}) {
  const child = spawn(cmd, args, {
    cwd: projectRoot,
    stdio: "inherit",
    shell: false,
    ...options,
  });

  await new Promise((resolve, reject) => {
    child.on("error", reject);
    child.on("exit", (code) => {
      if (code === 0) {
        resolve();
        return;
      }
      reject(new Error(`${cmd} ${args.join(" ")} exited with code ${code}`));
    });
  });
}

async function ensureBuild() {
  await runCommand("pnpm", ["exec", "vite", "build"]);
}

async function startMockCoreServer(port = DEFAULT_CORE_PORT) {
  const commandRegistryDigest = await getExpectedCommandRegistryDigest();

  const server = http.createServer((request, response) => {
    const url = new URL(request.url ?? "/", `http://127.0.0.1:${port}`);
    if (request.method === "GET" && url.pathname === "/meta/handshake") {
      response.writeHead(200, { "content-type": "application/json" });
      response.end(
        JSON.stringify({
          core_version: "qa-mock-core",
          api_version: "qa",
          schema_version: EXPECTED_SCHEMA_VERSION,
          command_registry_digest: commandRegistryDigest,
          min_cli_version: "",
          recommended_cli_version: "",
          cli_download_url: "",
          core_instance_id: "qa-baseline",
          dev_actor_mode: false,
          human_auth_mode: "workspace_local",
        }),
      );
      return;
    }

    if (request.method === "GET" && url.pathname === "/version") {
      response.writeHead(200, { "content-type": "application/json" });
      response.end(
        JSON.stringify({
          schema_version: EXPECTED_SCHEMA_VERSION,
          command_registry_digest: commandRegistryDigest,
        }),
      );
      return;
    }

    response.writeHead(404, { "content-type": "application/json" });
    response.end(JSON.stringify({ error: { message: "not found" } }));
  });

  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", resolve);
  });

  return {
    async stop() {
      await new Promise((resolve, reject) => {
        server.close((error) => {
          if (error) {
            reject(error);
            return;
          }
          resolve();
        });
      });
    },
  };
}

async function startBuiltUiServer(port) {
  const qaWorkspaceCatalog = JSON.stringify([
    {
      organizationSlug: "local",
      slug: "local",
      label: "Local QA Workspace",
      coreBaseUrl: `http://127.0.0.1:${DEFAULT_CORE_PORT}`,
    },
  ]);
  const child = spawn("node", ["build/index.js"], {
    cwd: projectRoot,
    stdio: "inherit",
    shell: false,
    env: {
      ...process.env,
      HOST: "127.0.0.1",
      PORT: String(port),
      ORIGIN: `http://127.0.0.1:${port}`,
      ANX_WORKSPACES: qaWorkspaceCatalog,
      ANX_UI_CSP_SCRIPT_SRC_EXTRA: "'unsafe-inline'",
    },
  });

  const baseUrl = `http://127.0.0.1:${port}`;
  try {
    await waitForServer(baseUrl);
  } catch (error) {
    child.kill("SIGTERM");
    throw error;
  }

  return {
    baseUrl,
    async stop() {
      child.kill("SIGTERM");
      await Promise.race([
        new Promise((resolve) => child.once("exit", resolve)),
        delay(5_000).then(() => {
          child.kill("SIGKILL");
        }),
      ]);
    },
  };
}

export function buildSceneUrl(baseUrl, scenePath) {
  const separator = scenePath.includes("?") ? "&" : "?";
  return `${baseUrl}${scenePath}${separator}qa=1`;
}

export async function installQaEnvironment(page, scene) {
  await page.addInitScript(
    ({ fixedNowIso, sceneLocalStorage }) => {
      const fixedNowMs = Date.parse(fixedNowIso);
      const RealDate = Date;

      class QADate extends RealDate {
        constructor(...args) {
          if (args.length === 0) {
            super(fixedNowMs);
            return;
          }
          super(...args);
        }

        static now() {
          return fixedNowMs;
        }
      }

      Object.setPrototypeOf(QADate, RealDate);
      globalThis.Date = QADate;

      let seed = 1337;
      Math.random = () => {
        seed = (seed * 48271) % 0x7fffffff;
        return seed / 0x7fffffff;
      };

      for (const [key, value] of Object.entries(sceneLocalStorage ?? {})) {
        if (value == null) {
          localStorage.removeItem(key);
          continue;
        }
        localStorage.setItem(key, String(value));
      }

      if (document.documentElement) {
        document.documentElement.dataset.qa = "1";
      }
    },
    {
      fixedNowIso: QA_FIXED_NOW_ISO,
      sceneLocalStorage: scene.localStorage ?? {},
    },
  );
}

export async function installQaRoutes(page, scene) {
  const workspaceScenario = createWorkspaceScenario(scene.workspaceMode);

  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const pathname = normalizePathname(url.pathname);

    if (
      pathname.startsWith("/_app/") ||
      pathname.startsWith("/assets/") ||
      pathname === "/favicon.svg" ||
      pathname === "/apple-touch-icon.png" ||
      pathname === "/icon-192.png" ||
      pathname === "/icon-512.png" ||
      pathname === "/manifest.json" ||
      pathname === "/robots.txt"
    ) {
      await route.continue();
      return;
    }

    if (await routeExtensionRequest({ route, request, url, scene })) return;

    await handleWorkspaceApiRoute(
      route,
      request,
      url,
      pathname,
      workspaceScenario,
    );
  });
}

async function handleWorkspaceApiRoute(
  route,
  request,
  url,
  pathname,
  scenario,
) {
  if (pathname === "/overview" && request.method() === "GET") {
    await route.fulfill(jsonResponse(200, overviewSnapshot(scenario)));
    return;
  }

  if (pathname === "/auth/session" && request.method() === "GET") {
    await route.fulfill(jsonResponse(200, { agent: QA_AUTH_AGENT }));
    return;
  }

  if (pathname === "/meta/handshake" && request.method() === "GET") {
    await route.fulfill(
      jsonResponse(200, {
        dev_actor_mode: false,
        human_auth_mode: "passkey",
      }),
    );
    return;
  }

  if (pathname === "/actors" && request.method() === "GET") {
    const items = filterByQuery(QA_ACTORS, url.searchParams.get("q"), [
      "id",
      "display_name",
      "tags",
    ]);
    await route.fulfill(
      jsonResponse(200, {
        actors: sliceByLimit(items, url.searchParams),
      }),
    );
    return;
  }

  if (pathname === "/auth/admins" && request.method() === "GET") {
    await route.fulfill(jsonResponse(200, { admins: QA_AUTH_ADMINS }));
    return;
  }

  if (pathname === "/auth/principals" && request.method() === "GET") {
    await route.fulfill(
      jsonResponse(200, {
        principals: sliceByLimit(QA_PRINCIPALS, url.searchParams),
        active_human_principal_count: 1,
        next_cursor: "",
      }),
    );
    return;
  }

  if (pathname === "/agents" && request.method() === "GET") {
    const agents =
      scenario.overviewState === "empty" ||
      scenario.overviewState === "first-run"
        ? []
        : QA_AGENTS;
    await route.fulfill(jsonResponse(200, { agents }));
    return;
  }

  if (pathname === "/hosts" && request.method() === "GET") {
    await route.fulfill(jsonResponse(200, { hosts: QA_HOSTS }));
    return;
  }

  if (
    pathname === "/auth/hosts/enrollments/pending" &&
    request.method() === "GET"
  ) {
    await route.fulfill(
      jsonResponse(200, { enrollments: QA_HOST_ENROLLMENTS }),
    );
    return;
  }

  if (
    pathname === "/auth/hosts/enrollment-tokens" &&
    request.method() === "GET"
  ) {
    await route.fulfill(jsonResponse(200, { enrollment_tokens: [] }));
    return;
  }

  if (pathname === "/auth/invites" && request.method() === "GET") {
    await route.fulfill(
      jsonResponse(200, {
        invites: sliceByLimit(QA_INVITES, url.searchParams),
      }),
    );
    return;
  }

  if (pathname === "/auth/audit" && request.method() === "GET") {
    await route.fulfill(
      jsonResponse(200, {
        events: sliceByLimit(QA_AUTH_AUDIT, url.searchParams),
        next_cursor: "",
      }),
    );
    return;
  }

  if (pathname === "/secrets" && request.method() === "GET") {
    await route.fulfill(
      jsonResponse(200, {
        secrets: sliceByLimit(QA_SECRETS, url.searchParams),
      }),
    );
    return;
  }

  const revealSecretMatch = pathname.match(/^\/secrets\/([^/]+)\/reveal$/);
  if (revealSecretMatch && request.method() === "POST") {
    await route.fulfill(
      jsonResponse(200, {
        value: "sk-qa-visible-secret-value",
      }),
    );
    return;
  }

  if (pathname === "/home/unread" && request.method() === "GET") {
    await route.fulfill(
      jsonResponse(200, qaHomeUnreadResponse(scenario.homeState)),
    );
    return;
  }

  if (pathname === "/home/read" && request.method() === "POST") {
    await route.fulfill(
      jsonResponse(200, {
        ok: true,
        group_count: 0,
        generated_at: QA_FIXED_NOW_ISO,
      }),
    );
    return;
  }

  if (pathname === "/inbox" && request.method() === "GET") {
    if (scenario.inboxState === "loading") {
      await new Promise(() => {});
      return;
    }
    if (scenario.inboxState === "error") {
      await route.fulfill(
        jsonResponse(500, { error: { message: "QA baseline inbox failure." } }),
      );
      return;
    }
    const status = url.searchParams.get("status") || "open";
    let items = [];
    if (scenario.overviewState === "first-run" && status !== "completed") {
      items = [overviewFirstRunInboxItem()];
    } else if (scenario.overviewState === "empty") {
      items = [];
    } else if (scenario.inboxState === "populated" && status !== "completed") {
      items = QA_INBOX_POPULATED;
    }
    await route.fulfill(
      jsonResponse(200, {
        items,
        generated_at: QA_FIXED_NOW_ISO,
      }),
    );
    return;
  }

  if (pathname === "/work" && request.method() === "GET") {
    const work =
      scenario.overviewState === "populated" ? overviewPopulatedWork() : [];
    await route.fulfill(jsonResponse(200, { work, next_cursor: "" }));
    return;
  }

  if (pathname === "/pm/decisions" && request.method() === "GET") {
    await route.fulfill(jsonResponse(200, { items: [], has_more: false }));
    return;
  }

  if (pathname === "/pm/actions" && request.method() === "GET") {
    await route.fulfill(jsonResponse(200, { items: [], has_more: false }));
    return;
  }

  const inboxItemMatch = pathname.match(/^\/inbox\/([^/]+)$/);
  if (inboxItemMatch && request.method() === "GET") {
    if (scenario.askState === "error") {
      await route.fulfill(
        jsonResponse(500, {
          error: { message: "QA baseline ask context failed." },
        }),
      );
      return;
    }
    if (inboxItemMatch[1] !== QA_ASK_ITEM.id) {
      await route.fulfill(
        jsonResponse(404, { error: { message: "not found" } }),
      );
      return;
    }
    await route.fulfill(jsonResponse(200, { item: QA_ASK_ITEM }));
    return;
  }

  if (pathname === "/topics" && request.method() === "GET") {
    const items = filterByQuery(QA_TOPICS, url.searchParams.get("q"), [
      "id",
      "thread_id",
      "title",
      "current_summary",
      "tags",
    ]);
    await route.fulfill(
      jsonResponse(200, {
        topics: sliceByLimit(items, url.searchParams),
      }),
    );
    return;
  }

  if (pathname === "/boards" && request.method() === "GET") {
    const items = filterByQuery(QA_BOARDS, url.searchParams.get("q"), [
      "id",
      "title",
      "labels",
      "owners",
    ]);
    const rows = qaBoardListRows(sliceByLimit(items, url.searchParams));
    await route.fulfill(
      jsonResponse(200, {
        boards: rows,
      }),
    );
    return;
  }

  if (pathname === "/docs" && request.method() === "GET") {
    const source =
      scenario.overviewState === "populated"
        ? [overviewFleetDocument(), ...QA_DOCUMENTS]
        : scenario.overviewState === "empty" ||
            scenario.overviewState === "first-run"
          ? []
          : QA_DOCUMENTS;
    const items = filterByQuery(source, url.searchParams.get("q"), [
      "id",
      "title",
      "labels",
    ]);
    await route.fulfill(
      jsonResponse(200, {
        documents: sliceByLimit(items, url.searchParams),
      }),
    );
    return;
  }

  const documentMatch = pathname.match(/^\/docs\/([^/]+)$/);
  if (documentMatch && request.method() === "GET") {
    const documentId = decodeURIComponent(documentMatch[1]);
    if (documentId === "doc-fleet-dashboard") {
      await route.fulfill(jsonResponse(200, overviewFleetDetail()));
      return;
    }
    const detail = qaDocumentDetail(documentId);
    if (!detail) {
      await route.fulfill(
        jsonResponse(404, { error: { message: "not found" } }),
      );
      return;
    }
    await route.fulfill(jsonResponse(200, detail));
    return;
  }

  const documentRevisionsMatch = pathname.match(/^\/docs\/([^/]+)\/revisions$/);
  if (documentRevisionsMatch && request.method() === "GET") {
    const detail = qaDocumentDetail(
      decodeURIComponent(documentRevisionsMatch[1]),
    );
    if (!detail) {
      await route.fulfill(
        jsonResponse(404, { error: { message: "not found" } }),
      );
      return;
    }
    await route.fulfill(jsonResponse(200, { revisions: [detail.revision] }));
    return;
  }

  const threadTimelineMatch = pathname.match(/^\/threads\/([^/]+)\/timeline$/);
  if (threadTimelineMatch && request.method() === "GET") {
    const timeline = qaDocumentDetailTimeline(
      decodeURIComponent(threadTimelineMatch[1]),
    );
    if (!timeline) {
      await route.fulfill(
        jsonResponse(404, { error: { message: "not found" } }),
      );
      return;
    }
    await route.fulfill(jsonResponse(200, timeline));
    return;
  }

  if (pathname === "/artifacts" && request.method() === "GET") {
    const items = filterByQuery(QA_ARTIFACTS, url.searchParams.get("q"), [
      "id",
      "summary",
      "refs",
    ]);
    await route.fulfill(
      jsonResponse(200, {
        artifacts: sliceByLimit(items, url.searchParams),
      }),
    );
    return;
  }

  if (pathname === "/events" && request.method() === "GET") {
    await route.fulfill(
      jsonResponse(200, {
        events: sliceByLimit(QA_EVENTS, url.searchParams),
      }),
    );
    return;
  }

  await route.continue();
}

async function captureScene(browser, baseUrl, scene, outDir) {
  const context = await browser.newContext({
    viewport: DEFAULT_VIEWPORT,
    deviceScaleFactor: 2,
    colorScheme: "dark",
    reducedMotion: "reduce",
  });

  try {
    const page = await context.newPage();
    await installQaEnvironment(page, scene);
    await installQaRoutes(page, scene);
    await page.goto(buildSceneUrl(baseUrl, scene.path), {
      waitUntil: "domcontentloaded",
      timeout: 30_000,
    });
    await scene.waitFor(page);
    await page.waitForTimeout(150);
    await page.evaluate(async () => {
      if (document.fonts?.ready) {
        await document.fonts.ready;
      }
    });

    const outputPath = path.join(outDir, `${scene.name}.png`);
    await page.screenshot({
      path: outputPath,
      fullPage: false,
      animations: "disabled",
    });

    // Geometry audit of the same state, at the capture size and at phone
    // width (screenshots only cover desktop).
    const layoutViolations = [];
    for (const viewport of [DEFAULT_VIEWPORT, QA_AUDIT_MOBILE_VIEWPORT]) {
      await page.setViewportSize(viewport);
      const { violations } = await auditLayout(page);
      if (violations.length > 0) {
        layoutViolations.push(
          formatViolations(`${scene.name} @${viewport.width}px`, violations),
        );
      }
    }
    return {
      scene: scene.name,
      path: outputPath,
      status: "ok",
      layoutViolations,
    };
  } finally {
    await context.close();
  }
}

function scenesForNames(sceneNames) {
  if (!sceneNames?.length) return QA_SCENES;
  const wanted = new Set(sceneNames);
  const scenes = QA_SCENES.filter((scene) => wanted.has(scene.name));
  const known = new Set(scenes.map((scene) => scene.name));
  const missing = sceneNames.filter((name) => !known.has(name));
  if (missing.length) {
    throw new Error(`Unknown QA scene: ${missing.join(", ")}`);
  }
  return scenes;
}

async function captureAllScenes({ outDir, port, sceneNames }) {
  const scenes = scenesForNames(sceneNames);
  await ensureBuild();
  if (!sceneNames?.length) {
    await rm(outDir, { recursive: true, force: true });
  }
  await mkdir(outDir, { recursive: true });

  const core = await startMockCoreServer();
  const preview = await startBuiltUiServer(port);
  const browser = await chromium.launch({
    headless: true,
    // Opt-in: use an installed browser (e.g. PLAYWRIGHT_CHANNEL=chrome).
    channel: process.env.PLAYWRIGHT_CHANNEL || undefined,
  });

  try {
    const results = [];
    for (const scene of scenes) {
      results.push(await captureScene(browser, preview.baseUrl, scene, outDir));
    }
    return results;
  } finally {
    await browser.close();
    await preview.stop();
    await core.stop();
  }
}

async function diffPngFiles(baselinePath, currentPath, diffPath) {
  const [baselineBuffer, currentBuffer] = await Promise.all([
    readFile(baselinePath),
    readFile(currentPath),
  ]);

  const baselinePng = PNG.sync.read(baselineBuffer);
  const currentPng = PNG.sync.read(currentBuffer);

  if (
    baselinePng.width !== currentPng.width ||
    baselinePng.height !== currentPng.height
  ) {
    return {
      mismatchPixels: Number.POSITIVE_INFINITY,
      mismatchRatio: Number.POSITIVE_INFINITY,
      dimensionMismatch: {
        baseline: `${baselinePng.width}x${baselinePng.height}`,
        current: `${currentPng.width}x${currentPng.height}`,
      },
    };
  }

  const diffPng = new PNG({
    width: baselinePng.width,
    height: baselinePng.height,
  });

  const mismatchPixels = pixelmatch(
    baselinePng.data,
    currentPng.data,
    diffPng.data,
    baselinePng.width,
    baselinePng.height,
    {
      threshold: 0.1,
      includeAA: false,
    },
  );

  const mismatchRatio =
    mismatchPixels / (baselinePng.width * baselinePng.height);

  if (mismatchPixels > 0) {
    await writeFile(diffPath, PNG.sync.write(diffPng));
  }

  return {
    mismatchPixels,
    mismatchRatio,
    dimensionMismatch: null,
  };
}

export async function runQaVisualCommand(options) {
  const sceneNames = options.sceneNames?.filter(Boolean) ?? [];
  if (options.mode === "baseline") {
    const results = await captureAllScenes({
      outDir: options.outDir,
      port: options.port,
      sceneNames,
    });
    const summary = {
      mode: "baseline",
      outDir: options.outDir,
      fixedNow: QA_FIXED_NOW_ISO,
      scenes: results.map((result) => result.scene),
    };
    if (options.json) {
      console.log(JSON.stringify(summary, null, 2));
    } else {
      console.log(
        `Captured ${results.length} QA baseline screenshots into ${options.outDir}`,
      );
    }
    return 0;
  }

  const captures = await captureAllScenes({
    outDir: options.outDir,
    port: options.port,
    sceneNames,
  });

  await rm(QA_DIFF_DIR, { recursive: true, force: true });
  await mkdir(QA_DIFF_DIR, { recursive: true });

  const failures = [];
  const matches = [];

  for (const capture of captures) {
    for (const report of capture.layoutViolations ?? []) {
      failures.push({
        scene: capture.scene,
        reason: `layout audit\n${report}`,
      });
    }
  }

  for (const scene of scenesForNames(sceneNames)) {
    const baselinePath = path.join(QA_BASELINE_DIR, `${scene.name}.png`);
    const currentPath = path.join(options.outDir, `${scene.name}.png`);
    const diffPath = path.join(QA_DIFF_DIR, `${scene.name}.diff.png`);

    try {
      const result = await diffPngFiles(baselinePath, currentPath, diffPath);
      if (result.dimensionMismatch) {
        failures.push({
          scene: scene.name,
          reason: `dimension mismatch (${result.dimensionMismatch.baseline} vs ${result.dimensionMismatch.current})`,
        });
        continue;
      }

      const thresholdRatio =
        typeof scene.thresholdRatio === "number"
          ? scene.thresholdRatio
          : options.thresholdRatio;
      if (result.mismatchRatio > thresholdRatio) {
        failures.push({
          scene: scene.name,
          reason: `${(result.mismatchRatio * 100).toFixed(3)}% pixels differ (${result.mismatchPixels} px; threshold ${(thresholdRatio * 100).toFixed(3)}%)`,
        });
      } else {
        matches.push(scene.name);
      }
    } catch (error) {
      failures.push({
        scene: scene.name,
        reason: error instanceof Error ? error.message : String(error),
      });
    }
  }

  const summary = {
    mode: "diff",
    fixedNow: QA_FIXED_NOW_ISO,
    currentDir: options.outDir,
    baselineDir: QA_BASELINE_DIR,
    diffDir: QA_DIFF_DIR,
    thresholdRatio: options.thresholdRatio,
    ok: failures.length === 0,
    matches,
    failures,
  };

  if (options.json) {
    console.log(JSON.stringify(summary, null, 2));
  } else if (failures.length === 0) {
    console.log(
      `QA diff passed: ${matches.length}/${QA_SCENES.length} scenes within thresholds (default max diff ${(options.thresholdRatio * 100).toFixed(3)}%; some scenes allow more).`,
    );
  } else {
    console.error(
      `QA diff failed: ${failures.length}/${QA_SCENES.length} scenes exceeded ${(options.thresholdRatio * 100).toFixed(3)}%.`,
    );
    for (const failure of failures) {
      console.error(`  - ${failure.scene}: ${failure.reason}`);
    }
    console.error(`Diff images written to ${QA_DIFF_DIR}`);
  }

  return failures.length === 0 ? 0 : 1;
}

if (
  process.argv[1] &&
  fileURLToPath(import.meta.url) === path.resolve(process.argv[1])
) {
  runQaVisualCommand(parseCliArgs(process.argv))
    .then((code) => {
      process.exit(code);
    })
    .catch((error) => {
      console.error(
        "qa-visual failed:",
        error instanceof Error ? error.message : error,
      );
      process.exit(1);
    });
}
