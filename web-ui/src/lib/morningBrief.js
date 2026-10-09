/**
 * The morning brief, as five readable sections.
 *
 * Core computes the brief (`GET /overview` → `brief`): the ranking, the
 * reasons, the grouped digest and the counts all arrive decided, from rows
 * that request had already loaded and already filtered. This module does not
 * recompute any of it. It names things, orders the sections, and — the part
 * that matters for a page someone reads in thirty seconds — decides what an
 * empty section says, because a blank box tells a reader nothing about
 * whether the answer is "nothing" or "we did not look".
 *
 * Status vocabulary comes from `workSummary.js`, so a brief row, a card and a
 * list row cannot disagree about what `no_plan` is called or how urgent it is.
 */

import { formatTime } from "$lib/time/format.js";
import { statusChip, summaryFromStatus } from "$lib/workSummary.js";

const asText = (value) => String(value ?? "").trim();

const count = (value) => {
  const n = Number(value);
  return Number.isFinite(n) && n > 0 ? Math.floor(n) : 0;
};

const rows = (value) => (Array.isArray(value) ? value : []);

/**
 * Panel order inside the band. Decisions and At risk come first because they
 * are the two questions that can change what a reader does next.
 *
 * `initiatives` is computed but is deliberately not a panel: the Overview shows
 * the initiative cards themselves directly under the band, and a one-line
 * restatement of the same seven initiatives above them was the same answer
 * twice. The section's counts still feed that card section's header.
 */
export const BRIEF_SECTIONS = Object.freeze([
  "decisions",
  "risk",
  "changes",
  "machine",
]);

/**
 * The clock time a baseline reads as. A digest's "since" is the sentence
 * "nothing new since 08:12", so it wants a time of day, not a date.
 */
export function briefClock(iso, locale = undefined) {
  return formatTime(iso, { style: "clock", locale });
}

/**
 * A brief row's status, as the one summary renderer draws it.
 *
 * Core ranked these rows from the same computation that fills a card's
 * `work_summary`, and sends the state, its reason and its progress rather
 * than the whole card. `summaryFromStatus` turns that into the same model, so
 * a brief row and the card below it cannot say different things.
 */
export function briefSummary(item) {
  return summaryFromStatus({
    state: asText(item?.state),
    reason: asText(item?.reason),
    since: asText(item?.since),
    progress: item?.progress ?? null,
  });
}

function link(href, hrefFor) {
  const path = asText(href);
  return path ? hrefFor(path) : "";
}

function decisionsSection(brief, hrefFor) {
  const source = brief?.decisions ?? {};
  if (asText(source.status) === "unavailable") {
    return {
      key: "decisions",
      title: "Decisions",
      status: "unavailable",
      message: asText(source.message) || "Decisions could not be loaded.",
      rows: [],
    };
  }
  const total = count(source.count);
  return {
    key: "decisions",
    title: "Decisions",
    status: "ok",
    total,
    truncated: source.truncated === true,
    empty: total === 0,
    emptyLine: "Nothing is waiting on your decision.",
    rows: rows(source.items).map((item) => ({
      id: asText(item.id),
      title: asText(item.title) || "Needs you",
      href: link(item.href, hrefFor),
      reason: asText(item.reason),
      source: asText(item.source),
      blocks: count(item.signals?.blocks),
    })),
    more: count(source.more),
    moreHref: link(source.href || "/inbox?mailbox=needs-you", hrefFor),
  };
}

function changesSection(brief, hrefFor) {
  const source = brief?.since_last_look ?? {};
  const since = asText(source.since);
  const clock = briefClock(since);
  const groups = rows(source.groups).map((group) => ({
    key: asText(group.key),
    label: asText(group.label),
    count: count(group.count),
    more: count(group.more),
    rows: rows(group.items).map((item, index) => ({
      ref: asText(item.ref),
      stepId: asText(item.step_id),
      title: asText(item.title),
      href: link(item.href, hrefFor),
      at: asText(item.at),
      /*
       * A row's identity, for keyed rendering. Two completed steps of one
       * initiative share its ref, so keying on ref alone is a duplicate key
       * and the list throws rather than rendering. step_id separates them;
       * the index is the last resort for a row with neither, which must
       * still render rather than take the page down.
       */
      key: [
        asText(group.key),
        asText(item.ref),
        asText(item.step_id),
        index,
      ].join("#"),
    })),
  }));
  const total = count(source.total);
  /*
   * Three different sentences for three different facts. "Nothing changed"
   * and "this is your first look" used to render identically, which is how a
   * digest that had never had a baseline to compare against read as a calm
   * report that nothing had happened.
   */
  let emptyLine = "Nothing new.";
  if (source.first_visit === true) {
    emptyLine =
      "First look. From now on this shows what changed since your last visit.";
  } else if (clock) {
    emptyLine = `Nothing new since ${clock}.`;
  }
  return {
    key: "changes",
    title: "Since you last looked",
    status: "ok",
    since,
    clock,
    firstVisit: source.first_visit === true,
    total,
    truncated: source.truncated === true,
    empty: total === 0,
    emptyLine,
    groups,
    rows: [],
  };
}

