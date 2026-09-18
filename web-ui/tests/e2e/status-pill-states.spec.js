import { expect as baseExpect, test } from "@playwright/test";

import { AUDIT_VIEWPORTS, expectCleanLayout } from "../helpers/layoutAudit.js";

// The dev server compiles routes on demand; first paint can be slow under
// parallel runs.
const expect = baseExpect.configure({ timeout: 15_000 });

/**
 * StatusPill takes an arbitrary `label`, not only the short status words, so a
 * long label has to shrink rather than push its row siblings out. These tests
 * pin both halves of that contract:
 *
 *   - a long label stays on one line, ellipsizes, and keeps its row siblings
 *     and its container intact;
 *   - the known short statuses are never truncated at any viewport, which is
 *     the regression risk of making the pill shrinkable.
 */

const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;
const ago = (ms) => new Date(Date.now() - ms).toISOString();

const WS_ID = "ws_0123456789abcdef0123456789abcdef";

// Rendered through formatListValue, which only swaps "_" for " ". Long enough
// that it cannot fit at ANY audit viewport, including a full-width card at
// 1440 — so "truncates" is a real assertion at every size rather than an
// accident of where the grid happens to break.
const LONG_ACCESS_MODE = [
  "read_only_pending_operator_review_after_quota_exhaustion_on_the_primary",
  "shard_and_the_frankfurt_replica_while_the_billing_reconciliation_job",
  "drains_every_pending_workspace_snapshot_from_the_overlay_upper_directory",
  "before_the_next_scheduled_placement_sweep_can_be_allowed_to_run_again",
].join("_");
const LONG_ACCESS_LABEL = LONG_ACCESS_MODE.replaceAll("_", " ");
const LONG_POWER_STATE =
  "stopping_after_operator_requested_drain_of_the_frankfurt_rack";

/** Every known status that must stay readable in full. */
const SHORT_STATUSES = [
  "ready",
  "active",
  "provisioning",
  "pending",
  "failed",
  "error",
  "degraded",
  "suspended",
];

function usage() {
  const gb = 1024 * 1024 * 1024;
  return {
    storage_bytes: 18 * gb,
    db_bytes: 2 * gb,
    blob_bytes: 16 * gb,
    artifact_count: 1284,
    document_count: 912,
    event_count: 24819,
    agent_count: 6,
    workspace_count: 1,
  };
}

function workspaceDetail(overrides = {}) {
  return {
    id: WS_ID,
    organization_id: "org_0123456789abcdef",
    organization_slug: "northwind",
    slug: "northwind-workspace",
    display_name: "Northwind Workspace",
    status: "failed",
    runtime_power_state: "running",
    access_mode: "read_write",
    restriction_reason: "",
    heartbeat_freshness: "stale",
    heartbeat_age_seconds: 93_600,
    heartbeat_version: "0.10.21",
    heartbeat_build: "",
    host_id: "host_0",
    host_label: "packed-host-frankfurt",
    listen_port: 18100,
    container_id_short: "c0123456789",
    runtime_image_tag: "anx-core:2026.05.20",
    runtime_stopped_at: ago(3 * HOUR),
    last_activity_at: ago(17 * MIN),
    last_successful_backup_at: ago(HOUR),
    active_stream_count: 2,
    usage: usage(),
    health_summary: { database: "ok", scheduler: "ok" },
    // One row per short status, so every tone is on the page at once.
    recent_jobs: SHORT_STATUSES.map((status, i) => ({
      id: `wsjob_${i}`,
      kind: i % 2 ? "workspace_start" : "workspace_create",
      status,
      requested_at: ago(i * 40 * MIN),
      failure_reason: "",
    })),
    recent_backup_runs: [
      {
        id: "wsbackup_0",
        schedule_name: "nightly-full-snapshot-retained-35-days",
        status: "ready",
        requested_at: ago(7 * HOUR),
        failure_reason: "",
      },
    ],
    recent_audit_events: [
      {
        id: "wsaudit_0",
        event_type: "workspace_session_exchanged",
        occurred_at: ago(25 * MIN),
      },
    ],
    created_at: ago(4 * DAY),
    updated_at: ago(HOUR),
    ...overrides,
  };
}

