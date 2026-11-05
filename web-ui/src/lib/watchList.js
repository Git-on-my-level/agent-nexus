/**
 * Watch's Initiatives list: which section each top-level initiative reads
 * in, its one status line, its bar, and the header's counts.
 *
 * Presentation only. Every count, state, promise and date here is one core
 * computed in `brief.v1` (the initiative row and its `rollup`); the rules
 * below only decide where a row reads. Parts carry the brief field they came
 * from so the renderer can tag them (I1).
 */
import { ageSeconds, formatDay, formatSpan, plainInline } from "$lib/brief.js";
import { formatAgo } from "$lib/time/format.js";
import { buildLanes } from "$lib/swimlanes.js";

/** No evidence for this long reads as stale. */
export const STALE_DAYS = 7;
const STALE_SECONDS = STALE_DAYS * 86_400;

/** Recent changes look back this far. */
export const CHANGES_DAYS = 7;
export const CHANGES_PREVIEW = 5;

/**
 * One section per initiative, in reading order; the first rule that matches
 * wins. Done folds away under the list.
 */
export const SECTIONS = [
  { key: "needs_you", label: "Needs you", word: "need you", tone: "warn" },
  { key: "lagging", label: "Lagging", word: "lagging", tone: "danger" },
  { key: "blocked", label: "Blocked", word: "blocked", tone: "danger" },
  { key: "stale", label: "Stale", word: "stale", tone: "muted" },
  { key: "moving", label: "Moving", word: "moving", tone: "accent" },
  { key: "proposed", label: "Proposed", word: "proposed", tone: "subtle" },
  { key: "done", label: "Done", word: "done", tone: "ok" },
];
const RANK = Object.fromEntries(SECTIONS.map((s, i) => [s.key, i]));

/** Children past their promise, and how many children core counted. */
function pastOf(row) {
  return {
    past: Math.max(0, row.rollup?.pastPromise ?? 0),
    total: Math.max(0, row.rollup?.total ?? 0),
  };
}

/**
 * Lagging: the initiative's own promise is past, or at least two children
 * are past theirs and they are at least a third of the children. One late
 * child alone does not lag the initiative.
 */
export function isLagging(row) {
  if (row.state === "past_promise") return true;
  const { past, total } = pastOf(row);
  return past >= 2 && total > 0 && past * 3 >= total;
}

/** Children in the blocked phase. */
export function blockedChildren(row) {
  return Math.max(0, Number(row.rollup?.byState?.blocked) || 0);
}

/**
 * Blocked: core's `blocked_count` (open asks about the initiative or that
 * it blocks on) or a child in the blocked phase.
 */
export function isBlocked(row) {
  return (row.blockedCount ?? 0) > 0 || blockedChildren(row) > 0;
}

/**
 * Stale: dated evidence, none of it in the last week. An initiative with no
 * evidence yet has not gone quiet; only a dated gap counts.
 */
export function isStale(row, now = Date.now()) {
  const age = ageSeconds(row.lastEvidenceAt, now);
  return age !== null && age >= STALE_SECONDS;
}

export function sectionOf(row, now = Date.now()) {
  if (row.state === "done") return "done";
  if (row.waitingOnYouCount > 0 || row.rollup?.needingViewer > 0)
    return "needs_you";
  if (isLagging(row)) return "lagging";
  if (isBlocked(row)) return "blocked";
  if (isStale(row, now)) return "stale";
  if (row.proposed) return "proposed";
  return "moving";
}

/**
 * Top-level initiatives (loops and children never count), each with its
 * `section`, in section order and then core's order.
 */
export function watchRows(initiatives, now = Date.now()) {
  return buildLanes(initiatives)
    .map((row, index) => ({ row, index, section: sectionOf(row, now) }))
    .sort((a, b) => RANK[a.section] - RANK[b.section] || a.index - b.index)
    .map(({ row, section }) => ({ ...row, section }));
}

/** `{ needs_you, lagging, …, done }` counts. */
export function sectionCounts(rows) {
  const counts = Object.fromEntries(SECTIONS.map((s) => [s.key, 0]));
  for (const row of rows) counts[row.section] += 1;
  return counts;
}

