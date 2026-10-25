/**
 * Watch's rollup presentation: how one initiative and the children core
 * rolled into it read on the Initiatives list and on the swimlane board.
 *
 * Presentation only. Every count, state and promise named here is one core
 * computed (`brief.v1` initiatives, their `rollup`, and the reader's open
 * decisions); nothing is inferred from titles. Parts carry the brief field
 * they came from so the renderer can tag them (I1).
 */
import {
  ageSeconds,
  formatDay,
  formatSpan,
  plainInline,
  sortDecisions,
} from "$lib/brief.js";
import { formatAgo } from "$lib/time/format.js";
import { label as phaseLabel } from "$lib/pm/presentation.js";
import { buildLanes } from "$lib/swimlanes.js";

/**
 * One bucket per initiative, in reading order. "Needs you" is an open
 * decision or a child waiting on the reader; "slipping" is a promise past
 * due, the initiative's own or a child's.
 */
export const BUCKETS = [
  { key: "needs_you", label: "need you", tone: "danger" },
  { key: "slipping", label: "slipping", tone: "warn" },
  { key: "moving", label: "moving", tone: "accent" },
  { key: "quiet", label: "no promise", tone: "muted" },
  { key: "done", label: "done", tone: "muted" },
];
const BUCKET_RANK = Object.fromEntries(BUCKETS.map((b, i) => [b.key, i]));

export function bucketOf(row) {
  if (row.state === "done") return "done";
  if (row.waitingOnYouCount > 0 || row.rollup?.needingViewer > 0)
    return "needs_you";
  if (row.state === "past_promise" || row.rollup?.pastPromise > 0)
    return "slipping";
  if (row.state === "moving") return "moving";
  return "quiet";
}

/**
 * Top-level initiatives (loops and children never count), sorted by what
 * needs the reader, then core's order.
 */
export function rollupRows(initiatives) {
  return buildLanes(initiatives)
    .map((row, index) => ({ row, index, bucket: bucketOf(row) }))
    .sort(
      (a, b) =>
        BUCKET_RANK[a.bucket] - BUCKET_RANK[b.bucket] || a.index - b.index,
    )
    .map(({ row, bucket }) => ({ ...row, bucket }));
}

/** `{ needs_you, slipping, moving, quiet, done }` counts. */
export function bucketCounts(rows) {
  const counts = Object.fromEntries(BUCKETS.map((b) => [b.key, 0]));
  for (const row of rows) counts[row.bucket] += 1;
  return counts;
}

/** "10 initiatives · 126 cards rolled up into them". */
export function rollupSummary(rows, { truncated = false } = {}) {
  const cards = rows.reduce((sum, row) => sum + (row.rollup?.total ?? 0), 0);
  const more = truncated ? "+" : "";
  return `${rows.length}${more} ${rows.length === 1 ? "initiative" : "initiatives"} · ${cards} ${cards === 1 ? "card" : "cards"} rolled up into them`;
}

// ---------------------------------------------------------------------------
// Children

/** A child's tone: the same five the dots and the bar use. */
export function childTone(child) {
  if (child.needsViewer) return "needs";
  if (child.pastPromise || child.state === "blocked") return "slipping";
  if (child.state === "in_progress" || child.state === "review")
    return "moving";
  if (child.state === "done" || child.state === "cancelled") return "done";
  return "not_started";
}
const TONE_ORDER = ["done", "moving", "slipping", "needs", "not_started"];

/** Children drawn one segment each up to this many; above it, by share. */
export const SEGMENT_CAP = 16;

/**
 * The "children by state" bar. One segment per child when core sent every
 * child and there are few enough; otherwise one segment per tone, sized by
 * core's `by_state` counts. Children core did not preview are not started
 * only when the counts say so.
 *
 * @returns {{ tone: string, flex: number }[]}
 */