/** Minimal admin analytics mock: only what the workspace detail page calls. */
async function installWorkspaceDetail(page, workspace) {
  await page.addInitScript(() => {
    localStorage.setItem("anx_admin_token", "admin-secret");
    localStorage.setItem("anx_admin_actor", "ops@example.com");
    localStorage.setItem("anx_admin_ops_window", "24h");
  });
  await page.route("**/hosted/api/admin/analytics/**", (route) =>
    route.fulfill({
      status: 200,
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ workspace }),
    }),
  );
}

const pills = (page) => page.locator("[data-testid='status-pill']");

/** Geometry of one pill, its row and whether it actually ellipsizes. */
function measurePill(locator) {
  return locator.evaluate((el) => {
    const style = getComputedStyle(el);
    const box = el.getBoundingClientRect();
    const rowBox = (el.closest("div,dd,td,li") ?? el.parentElement)
      .closest("div,dd,td,li")
      .getBoundingClientRect();
    return {
      text: (el.textContent ?? "").trim(),
      title: el.getAttribute("title"),
      whiteSpace: style.whiteSpace,
      textOverflow: style.textOverflow,
      // Horizontal overflow of the clipped box == visually ellipsized.
      truncated: el.scrollWidth > el.clientWidth + 1,
      // truncate is nowrap, so a correct pill is exactly one line tall.
      wrapped: el.scrollHeight > el.clientHeight + 1,
      overflowsRow: box.right > rowBox.right + 1,
      width: box.width,
      rowWidth: rowBox.width,
    };
  });
}

for (const viewport of AUDIT_VIEWPORTS) {
  test.describe(`status pill @ ${viewport.name}`, () => {
    test.describe.configure({ timeout: 120_000 });
    test.use({ viewport: { width: viewport.width, height: viewport.height } });

    test("a long label ellipsizes on one line without pushing its row out", async ({
      page,
    }) => {
      await installWorkspaceDetail(
        page,
        workspaceDetail({
          access_mode: LONG_ACCESS_MODE,
          runtime_power_state: LONG_POWER_STATE,
        }),
      );
      await page.goto(`/hosted/admin/workspaces/${WS_ID}`);

      const longPill = pills(page).filter({ hasText: LONG_ACCESS_LABEL });
      await expect(longPill).toHaveCount(1);

      const m = await measurePill(longPill);
      // Full text is still in the DOM (assistive tech) and on hover.
      expect(m.text).toBe(LONG_ACCESS_LABEL);
      expect(m.title).toBe(LONG_ACCESS_LABEL);
      // One line, clipped with an ellipsis rather than wrapped or overflowing.
      expect(m.whiteSpace).toBe("nowrap");
      expect(m.textOverflow).toBe("ellipsis");
      expect(m.wrapped).toBe(false);
      expect(m.truncated).toBe(true);
      expect(m.overflowsRow).toBe(false);
      expect(m.width).toBeLessThanOrEqual(m.rowWidth + 1);

      // The pill's row sibling ("Access") is still on screen, i.e. the pill
      // shrank instead of shoving it out of the row.
      const accessTerm = page.getByText("Access", { exact: true });
      await expect(accessTerm).toBeVisible();
      const termBox = await accessTerm.boundingBox();
      expect(termBox.x).toBeGreaterThanOrEqual(0);
      expect(termBox.x + termBox.width).toBeLessThanOrEqual(viewport.width);

      await expectCleanLayout(page, "workspace detail, long status labels", {
        scrollPositions: ["top", "bottom"],
      });
    });

    test("short statuses are never truncated", async ({ page }) => {
      await installWorkspaceDetail(page, workspaceDetail());
      await page.goto(`/hosted/admin/workspaces/${WS_ID}`);

      // Every short status word is on the page via recent_jobs.
      await expect(pills(page).first()).toBeVisible();
      for (const status of SHORT_STATUSES) {
        const pill = pills(page).filter({ hasText: new RegExp(`^${status}$`) });
        await expect(pill.first()).toBeVisible();
        const m = await measurePill(pill.first());
        expect(m.text).toBe(status);
        // The regression risk of making the pill shrinkable: a short word must
        // still render in full, with no clipping, at every viewport.
        expect(m.truncated).toBe(false);
        expect(m.wrapped).toBe(false);
        expect(m.overflowsRow).toBe(false);
      }

      await expectCleanLayout(page, "workspace detail, short statuses", {
        scrollPositions: ["top", "bottom"],
      });
    });
  });
}