function riskSection(brief, hrefFor) {
  const source = brief?.at_risk ?? {};
  if (asText(source.status) === "unavailable") {
    return {
      key: "risk",
      title: "At risk",
      status: "unavailable",
      message: asText(source.message) || "Risk could not be loaded.",
      rows: [],
    };
  }
  const total = count(source.count);
  return {
    key: "risk",
    title: "At risk",
    status: "ok",
    total,
    truncated: source.truncated === true,
    empty: total === 0,
    emptyLine: "Nothing is off track.",
    rows: rows(source.items).map((item) => ({
      ref: asText(item.ref),
      title: asText(item.title),
      href: link(item.href, hrefFor),
      reason: asText(item.reason),
      since: asText(item.since),
      summary: briefSummary(item),
      progress: progressOf(item.progress),
    })),
    more: count(source.more),
  };
}

function progressOf(value) {
  const done = Number(value?.done);
  const total = Number(value?.total);
  if (!Number.isFinite(done) || !Number.isFinite(total) || total <= 0) {
    return null;
  }
  const bounded = Math.max(0, Math.min(done, total));
  return {
    done: bounded,
    total,
    percent: Math.round((bounded / total) * 100),
  };
}

function machineSection(brief, hrefFor) {
  const source = brief?.machine ?? {};
  const hours = count(brief?.throughput_hours) || 24;
  const rosterDown = asText(source.roster_status) === "unavailable";
  const throughputDown = asText(source.throughput_status) === "unavailable";
  /*
   * Four numbers, and only the last one is allowed to be loud. "Stuck" means
   * an agent is holding work and has gone quiet; offline agents are the normal
   * state of most agents most of the time and badging them amber made a
   * healthy workspace look broken (#317).
   */
  const stats = [];
  if (!rosterDown) {
    stats.push(
      { key: "working", label: "Working", value: count(source.working) },
      {
        key: "waiting",
        label: "Waiting on you",
        value: count(source.waiting),
        tone: count(source.waiting) ? "warn" : "",
      },
    );
  }
  if (!throughputDown) {
    stats.push({
      key: "finished",
      label: `Finished in ${hours}h`,
      value: count(source.finished_24h),
    });
  }
  if (!rosterDown) {
    stats.push({
      key: "stuck",
      label: "Stuck",
      value: count(source.stuck),
      tone: count(source.stuck) ? "warn" : "",
    });
  }
  return {
    key: "machine",
    title: "Machine",
    status: "ok",
    rosterDown,
    throughputDown,
    message: rosterDown
      ? asText(source.message) || "Agent presence could not be loaded."
      : throughputDown
        ? "Throughput could not be computed."
        : "",
    stats,
    agentsFinished: count(source.agents_finished_24h),
    truncated: source.truncated === true || source.roster_truncated === true,
    empty: false,
    rows: rows(source.stuck_items).map((item) => ({
      ref: asText(item.ref),
      title: asText(item.title),
      href: link(item.href, hrefFor),
      reason: asText(item.reason),
    })),
    moreHref: link(source.href || "/agents", hrefFor),
  };
}

function initiativesSection(brief, hrefFor) {
  const source = brief?.initiatives ?? {};
  const byState =
    source.by_state && typeof source.by_state === "object"
      ? source.by_state
      : {};
  /*
   * State counts, worst first, each one a fact rather than a badge: "3 no
   * plan" is the sentence a reader needs when seven initiatives all report
   * no_plan, and the one the old default on_track hid.
   */
  const chips = Object.keys(byState)
    .map((state) => statusChip(state, count(byState[state])))
    .filter((chip) => chip.count > 0)
    .sort((a, b) => a.rank - b.rank || a.state.localeCompare(b.state));
  const total = count(source.count);
  return {
    key: "initiatives",
    title: "Initiatives",
    status: "ok",
    total,
    truncated: source.truncated === true,
    empty: total === 0,
    emptyLine: "No initiatives yet.",
    chips,
    rows: rows(source.items).map((item) => ({
      ref: asText(item.ref),
      title: asText(item.title),
      href: link(item.href, hrefFor),
      reason: asText(item.reason),
      nextStep: asText(item.next_step),
      summary: briefSummary(item),
      progress: progressOf(item.progress),
    })),
    more: count(source.more),
    moreHref: link(source.href || "/tasks", hrefFor),
  };
}

/**
 * The brief as the band renders it.
 *
 * @param {object|null|undefined} brief the `brief` object from `GET /overview`
 * @param {{ hrefFor?: (path: string) => string }} [options]
 * @returns {{status: string, generatedAt: string, order: string[], sections: object}|null}
 *   `null` when the server did not send a brief, which is how an older core
 *   is told apart from a brief with nothing in it.
 */
export function morningBriefModel(brief, { hrefFor = (path) => path } = {}) {
  if (!brief || typeof brief !== "object") return null;
  const sections = {
    decisions: decisionsSection(brief, hrefFor),
    changes: changesSection(brief, hrefFor),
    risk: riskSection(brief, hrefFor),
    machine: machineSection(brief, hrefFor),
    initiatives: initiativesSection(brief, hrefFor),
  };
  return {
    status: asText(brief.status) || "ok",
    generatedAt: asText(brief.generated_at),
    order: [...BRIEF_SECTIONS],
    sections,
    /** True when every section has something worth reading. Used for nothing
     *  but the band's own aria summary, and deliberately not used to hide the
     *  band: a quiet morning is a result, not an absence. */
    quiet: [...BRIEF_SECTIONS, "initiatives"].every(
      (key) => sections[key].empty || key === "machine",
    ),
  };
}
