/**
 * Freshness, as an expectation rather than a date.
 *
 * "3d" on its own is not a fact a reader can act on: three days is nothing on
 * a backlog card and a week late on something in progress. So a freshness
 * badge carries both halves — how long it has been, and how long it was
 * supposed to be — and colours itself on the ratio:
 *
 * - **green** — updated within the expectation;
 * - **yellow** — late, but less than twice the expectation;
 * - **red** — beyond twice the expectation.
 *
 * This is what replaces the separate "Stale" text badge. A stale card used to
 * say "Stale" and "3d" in two badges, where the second already implied the
 * first once the expectation was known.
 *
 * Expectations are defaults by kind. Nothing in the contract carries a
 * per-card cadence yet, so `expectationHours` is read from the row when a
 * projection happens to carry `update_expectation_hours` (forward-compatible,
 * as every unknown field here is) and falls back to the default otherwise.
 * A real per-board / per-card field is a follow-up.
 */

import { formatAge } from "./ageBadge.js";
import { formatAbsoluteDateTime } from "./formatDate.js";

const HOUR_MS = 3_600_000;

/**
 * How long each kind of thing may go without an update before it is late.
 * `null` means the kind gets no badge at all — a finished card's age is a fact
 * about history, not something anyone needs to act on.
 */
export const FRESHNESS_EXPECTATION_HOURS = Object.freeze({
  in_progress: 24,
  initiative: 72,
  waiting: 72,
  backlog: 336,
  closed: null,
});

const asText = (value) => String(value ?? "").trim();

/**
 * Which expectation a card's phase falls under.
 *
 * `blocked` and `review` are both "waiting on somebody", which the brief puts
 * at three days. `ready` is waiting to be picked up, so it sits there too — it
 * is neither in flight nor parked in a backlog. A phase Nexus has no name for
 * still needs a look, so it gets the same middle expectation rather than being
 * excused from the badge.
 */
export function freshnessKindForPhase(phase, lifecycleState = "") {
  const state = asText(lifecycleState).toLowerCase();
  if (state === "archived" || state === "trashed") return "closed";
  switch (asText(phase).toLowerCase()) {
    case "in_progress":
      return "in_progress";
    case "backlog":
      return "backlog";
    case "done":
    case "cancelled":
      return "closed";
    default:
      return "waiting";
  }
}

/** An expectation as a badge tooltip writes it: `1d`, `3d`, `14d`, `12h`. */
export function formatExpectation(hours) {
  const value = Number(hours);
  if (!Number.isFinite(value) || value <= 0) return "";
  if (value % 24 === 0) return `${value / 24}d`;
  return `${value}h`;
}

/**
 * The expectation that applies, in hours, or `null` for a kind that gets no
 * badge. An explicit override wins over the row's own field, which wins over
 * the default for the kind.
 */
export function expectationHoursFor(
  kind,
  { override = null, row = null } = {},
) {
  for (const candidate of [override, row?.update_expectation_hours]) {
    const value = Number(candidate);
    if (Number.isFinite(value) && value > 0) return value;
  }
  const fallback = FRESHNESS_EXPECTATION_HOURS[asText(kind)];
  return fallback ?? null;
}

/**
 * One freshness badge, or `null` when there is nothing to show — no instant,
 * an unparseable one, or a kind that is finished and does not get a badge.
 *
 * @param {string|number|Date|null|undefined} at when it last moved
 * @param {{
 *   kind?: string,
 *   expectationHours?: number|null,
 *   row?: object|null,
 *   verb?: string,
 *   now?: number,
 * }} [options]
 * @returns {null | {
 *   age: string, tone: "ok"|"warn"|"danger", state: string,
 *   expectationHours: number, expectation: string, title: string, at: string,
 * }}
 */
export function freshnessModel(at, options = {}) {
  const {
    kind = "in_progress",
    expectationHours = null,
    row = null,
    verb = "updated",
    now = Date.now(),
  } = options;

  const hours = expectationHoursFor(kind, { override: expectationHours, row });
  if (!hours) return null;

  const instant = at ? new Date(at).getTime() : Number.NaN;
  if (!Number.isFinite(instant)) return null;
  const age = formatAge(at, now);
  if (!age) return null;

  const budget = hours * HOUR_MS;
  const elapsed = Number(now) - instant;
  const state =
    elapsed <= budget ? "fresh" : elapsed <= budget * 2 ? "late" : "very_late";
  const tone = state === "fresh" ? "ok" : state === "late" ? "warn" : "danger";

  const expectation = formatExpectation(hours);
  const absolute = formatAbsoluteDateTime(at);
  const action = asText(verb);
  const sentence = action
    ? `${action[0].toUpperCase()}${action.slice(1)} ${absolute}`
    : absolute;
  const expected =
    state === "fresh"
      ? `within the expected ${expectation}`
      : `expected every ${expectation}`;

  return {
    age,
    tone,
    state,
    expectationHours: hours,
    expectation,
    at: typeof at === "string" ? at : new Date(instant).toISOString(),
    title: `${sentence} (${age}) — ${expected}`,
  };
}
