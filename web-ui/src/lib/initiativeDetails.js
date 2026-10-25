/**
 * What an expanded Watch row says about one initiative.
 *
 * Most of it is already in the brief row (promise, evidence, the rollup and
 * its child preview, the latest note). The plan's next steps and the owner
 * are not, so the row reads the card once when it opens, through the
 * short-lived `workCache` the initiative page also paints from. Presentation
 * only: core computes every count, state and next step named here.
 */
import { formatAgo } from "$lib/time/format.js";
import { formatDay, plainInline } from "$lib/brief.js";
import { planNextSteps } from "$lib/planNext.js";
import { rawWorkSummary } from "$lib/workSummary.js";
import { label as phaseLabel } from "$lib/pm/presentation.js";
import { columnFor, LANE_COLUMNS, progressSegments } from "$lib/swimlanes.js";

/** Children listed before the rest become a count. */
export const CHILD_PREVIEW = 5;
/** Ready plan steps listed before the rest become a count. */
export const STEP_PREVIEW = 3;

const text = (value) => String(value ?? "").trim();

/** The promise, in one line, with the brief field it came from. */
export function promiseLine(initiative) {
  const promise = initiative?.promise;
  if (initiative?.state === "done")
    return { text: "Done", field: "initiatives.state", tone: "neutral" };
  if (!promise?.at && !promise?.what)
    return {
      text: "Nothing promised yet",
      field: "initiatives.state",
      tone: "muted",
    };
  const what = plainInline(promise.what);
  const when = promise.at ? formatDay(promise.at) : "";
  return {
    text: [what, when ? `by ${when}` : ""].filter(Boolean).join(" · "),
    field: "initiatives.promise",
    tone: initiative.state === "past_promise" ? "warning" : "neutral",
  };
}

/** When evidence last landed, or that none has. */
export function evidenceLine(initiative, now = Date.now()) {
  const at = text(initiative?.lastEvidenceAt);
  return {
    text: at ? `Last evidence ${formatAgo(at, now)}` : "No evidence yet",
    field: "initiatives.last_evidence_at",
  };
}

/**
 * The rollup: core's counts, the by-state bar the swimlane lane draws, and
 * the first children core sent, each with the column it sits in.
 */
export function rollupModel(rollup) {
  const total = rollup?.total ?? 0;
  const children = (rollup?.children ?? []).slice(0, CHILD_PREVIEW);
  const columnLabel = (key) =>
    LANE_COLUMNS.find((column) => column.key === key)?.label ?? "";
  return {
    total,
    pastPromise: rollup?.pastPromise ?? 0,
    needingViewer: rollup?.needingViewer ?? 0,
    segments: progressSegments(rollup).map((segment) => ({
      ...segment,
      label: columnLabel(segment.key).toLowerCase(),
    })),
    children: children.map((child) => ({
      ...child,
      column: columnFor(child),
      stateLabel: child.needsViewer ? "Needs you" : phaseLabel(child.state),
    })),
    // Core's total counts children the preview did not carry, too.
    more: Math.max(0, total - children.length),
    hasMore: Boolean(rollup?.hasMore),
  };
}

/**
 * The plan's next steps and the owner, from one `work.get` (summary=1).
 * Core's own `next` leads, as on the initiative page.
 */
export function planModel(work) {
  if (!work) return null;
  const summary = rawWorkSummary(work);
  const steps = planNextSteps(work.plan, work.plan_state, {
    preferred: text(summary?.next?.id),
  });
  const hasPlan = Array.isArray(work.plan?.steps) && work.plan.steps.length > 0;
  return {
    hasPlan,
    steps: steps.slice(0, STEP_PREVIEW),
    more: Math.max(0, steps.length - STEP_PREVIEW),
    owner: text(work.owner) || text(summary?.owner),
  };
}
