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
 * Two sources, one answer. A stored panel carries `authored_at` and
 * `review_by`; the rendered report resolves both — and the class itself —
 * against core's clock, returning `provenance_class`, an absolute `review_by`,
 * `review_by_defaulted` and `review_due`. Core's answer wins where it exists,
 * because a deadline must not depend on the reader's clock or timezone. The
 * document answers when the report has not been rendered yet, and the panel's
 * own shape answers when neither does.
 */

import { formatAge, ageTitle } from "./ageBadge.js";
import { formatAbsoluteDateTime } from "./formatDate.js";
import { isLivePanel } from "./liveReports.js";
import { reviewDeadlineMillis } from "./visualReports.js";

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

/** What the rendered report resolves, and this module reads back. */
export const PROVENANCE_PANEL_FIELDS = Object.freeze([
  "provenance_class",
  "authored_at",
  "review_by",
  "review_by_defaulted",
  "review_due",
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
  // `report_generated_at` is the report's own `generated_at`, attached by
  // `withReportDefaults`. Core dates an undated panel by exactly that, and a
  // UI that reached for `observed_at` instead would resolve a different
  // deadline from the same document.
  return (
    panel?.authored_at ??
    panel?.report_generated_at ??
    panel?.observed_at ??
    null
  );
}

/**
 * The report's own `generated_at`, carried onto each panel.
 *
 * An authored panel that does not date itself is dated by the report that
 * contains it — core's rule — and a panel on its own cannot see that.
 */
export function withReportDefaults(panel, report) {
  const generatedAt = report?.generated_at;
  if (!panel || !generatedAt || panel.report_generated_at === generatedAt)
    return panel;
  return { ...panel, report_generated_at: generatedAt };
}

/**
 * The review deadline for an authored panel, and whether it was defaulted.
 *
 * `{ at: number|null, defaulted: boolean }`. A panel with no writing time has
 * no deadline at all: a guess anchored to nothing would read as a fact.
 */
export function panelReviewDeadline(panel) {
  return reviewDeadline(
    panel?.review_by,
    panelAuthoredAt(panel),
    panel?.review_by_defaulted,
  );
}

/**
 * `{ at, defaulted, unreadable }`. `unreadable` separates "nobody set a review
 * date" from "a review date was set and could not be read": both fall back to
 * the default window, but only the first is the author's omission, and the
 * tooltip must not accuse them of the wrong one.
 */
function reviewDeadline(reviewBy, authoredAt, defaulted) {
  const authored = instant(authoredAt);
  // `review_by` is a calendar date, a zoned instant, or a duration measured
  // from the writing — `7d`, `168h`. Resolving it is the schema's job, so the
  // UI reads the same deadline the validator and core resolve.
  const declared = reviewDeadlineMillis(reviewBy, authored);
  if (declared !== null)
    return { at: declared, defaulted: defaulted === true, unreadable: false };
  const unreadable =
    reviewBy !== null && reviewBy !== undefined && reviewBy !== "";
  if (authored === null) return { at: null, defaulted: false, unreadable };
  return {
    at: authored + DEFAULT_REVIEW_AFTER_MS,
    defaulted: true,
    unreadable,
  };
}

/**
 * The same friendly instant every other surface shows: "2 min ago",
 * "yesterday", then a short local date. The exact instant stays on the hover.
 */
export function relativeAge(value, now = Date.now()) {
  return formatAge(value, now);
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
      leadBefore: "Live · reading workspace…",
      authorLabel: "",
      leadAfter: "",
      title: "Computed from workspace data at read time.",
    };
  if (!instant(observedAt) || (state.status && state.status !== "ok"))
    return {
      ...base,
      state: "live-unavailable",
      label: "Live · read failed",
      lead: "Live · read failed",
      leadBefore: "Live · read failed",
      authorLabel: "",
      leadAfter: "",
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
    leadBefore: lead,
    authorLabel: "",
    leadAfter: "",
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
 * `stale: true` says so without waiting for a deadline: an author who declared
 * the panel stale, or a snapshot standing in for a live read that failed.
 * `note` is one more sentence for the tooltip, saying which.
 *
 * `reviewDue` is core's verdict on the deadline as of the last read. It can
 * only make a panel due, never keep one from becoming due as time passes.
 *
 * @param {{ author?: string, authoredAt?: string|null, reviewBy?: string|null,
 *   reviewable?: boolean, stale?: boolean, note?: string, reviewDue?: boolean,
 *   reviewDefaulted?: boolean }} written
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
      : reviewDeadline(written.reviewBy, authoredAt, written.reviewDefaulted);
  // Core's `review_due` is a floor, not the whole answer. It is computed once,
  // when the report is read, so a dashboard left open past its deadline would
  // sit on `false` for as long as nobody reloaded it — which is exactly the
  // panel a reader most needs warning about. The clock can only move the
  // verdict toward caution: core saying due keeps it due, and this reader's
  // clock running fast shows the warning early rather than hiding it.
  const dueForReview =
    written.stale === true ||
    written.reviewDue === true ||
    // Inclusive, as `review.go` is: `!now.Before(due)`. At the instant the
    // deadline arrives the panel is due, and a strict comparison left the
    // chip reading "authored" on exactly the tick the scheduler woke for.
    (review.at !== null && now >= review.at);
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
  // The lead split around the author, so a renderer can let a long principal
  // label truncate without taking "Written by" or the age with it:
  // `leadBefore + authorLabel + leadAfter === lead`, always.
  //
  // Built from how the lead was assembled, never searched for: an author
  // called "W" or "Written" matches inside the words around it, and the clamp
  // would have landed on a slice of "Written by" instead of on the name.
  const named = Boolean(author) && !dueForReview;
  const leadBefore = named ? "Written by " : lead;
  const authorLabel = named ? author : "";
  const leadAfter = named ? lead.slice(leadBefore.length + author.length) : "";
  return {
    class: "authored",
    state: dueForReview ? "due-for-review" : "authored",
    label: `${lead}${age}`,
    lead,
    leadBefore,
    authorLabel,
    leadAfter,
    age,
    title: [authoredTitle(author, authoredAt, review, now), written.note]
      .filter(Boolean)
      .join(" · "),
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
  //
  // Not while the first read is still in flight, though: `seriesFallback` is
  // set from the moment there is no observation, and flipping a panel from
  // hand-written to live on every page load would teach a reader to ignore
  // the line. And always stale: the snapshot is standing in for a binding
  // that did not answer, which a quiet "Written by claude · 2d ago" hides.
  if (panel?.seriesFallback && panel?.seriesObservation?.status !== "loading")
    return authoredProvenance(
      {
        author: panel?.author,
        authoredAt: panel?.fallback?.as_of ?? panel?.observed_at ?? null,
        reviewBy: panel?.review_by,
        stale: true,
        note: "The live series could not be read; this is the document's own snapshot.",
      },
      now,
    );
  if (panelProvenanceClass(panel) === "live") {
    // A series that answered `stale` answered: it has a read time and rows.
    // `withSeriesObservation` drops that observation time when there is no
    // fallback to date, so the observation itself is the honest source.
    const status =
      panel?.live?.status ??
      panel?.seriesObservation?.status ??
      (panel?.observed_at ? "ok" : "loading");
    return liveProvenance(
      panel?.live?.observed_at ??
        panel?.observed_at ??
        panel?.seriesObservation?.observed_at ??
        null,
      {
        // A live panel the report has not asked about yet has no observation
        // and no failure either: it is still being read.
        status: status === "stale" ? "ok" : status,
        stale: freshness === "stale" || status === "stale",
      },
      now,
    );
  }
  return authoredProvenance(
    {
      author: panel?.author,
      authoredAt: panelAuthoredAt(panel),
      reviewBy: panel?.review_by,
      // An author who writes `freshness: "stale"` into the document is warning
      // the reader deliberately. That is a stronger statement than a review
      // date nobody has reached yet, and dropping it would leave the report's
      // own stale counter and filter pointing at panels that present as
      // ordinary notes.
      // Core resolves the deadline against its own clock and says whether it
      // has passed. That answer wins: a reader whose clock is off by a day
      // must not see a different verdict from the one core acted on when it
      // reminded the author.
      reviewDue: panel?.review_due,
      reviewDefaulted: panel?.review_by_defaulted,
      // An author who writes `freshness: "stale"` into the document is warning
      // the reader deliberately. That is a stronger statement than a review
      // date nobody has reached yet, and dropping it would leave the report's
      // own stale counter and filter pointing at panels that present as
      // ordinary notes.
      stale: panel?.freshness === "stale",
      note:
        panel?.freshness === "stale"
          ? "The author marked this panel stale."
          : "",
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
    // Inclusive, as the state is: at the instant the deadline arrives the chip
    // reads "May be stale", and its tooltip must not still say "Review due".
    parts.push(
      now >= review.at ? `Review was due ${due}` : `Review due ${due}`,
    );
    if (review.defaulted)
      parts.push(
        review.unreadable
          ? "The review date could not be read; defaulted to 7 days after writing"
          : "No review date set; defaulted to 7 days after writing",
      );
  }
  return parts.filter(Boolean).join(" · ");
}

/** Authored panels only, with whatever deadline they resolve to. */
function* authoredDeadlines(panels) {
  for (const panel of panels ?? []) {
    if (panelProvenanceClass(panel) !== "authored") continue;
    yield { panel, at: panelReviewDeadline(panel).at };
  }
}

/**
 * The soonest review deadline still ahead of these panels, or `null`.
 *
 * A report is read once when it opens, and that read is what tells core an
 * author is due a reminder. With nothing live to poll, a dashboard left open
 * would never reach its own deadline: this is the instant worth reading again
 * at. Panels whose deadline has already passed are skipped — they need a read
 * now, not a timer (see `reviewReadPending`).
 *
 * Deliberately unbounded: a dashboard with a thirty-day review window is the
 * case this exists for, and a timer that re-arms costs nothing. Clamping the
 * wait is the caller's job.
 */
export function nextReviewDeadline(panels, now = Date.now()) {
  let soonest = null;
  for (const { at } of authoredDeadlines(panels)) {
    if (at === null || at <= now) continue;
    if (soonest === null || at < soonest) soonest = at;
  }
  return soonest;
}

/**
 * Whether a panel has passed its deadline without core having said so.
 *
 * The reader's clock reaching the deadline turns the line amber on its own,
 * but only a read tells core to remind the author — and core decides with its
 * own clock, so a reader running fast can read early and be told "not yet".
 * While that is true the report is worth reading again: this is what says so,
 * and what stops saying so once core agrees.
 *
 * Deliberately narrower than "shows as due". A panel whose document declares
 * `freshness: "stale"`, or a series standing in with its snapshot, is amber
 * for reasons that have nothing to do with a deadline — and core will never
 * answer `review_due` for one, so treating it as pending made every report
 * containing one retry for ever and starved its other panels.
 */
export function reviewReadPending(panels, now = Date.now()) {
  for (const { panel, at } of authoredDeadlines(panels)) {
    if (panel?.review_due === true) continue;
    if (at !== null && at <= now) return true;
  }
  return false;
}
