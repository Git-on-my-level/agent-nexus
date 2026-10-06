/**
 * Which surfaces are computed and which were typed by hand — and whether the
 * hand-written ones are still worth trusting.
 *
 * A reader cannot tell a live panel from a snapshot an agent wrote nine days
 * ago: both render as a bordered box with a title. This module is the one
 * place that decides, so the Overview's embedded dashboard, the document
 * report view and the initiative page all say the same thing in the same
 * words.
 *
 * Two classes, never more:
 *
 * - `live`: computed at read time (a live query panel, a series binding, a
 *   plan core recomputes on every read). Its age is the age of the read.
 * - `authored`: static content. Its age is the age of the writing, and it
 *   carries a review deadline after which the reader should distrust it.
 *
 * Both classes are stated in text. Colour is a second signal, never the only
 * one: "May be stale" says so in words whether or not the amber renders.
 *
 * Core is adding `provenance_class`, `authored_at` and `review_by` to report
 * panels. This module prefers those fields and derives the same answer from
 * the panel's shape without them, so the UI side does not have to land second
 * — but the report schema still gates them: `parseVisualReport` rejects a
 * panel field it does not know, so a document can only carry them once
 * `visualReports.js` and `contracts/visualreport/report.go` accept them
 * together. Until core lands that, these fields reach the UI only through the
 * rendered report response, never through a stored document.
 */

import { formatAge, ageTitle } from "./ageBadge.js";
import { formatAbsoluteDateTime } from "./formatDate.js";
import { isLivePanel } from "./liveReports.js";

export const PROVENANCE_CLASSES = Object.freeze(["live", "authored"]);

/**
 * How long an authored panel is trusted when it does not say.
 *
 * Mirrors the core default for a panel written before `review_by` existed:
 * seven days after it was written. Deliberately not the 24-hour evidence
 * staleness window the freshness filter uses — that window asks "how old is
 * this observation", and this one asks "has the author promised to look at it
 * again by now".
 */
export const DEFAULT_REVIEW_AFTER_MS = 7 * 24 * 60 * 60 * 1000;

/** Panel fields this module reads beyond the ones the report schema requires. */
export const PROVENANCE_PANEL_FIELDS = Object.freeze([
  "provenance_class",
  "authored_at",
  "review_by",
]);

/**
 * `live` or `authored`.
 *
 * A declared class wins, because core resolves it against the stored panel and
 * knows more than its shape does. Anything else is read off the panel: a live
 * query or a series binding is computed, and everything else was written.
 */
export function panelProvenanceClass(panel) {
  if (PROVENANCE_CLASSES.includes(panel?.provenance_class))
    return panel.provenance_class;
  return isLivePanel(panel) ? "live" : "authored";
}

/** An instant in millis, or `null`. Accepts `YYYY-MM-DD` as well as RFC3339. */
function instant(value) {
  if (value === null || value === undefined || value === "") return null;
  const at = new Date(value).getTime();
  return Number.isFinite(at) ? at : null;
}

/**
 * When an authored panel was written.
 *
 * `authored_at` when core supplies it, and the observation time otherwise —
 * for a static panel those are the same claim, and the observation time is the
 * only one older reports carry.
 */
export function panelAuthoredAt(panel) {
  return panel?.authored_at ?? panel?.observed_at ?? null;
}

/**
 * The review deadline for an authored panel, and whether it was defaulted.
 *
 * `{ at: number|null, defaulted: boolean }`. A panel with no writing time has
 * no deadline at all: a guess anchored to nothing would read as a fact.
 */
export function panelReviewDeadline(panel) {
  return reviewDeadline(panel?.review_by, panelAuthoredAt(panel));
}

/**
 * `{ at, defaulted, unreadable }`. `unreadable` separates "nobody set a review
 * date" from "a review date was set and could not be read": both fall back to
 * the default window, but only the first is the author's omission, and the
 * tooltip must not accuse them of the wrong one.
 */
