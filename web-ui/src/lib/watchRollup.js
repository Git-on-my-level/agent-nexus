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
import { countWord, label as phaseLabel } from "$lib/pm/presentation.js";
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

/** "22 children in 4 workstreams", "8 children", or "no children yet". */
export function childrenLine(rollup) {
  const total = rollup?.total ?? 0;
  if (!total) return "no children yet";
  const kids = `${total} ${total === 1 ? "child" : "children"}`;
  const streams = rollup.workstreams?.length ?? 0;
  if (streams)
    return `${kids} in ${streams} ${streams === 1 ? "workstream" : "workstreams"}`;
  return kids;
}

/**
 * A workstream's children as short codes, in core's order: "T1 T2 P86",
 * "P76 ×5", "fs-p3". Core sends each child's lowercase `short_code`; a
 * letters-and-number code reads in capitals as people write it. A label,
 * not an identity, so repeats fold into one with a count.
 */
export const CODE_PREVIEW = 4;
export function shortCodes(rollup) {
  const counts = new Map();
  for (const child of rollup?.children ?? []) {
    const code = String(child.shortCode ?? "").trim();
    if (code) counts.set(code, (counts.get(code) ?? 0) + 1);
  }
  const codes = [...counts].map(([code, n]) => {
    const shown = /^[a-z]{1,3}\d+$/.test(code) ? code.toUpperCase() : code;
    return n > 1 ? `${shown} ×${n}` : shown;
  });
  return codes.length > CODE_PREVIEW
    ? `${codes.slice(0, CODE_PREVIEW).join(" ")} …`
    : codes.join(" ");
}

/** The order a kid row counts phases in: what is stuck first. */
const STATE_ORDER = [
  "blocked",
  "in_progress",
  "review",
  "ready",
  "backlog",
  "done",
  "cancelled",
];
/** Parts a kid row's state summary shows; the rest are in its title. */
export const STATE_PARTS = 2;

/**
 * A workstream's children by state, from its own rollup: "1 needs you ·
 * 3 blocked", "2 ready · 1 review". A child waiting on the reader counts
 * once, as needing you, not again under its phase.
 *
 * @returns {{ text: string, full: string, tone: string }}
 */
export function stateSummary(rollup) {
  const counts = { ...(rollup?.byState ?? {}) };
  for (const child of rollup?.children ?? [])
    if (child.needsViewer && counts[child.state] > 0) counts[child.state] -= 1;
  const parts = [];
  if (rollup?.needingViewer > 0)
    parts.push(
      `${rollup.needingViewer} ${rollup.needingViewer === 1 ? "needs" : "need"} you`,
    );
  const known = [
    ...STATE_ORDER,
    ...Object.keys(counts).filter((s) => !STATE_ORDER.includes(s)),
  ];
  for (const state of known) {
    const n = Math.max(0, Number(counts[state]) || 0);
    if (n) parts.push(`${n} ${countWord(state)}`);
  }
  return {
    text: parts.slice(0, STATE_PARTS).join(" · "),
    full: parts.join(" · "),
    tone: rollup?.needingViewer > 0 ? "needs" : "muted",
  };
}

/**
 * A kid row's promise: for a workstream, its children's ("3 past", "10h
 * past", "due Oct 25") from its rollup; for a single card, whether it is past
 * its promise, or when it finished.
 */
export function kidPromise(kid, now = Date.now()) {
  const rollup = kid.rollup;
  const field = "initiatives.rollup.workstreams.rollup";
  if (rollup) {
    if (rollup.pastPromise > 1)
      return {
        text: `${rollup.pastPromise} past`,
        tone: "warn",
        field: `${field}.past_promise`,
      };
    const at = Date.parse(rollup.earliestPromiseAt ?? "");
    if (Number.isFinite(at))
      return at <= now
        ? {
            text: `${formatSpan((now - at) / 1000)} past`,
            tone: "warn",
            field: `${field}.earliest_promise_at`,
          }
        : {
            text: `due ${formatDay(rollup.earliestPromiseAt)}`,
            tone: "muted",
            field: `${field}.earliest_promise_at`,
          };
    if (rollup.pastPromise === 1)
      return { text: "1 past", tone: "warn", field: `${field}.past_promise` };
  }
  if (kid.pastPromise)
    return {
      text: "past promise",
      tone: "warn",
      field: "initiatives.rollup.children",
    };
  if (kid.doneAt)
    return {
      text: `done ${formatDay(kid.doneAt)}`,
      tone: "muted",
      field: "initiatives.rollup.children.done_at",
    };
  return { text: "—", tone: "muted", field: "" };
}

/**
 * The rows under an open initiative: its workstreams when it has any, else
 * its children, in core's order. A workstream core expanded says its card
 * count, short codes, children by state, nearest promise and owner from its
 * own rollup; one it did not expand (or a single card) says its own phase
 * and promise.
 */