export function childSegments(rollup) {
  const total = rollup?.total ?? 0;
  if (!total) return [];
  const children = rollup.children ?? [];
  if (children.length === total && total <= SEGMENT_CAP)
    return [...children]
      .map(childTone)
      .sort((a, b) => TONE_ORDER.indexOf(a) - TONE_ORDER.indexOf(b))
      .map((tone) => ({ tone, flex: 1 }));
  const counts = Object.fromEntries(TONE_ORDER.map((tone) => [tone, 0]));
  for (const [phase, n] of Object.entries(rollup.byState ?? {}))
    counts[childTone({ state: phase })] += Math.max(0, Number(n) || 0);
  // Needs-you children sit there whatever their phase.
  for (const child of children) {
    if (!child.needsViewer) continue;
    const from = childTone({ state: child.state });
    if (counts[from] < 1) continue;
    counts[from] -= 1;
    counts.needs += 1;
  }
  return TONE_ORDER.filter((tone) => counts[tone] > 0).map((tone) => ({
    tone,
    flex: counts[tone],
  }));
}

const TONE_WORDS = {
  done: "done",
  moving: "in progress",
  slipping: "slipping",
  needs: "need you",
  not_started: "not started",
};

/**
 * What the bar shows, in words for a screen reader: "1 done, 3 in
 * progress, 2 slipping, 2 need you, 1 not started".
 */
export function segmentsLabel(rollup) {
  const counts = new Map();
  for (const seg of childSegments(rollup))
    counts.set(seg.tone, (counts.get(seg.tone) ?? 0) + seg.flex);
  return TONE_ORDER.filter((tone) => counts.get(tone))
    .map((tone) => `${counts.get(tone)} ${TONE_WORDS[tone]}`)
    .join(", ");
}

/** "22 children in 4 workstreams", "8 children", "1 child", or "". */
export function childrenLine(rollup) {
  const total = rollup?.total ?? 0;
  if (!total) return "";
  const kids = `${total} ${total === 1 ? "child" : "children"}`;
  const streams = rollup.workstreams?.length ?? 0;
  if (streams)
    return `${kids} in ${streams} ${streams === 1 ? "workstream" : "workstreams"}`;
  return kids;
}

/**
 * The rows under an open initiative: its workstreams when it has any, else
 * its children, in core's order. Each says its own phase, whether it is past
 * its promise, and whether it waits on the reader. A workstream's own child
 * counts, promise date and owner are not in the rollup yet.
 */
export const KID_PREVIEW = 8;
export function kidRows(rollup) {
  const streams = rollup?.workstreams ?? [];
  const source = streams.length ? streams : (rollup?.children ?? []);
  const shown = source.slice(0, KID_PREVIEW).map((child) => ({
    ...child,
    tone: childTone(child),
    stateText: phaseLabel(child.state),
    promise: child.pastPromise
      ? { text: "past promise", tone: "warn" }
      : { text: "—", tone: "muted" },
    next: child.needsViewer ? "You" : "—",
    workstream: streams.length > 0,
  }));
  const counted = streams.length ? streams.length : (rollup?.total ?? 0);
  return {
    rows: shown,
    more: Math.max(0, counted - shown.length),
    workstreams: streams.length > 0,
    otherCards: streams.length
      ? Math.max(0, (rollup?.total ?? 0) - streams.length)
      : 0,
  };
}

// ---------------------------------------------------------------------------
// Row cells

/**
 * The line under the title: the stated next step, else what is known.
 * @returns {{ field: string, text: string, tone?: string }[]}
 */
export function nextLine(row, now = Date.now()) {
  const parts = [];
  const what = plainInline(row.promise?.what);
  if (row.state === "done") {
    const age = ageSeconds(row.lastEvidenceAt, now);
    parts.push({
      field:
        age === null ? "initiatives.state" : "initiatives.last_evidence_at",
      text:
        age === null ? "done" : `done ${formatAgo(row.lastEvidenceAt, now)}`,
    });
    return parts;
  }
  if (row.state === "past_promise") {
    if (what || row.promise?.at)
      parts.push({
        field: "initiatives.promise",
        text: what ? `next: ${what}` : `promised ${formatDay(row.promise.at)}`,
      });
    parts.push({
      field: "initiatives.past_promise_for",
      text: `${formatSpan(row.pastPromiseFor)} past`,
      tone: "warn",
    });
  } else if (row.promise) {
    // Word for word what the initiative page says, so the two agree.
    parts.push({
      field: "initiatives.promise",
      text: what
        ? `next: ${what}${row.promise.at ? ` · ${formatDay(row.promise.at)}` : ""}`
        : `next ${formatDay(row.promise.at)}`,
    });
  }
  if (!what && row.state === "no_promise") {
    parts.push({ field: "initiatives.state", text: "no promise" });
    const age = ageSeconds(row.lastEvidenceAt, now);
    parts.push({
      field: "initiatives.last_evidence_at",
      text:
        age === null
          ? "no evidence yet"
          : `last evidence ${formatAgo(row.lastEvidenceAt, now)}`,
    });
  }
  return parts;
}