function reviewDeadline(reviewBy, authoredAt) {
  const declared = instant(reviewBy);
  if (declared !== null)
    return { at: declared, defaulted: false, unreadable: false };
  const unreadable =
    reviewBy !== null && reviewBy !== undefined && reviewBy !== "";
  const authored = instant(authoredAt);
  if (authored === null) return { at: null, defaulted: false, unreadable };
  return {
    at: authored + DEFAULT_REVIEW_AFTER_MS,
    defaulted: true,
    unreadable,
  };
}

/**
 * `3d ago`, `in 2d`, `just now` — the badge vocabulary as a phrase.
 *
 * `formatAge` already decides the units, and reusing it keeps "2m" on a card
 * and "2m ago" in a panel header the same reading. Unlike `formatTimestamp`
 * this never switches to a calendar date: a header says "written 9d ago"
 * because the elapsed time is the point, and the absolute instant is one hover
 * away.
 */
export function relativeAge(value, now = Date.now()) {
  const age = formatAge(value, now);
  if (!age) return "";
  if (age === "now") return "just now";
  return age.startsWith("in ") ? age : `${age} ago`;
}

/**
 * The provenance line for something computed at read time.
 *
 * `label` is the whole line. `lead` and `age` are the same line split where a
 * `<time>` element starts, so a renderer carries the machine-readable instant
 * without re-deriving the words: `label === lead + age`, always.
 *
 * @param {string|null} observedAt when the read happened
 * @param {{ status?: string, stale?: boolean }} [state] `status` is the live
 *   read's own status (`loading`, `ok`, anything else is a failure); `stale`
 *   is for a bound series whose publisher has missed its interval.
 * @param {number} [now]
 */
export function liveProvenance(observedAt, state = {}, now = Date.now()) {
  const base = {
    class: "live",
    author: "",
    reviewBy: null,
    reviewDefaulted: false,
    dueForReview: false,
    age: "",
    datetime: "",
  };
  if (state.status === "loading")
    return {
      ...base,
      state: "live-pending",
      label: "Live · reading workspace…",
      lead: "Live · reading workspace…",
      title: "Computed from workspace data at read time.",
    };
  if (!instant(observedAt) || (state.status && state.status !== "ok"))
    return {
      ...base,
      state: "live-unavailable",
      label: "Live · read failed",
      lead: "Live · read failed",
      title:
        "Computed from workspace data at read time. The last read did not complete.",
    };
  const age = relativeAge(observedAt, now);
  // A bound series whose publisher has missed its interval reads the same as a
  // healthy one if only the amber changes, so it says so in the same words an
  // overdue authored panel uses.
  const lead = state.stale ? "May be stale · last read " : "Live · updated ";
  return {
    ...base,
    state: state.stale ? "live-stale" : "live",
    label: `${lead}${age}`,
    lead,
    age,
    datetime: observedAt,
    title: `${ageTitle(observedAt, "read", now)} · ${
      state.stale
        ? "This series has not been published within its expected interval."
        : "Computed from workspace data at read time."
    }`,
  };
}

/**
 * The provenance line for something a principal wrote.
 *
 * `reviewable: false` is for narrative that carries no review promise — the
 * body of a card, a decision note. Marking it hand-written is useful; turning
 * it amber a week later is noise, because nobody ever undertook to rewrite it.
 *
 * @param {{ author?: string, authoredAt?: string|null, reviewBy?: string|null,
 *   reviewable?: boolean }} written
 * @param {number} [now]
 */