/** A section's tone, for its dot and bar. */
export function sectionTone(key) {
  return SECTIONS[RANK[key]]?.tone ?? "muted";
}

/** `[{ key, label, rows }]` in reading order, empty sections dropped. */
export function groupSections(rows) {
  return SECTIONS.map((s) => ({
    ...s,
    rows: rows.filter((row) => row.section === s.key),
  })).filter((s) => s.rows.length);
}

/** Sections the header counts, with the brief field each count reads. */
const SUMMARY = [
  ["needs_you", "initiatives.waiting_on_you_count"],
  ["lagging", "initiatives.rollup.past_promise"],
  ["blocked", "initiatives.blocked_count"],
  ["stale", "initiatives.last_evidence_at"],
];

/**
 * The header line, every count on one basis (open initiatives): "5 open ·
 * 2 need you · 3 lagging · 1 stale". Zero counts are left out. When core
 * truncated the initiatives section the open count reads "50+ open".
 *
 * @returns {{ key: string, text: string, field: string }[]}
 */
export function watchSummary(rows, { truncated = false } = {}) {
  const counts = sectionCounts(rows);
  const open = rows.length - counts.done;
  // Core caps the section; a capped list is a lower bound, not a total.
  const more = truncated ? "+" : "";
  return [
    { key: "open", text: `${open}${more} open`, field: "initiatives.state" },
    ...SUMMARY.filter(([key]) => counts[key]).map(([key, field]) => ({
      key,
      text: `${counts[key]} ${SECTIONS[RANK[key]].word}`,
      field,
    })),
  ];
}

/**
 * Over target, on the same basis as the header: open initiatives against
 * the board's target. Null within target or with no target.
 */
export function overTargetNotice(rows, budget, overTarget = null) {
  const target = budget?.target ?? overTarget?.target;
  if (target == null) return null;
  const open = rows.filter((row) => row.section !== "done").length;
  return open > target ? { open, target } : null;
}

/**
 * A row's bar: core's plan progress when there is a plan, else its
 * children done (or cancelled) out of all of them. Null with neither.
 *
 * @returns {{ done: number, total: number, field: string, unit: string } | null}
 */
export function rowProgress(row) {
  if (row.progress?.total > 0)
    return {
      done: Math.min(row.progress.done, row.progress.total),
      total: row.progress.total,
      field: "initiatives.progress",
      unit: row.progress.total === 1 ? "step" : "steps",
    };
  const total = row.rollup?.total ?? 0;
  if (!total) return null;
  const by = row.rollup.byState ?? {};
  const done = Math.min(total, (by.done ?? 0) + (by.cancelled ?? 0));
  return {
    done,
    total,
    field: "initiatives.rollup.by_state",
    unit: total === 1 ? "child" : "children",
  };
}

/**
 * The one line under a title: what is late, what is stuck, how long it has
 * been quiet, then the stated next step.
 *
 * `label` reads inside the tagged part ("next: …"); `prefix` reads before
 * it, untagged ("own promise" before "10h past").
 *
 * @returns {{ text: string, field: string, tone?: string, label?: string, prefix?: string }[]}
 */