/** The Promises column. */
export function promisesCell(row) {
  const total = row.rollup?.total ?? 0;
  const past = row.rollup?.pastPromise ?? 0;
  if (past > 0)
    return {
      text: total ? `${past} of ${total} past` : `${past} past`,
      tone: "warn",
      field: "initiatives.rollup.past_promise",
    };
  if (row.state === "past_promise")
    return {
      text: `${formatSpan(row.pastPromiseFor)} past`,
      tone: "warn",
      field: "initiatives.past_promise_for",
    };
  if (row.state === "done")
    return { text: "done", tone: "muted", field: "initiatives.state" };
  if (total > 0)
    return {
      text: "on track",
      tone: "muted",
      field: "initiatives.rollup.past_promise",
    };
  if (row.promise?.at)
    return {
      text: `by ${formatDay(row.promise.at)}`,
      tone: "muted",
      field: "initiatives.promise",
    };
  return { text: "—", tone: "muted", field: "" };
}

/**
 * Who acts next: the reader when something waits on them, else the owner
 * core names (resolved to a name by the caller), else nobody.
 */
export function nextCell(row) {
  if (row.waitingOnYouCount > 0)
    return { kind: "you", field: "initiatives.waiting_on_you_count" };
  if (row.rollup?.needingViewer > 0)
    return { kind: "you", field: "initiatives.rollup.needing_viewer" };
  if (row.owner) return { kind: "owner", ref: row.owner };
  return { kind: "none" };
}

// ---------------------------------------------------------------------------
// The board's needs-you column

/**
 * The refs a set of decisions stands in for: the subject of each, what it
 * blocks, and a grouped ask's members. A child named there shows as the ask.
 */
export function coveredRefs(decisions) {
  return new Set(
    decisions
      .flatMap((decision) => [
        decision.subject?.ref,
        ...(decision.blocks ?? []).map((block) => block.ref),
        ...(decision.batch?.members ?? []).map((member) => member.subjectRef),
      ])
      .filter(Boolean),
  );
}

/**
 * The reader's open decisions by every ref they name (subject, blocks, and
 * a grouped ask's members), built once per brief so a lane looks its refs
 * up instead of scanning every decision per child.
 *
 * @returns {Map<string, object[]>} in Decide order
 */
export function decisionIndex(brief) {
  const index = new Map();
  const add = (ref, decision) => {
    if (!ref) return;
    const list = index.get(ref) ?? [];
    if (!list.includes(decision)) list.push(decision);
    index.set(ref, list);
  };
  for (const decision of sortDecisions(brief?.decisions)) {
    add(decision.subject?.ref, decision);
    for (const block of decision.blocks ?? []) add(block.ref, decision);
    for (const member of decision.batch?.members ?? []) {
      add(member.subjectRef, decision);
      for (const block of member.blocks ?? []) add(block.ref, decision);
    }
  }
  return index;
}

/**
 * What waits on the reader in one lane: open decisions about the lane or
 * one of its children ("ask · you"), then children core marks as waiting on
 * the reader that no decision already covers.
 *
 * @param {Map<string, object[]>} index from {@link decisionIndex}
 * @returns {{ kind: "ask"|"card", key: string, ref: string, title: string, decision?: object }[]}
 */
export function needsYouItems(lane, index) {
  const children = lane.rollup?.children ?? [];
  const asks = new Map();
  for (const ref of [lane.ref, ...children.map((c) => c.ref)])
    for (const decision of index.get(ref) ?? [])
      if (!asks.has(decision.ref)) asks.set(decision.ref, decision);
  const covered = coveredRefs([...asks.values()]);
  return [
    ...[...asks.values()].map((decision) => ({
      kind: "ask",
      key: decision.ref,
      ref: decision.subject?.ref ?? lane.ref,
      title: decision.title,
      decision,
    })),
    ...children
      .filter((child) => child.needsViewer && !covered.has(child.ref))
      .map((child) => ({
        kind: "card",
        key: child.ref,
        ref: child.ref,
        title: child.title,
      })),
  ];
}