export const KID_PREVIEW = 8;
export function kidRows(rollup, now = Date.now()) {
  const streams = rollup?.workstreams ?? [];
  const source = streams.length ? streams : (rollup?.children ?? []);
  const shown = source.slice(0, KID_PREVIEW).map((child) => {
    const own = child.rollup;
    const summary = own?.total ? stateSummary(own) : null;
    const tone = childTone(child);
    return {
      ...child,
      tone,
      cards: own?.total ?? 0,
      codes: own ? shortCodes(own) : "",
      stateText: summary?.text || phaseLabel(child.state),
      stateTitle: summary && summary.full !== summary.text ? summary.full : "",
      stateTone: summary ? summary.tone : tone === "needs" ? "needs" : "muted",
      stateField: summary
        ? "initiatives.rollup.workstreams.rollup.by_state"
        : "initiatives.rollup.children",
      promise: kidPromise(child, now),
      next:
        child.needsViewer || own?.needingViewer > 0
          ? { kind: "you" }
          : child.owner
            ? { kind: "owner", ref: child.owner }
            : { kind: "none" },
      workstream: streams.length > 0,
    };
  });
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

/**
 * The initiative's outcome, when it says more than the title: core's
 * `outcome` (its goal, else its summary's first line).
 */
export function outcomeLine(row) {
  const outcome = plainInline(row.outcome);
  const same = (a, b) => a.trim().toLowerCase() === b.trim().toLowerCase();
  return outcome && !same(outcome, String(row.title ?? "")) ? outcome : "";
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

/**
 * "Initiatives: 6 of 15", "6+ of 15" when the count is a lower bound, or
 * "Initiatives: 6" with no target. A target of 0 is real and reads "of 0".
 * Without a budget from core, the rows on screen are the count.
 */
export function budgetIndicator(budget, rows = []) {
  const count = budget ? budget.count : rows.length;
  const plus = budget?.truncated ? "+" : "";
  const target = budget?.target;
  const hasTarget = target != null;
  return {
    text: `Initiatives: ${count}${plus}${hasTarget ? ` of ${target}` : ""}`,
    label: hasTarget
      ? `${plus ? "At least " : ""}${count} initiatives against a target of ${target}`
      : `${plus ? "At least " : ""}${count} initiatives`,
    title: plus
      ? "At least this many. Some initiatives may be outside your view."
      : "",
  };
}

/** "6 initiatives against a target of 5." */
export function overTargetLine(over) {
  if (!over) return "";
  return `${over.truncated ? "At least " : ""}${over.count} initiatives against a target of ${over.target}.`;
}

/**
 * The reader's open ask about the initiative count: the question core
 * recommends, once an agent has asked it. Every brief decision is already
 * open, so among several matches — the same fixed question re-asked — the
 * newest one wins. Core's recommended argv must name the initiative, and
 * only an ask about that card links: a title alone, or a same-titled ask
 * about another subject, is not this link.
 */
export function overTargetAsk(over, decisions = []) {
  const question = over?.ask?.trim().toLowerCase();
  const subjectRef = over?.subjectRef?.trim();
  if (!question || !subjectRef) return null;
  const matches = decisions.filter(
    (d) =>
      d.kind === "ask" &&
      d.title.trim().toLowerCase() === question &&
      d.subject?.ref === subjectRef,
  );
  if (matches.length === 0) return null;
  const askedAt = (d) => Date.parse(d.createdAt ?? "");
  return matches.reduce((newest, d) =>
    (askedAt(d) || 0) > (askedAt(newest) || 0) ? d : newest,
  );
}

/** Verb for one membership change, from its type and before/after. */
export function changeVerb(change) {
  if (change.type === "initiative_promoted") return "Added";
  if (change.type === "initiative_retired") return "Retired";
  if (change.type === "initiative_moved") {
    if (change.after.initiative && !change.before.initiative) return "Moved in";
    if (change.before.initiative && !change.after.initiative)
      return "Moved out";
    return "Moved";
  }
  return "Changed";
}

/**
 * "Added <title> — by <actor>: <reason> · <time>" as parts, so the title can
 * be a link. `reason` is empty when none was supplied.
 */
export function changeLine(change, actorLabel = (id) => id, now = Date.now()) {
  return {
    verb: changeVerb(change),
    title: change.title,
    actor: (change.actorId && actorLabel(change.actorId)) || "unknown",
    reason: change.reason,
    time: formatAgo(change.ts, now),
  };
}

export const CHANGES_FRESH_DAYS = 7;
export const CHANGES_PREVIEW = 5;

/** Open by default only while the newest change is under a week old. */
export function changesStartOpen(changes, now = Date.now()) {
  const newest = Math.max(
    ...(changes?.items ?? []).map((c) => Date.parse(c.ts) || 0),
    0,
  );
  return newest > 0 && now - newest < CHANGES_FRESH_DAYS * 86_400_000;
}