export function statusParts(row, now = Date.now()) {
  const parts = [];
  const { past, total } = pastOf(row);
  if (row.state === "past_promise")
    parts.push({
      prefix: "own promise",
      text: `${formatSpan(row.pastPromiseFor)} past`,
      field: "initiatives.past_promise_for",
      tone: "danger",
    });
  if (past > 0)
    parts.push({
      text: `${past} of ${total} past`,
      field: "initiatives.rollup.past_promise",
      tone: isLagging(row) ? "warn" : undefined,
    });
  const blocked = blockedChildren(row);
  if (blocked)
    parts.push({
      text: `${blocked} blocked`,
      field: "initiatives.rollup.by_state",
      tone: "danger",
    });
  const asks = Math.max(
    0,
    (row.blockedCount ?? 0) - (row.waitingOnYouCount ?? 0),
  );
  if (asks)
    parts.push({
      text: `${asks} open ${asks === 1 ? "ask" : "asks"}`,
      field: "initiatives.blocked_count",
    });
  if (row.state !== "done" && isStale(row, now))
    parts.push({
      text: `last evidence ${formatAgo(row.lastEvidenceAt, now)}`,
      field: "initiatives.last_evidence_at",
    });
  const what = plainInline(row.promise?.what);
  if (row.state === "done") {
    const at = formatAgo(row.lastEvidenceAt, now);
    if (at)
      parts.push({ text: `done ${at}`, field: "initiatives.last_evidence_at" });
  } else if (what) {
    // Word for word what the initiative page says, so the two agree.
    parts.push({
      text: `${what}${row.promise.at ? ` · ${formatDay(row.promise.at)}` : ""}`,
      field: "initiatives.promise",
      label: "next:",
    });
  }
  if (!parts.length)
    parts.push({ text: "no next step yet", field: "initiatives.promise" });
  return parts;
}

/** The status line as one string: "own promise 4d past · next: …". */
export function lineText(parts) {
  return parts
    .map((p) => [p.prefix, p.label, p.text].filter(Boolean).join(" "))
    .join(" · ");
}

/**
 * The right-hand status: the reason the row is in its section, and when it
 * is due.
 *
 * @returns {{ text: string, tone: string, field: string, decide?: boolean, sub?: { text: string, field: string } }}
 */
export function rowStatus(row, now = Date.now()) {
  const at = Date.parse(row.promise?.at ?? "");
  const dated = Number.isFinite(at);
  // The status line already dates a stated next step.
  const due =
    dated && !plainInline(row.promise?.what)
      ? {
          text: `${at <= now ? "was due" : "due"} ${formatDay(row.promise.at)}`,
          field: "initiatives.promise",
        }
      : undefined;
  switch (row.section) {
    case "done":
      return { text: "Done", tone: "ok", field: "initiatives.state" };
    case "needs_you":
      return {
        text: "Waiting on you",
        tone: "warn",
        field:
          row.waitingOnYouCount > 0
            ? "initiatives.waiting_on_you_count"
            : "initiatives.rollup.needing_viewer",
        decide: true,
        sub: due,
      };
    case "lagging":
      return row.state === "past_promise"
        ? {
            text: `${formatSpan(row.pastPromiseFor)} past`,
            tone: "danger",
            field: "initiatives.past_promise_for",
            sub: due,
          }
        : {
            text: "Children late",
            tone: "warn",
            field: "initiatives.rollup.past_promise",
            sub: due,
          };
    case "blocked":
      return {
        text: "Blocked",
        tone: "danger",
        field: blockedChildren(row)
          ? "initiatives.rollup.by_state"
          : "initiatives.blocked_count",
        sub: due,
      };
    case "stale":
      return {
        text: `Quiet ${formatSpan(ageSeconds(row.lastEvidenceAt, now))}`,
        tone: "muted",
        field: "initiatives.last_evidence_at",
        sub: due,
      };
    case "proposed":
      return {
        text: "Not started",
        tone: "subtle",
        field: "initiatives.proposed",
        sub: due,
      };
    default:
      return row.state === "no_promise"
        ? { text: "No promise", tone: "subtle", field: "initiatives.state" }
        : {
            text: "On track",
            tone: "ok",
            field: "initiatives.state",
            sub: due,
          };
  }
}

/** "21 children", "1 child", "No children". */
export function childrenCount(row) {
  const total = row.rollup?.total ?? 0;
  if (!total) return "No children";
  return `${total} ${total === 1 ? "child" : "children"}`;
}

/**
 * Changes from the last `CHANGES_DAYS` days, newest first. Core sends its
 * newest 50 membership changes; this keeps the recent ones, a constant-size
 * pass over that bounded list.
 */
export function recentChanges(changes, now = Date.now(), days = CHANGES_DAYS) {
  const since = now - days * 86_400_000;
  return (changes?.items ?? []).filter((c) => {
    const at = Date.parse(c.ts);
    return Number.isFinite(at) && at >= since;
  });
}