export function authoredProvenance(written = {}, now = Date.now()) {
  const author =
    typeof written.author === "string" ? written.author.trim() : "";
  const authoredAt = written.authoredAt ?? null;
  const age = relativeAge(authoredAt, now);
  const review =
    written.reviewable === false
      ? { at: null, defaulted: false, unreadable: false }
      : reviewDeadline(written.reviewBy, authoredAt);
  const dueForReview = review.at !== null && now > review.at;
  const written_by = author ? `Written by ${author}` : "Written";
  const lead = dueForReview
    ? age
      ? "May be stale · written "
      : "May be stale"
    : age
      ? `${written_by} · `
      : author
        ? written_by
        : "Hand-written";
  return {
    class: "authored",
    state: dueForReview ? "due-for-review" : "authored",
    label: `${lead}${age}`,
    lead,
    age,
    title: authoredTitle(author, authoredAt, review, now),
    datetime: authoredAt ?? "",
    author,
    reviewBy: review.at,
    reviewDefaulted: review.defaulted,
    dueForReview,
  };
}

/**
 * The provenance line for one report panel.
 *
 * @param {object} panel a report panel, after any live observation is merged in
 * @param {string} [freshness] the panel's computed freshness, when the caller
 *   has it: it is what distinguishes a live panel that read cleanly from one
 *   whose read failed, and a bound series that has gone quiet.
 * @param {number} [now]
 */
export function panelProvenance(panel, freshness = "", now = Date.now()) {
  // A bound series falling back to its snapshot is showing the document's own
  // hand-written numbers with their original as-of time. Calling that "Live"
  // is the exact confusion this line exists to remove, so it is authored —
  // dated by the snapshot, not by the read that failed.
  if (panel?.seriesFallback)
    return authoredProvenance(
      {
        author: panel?.author,
        authoredAt: panel?.fallback?.as_of ?? panel?.observed_at ?? null,
        reviewBy: panel?.review_by,
      },
      now,
    );
  if (panelProvenanceClass(panel) === "live")
    return liveProvenance(
      panel?.live?.observed_at ?? panel?.observed_at ?? null,
      {
        // A live panel the report has not asked about yet has no observation
        // and no failure either: it is still being read.
        status:
          panel?.live?.status ??
          panel?.seriesObservation?.status ??
          (panel?.observed_at ? "ok" : "loading"),
        stale: freshness === "stale",
      },
      now,
    );
  return authoredProvenance(
    {
      author: panel?.author,
      authoredAt: panelAuthoredAt(panel),
      reviewBy: panel?.review_by,
    },
    now,
  );
}

/**
 * Carry the provenance core resolved onto the panel.
 *
 * Core resolves a panel's class and review deadline against the stored report
 * and returns them with the rendered report, so the UI does not have to infer
 * either. Only the provenance fields are taken, and only when they are there:
 * everything else about a rendered panel is the live observation, and an
 * authored panel's body must keep coming from the document.
 *
 * @param {object} panel the authored panel
 * @param {object} [rendered] the matching entry from the rendered report
 */
export function withRenderedProvenance(panel, rendered) {
  if (!panel || !rendered) return panel;
  const carried = {};
  for (const field of PROVENANCE_PANEL_FIELDS)
    if (rendered[field] !== undefined && rendered[field] !== null)
      carried[field] = rendered[field];
  // `author` is already a report field; core may correct it to the principal
  // that actually wrote the revision.
  if (typeof rendered.author === "string" && rendered.author)
    carried.author = rendered.author;
  return Object.keys(carried).length ? { ...panel, ...carried } : panel;
}

function authoredTitle(author, authoredAt, review, now) {
  const parts = [];
  const written = ageTitle(authoredAt, "written", now);
  parts.push(
    author
      ? written
        ? `${written} by ${author}`
        : `Written by ${author}`
      : written || "Hand-written, not computed",
  );
  if (review.at !== null) {
    const due = formatAbsoluteDateTime(new Date(review.at).toISOString());
    parts.push(now > review.at ? `Review was due ${due}` : `Review due ${due}`);
    if (review.defaulted)
      parts.push(
        review.unreadable
          ? "The review date could not be read; defaulted to 7 days after writing"
          : "No review date set; defaulted to 7 days after writing",
      );
  }
  return parts.filter(Boolean).join(" · ");
}
