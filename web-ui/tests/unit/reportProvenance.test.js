import { describe, expect, it } from "vitest";

import {
  DEFAULT_REVIEW_AFTER_MS,
  authoredProvenance,
  liveProvenance,
  panelAuthoredAt,
  panelProvenance,
  nextReviewDeadline,
  panelProvenanceClass,
  reviewReadPending,
  panelReviewDeadline,
  relativeAge,
  withRenderedProvenance,
  withReportDefaults,
} from "../../src/lib/reportProvenance.js";

const NOW = Date.parse("2026-10-06T12:00:00Z");
const ago = (ms) => new Date(NOW - ms).toISOString();
const HOUR = 3_600_000;
const DAY = 24 * HOUR;

const authored = (extra = {}) => ({
  id: "standing",
  type: "explanation",
  title: "Where things stand",
  author: "claude",
  provenance: "reported",
  observed_at: ago(3 * DAY),
  // Not declared stale: that is the author warning a reader, and these
  // fixtures are ordinary notes that happen to be older than a day.
  freshness: "current",
  source_ids: [],
  data: { text: "Release B is in review." },
  ...extra,
});

const live = (extra = {}) => ({
  id: "asks",
  type: "live-asks",
  title: "Needs an answer",
  author: "claude",
  provenance: "reported",
  observed_at: ago(2 * 60_000),
  freshness: "current",
  source_ids: [],
  data: { limit: 5 },
  live: { status: "ok", observed_at: ago(2 * 60_000), data: { items: [] } },
  ...extra,
});

describe("provenance class", () => {
  it("reads a live query panel, a series binding and a static panel", () => {
    expect(panelProvenanceClass(live())).toBe("live");
    expect(
      panelProvenanceClass({ type: "metric", source: { stream: "a" } }),
    ).toBe("live");
    expect(panelProvenanceClass(authored())).toBe("authored");
  });

  it("treats a live type this build has never heard of as live", () => {
    // Core adds `live-cards` and `live-timeline` separately. A UI that
    // rendered them as authored panels would show a hand-written badge over
    // data computed one second ago.
    expect(panelProvenanceClass({ type: "live-cards" })).toBe("live");
    expect(panelProvenanceClass({ type: "live-timeline" })).toBe("live");
  });

  it("prefers the class core declares over the one it would infer", () => {
    expect(
      panelProvenanceClass({ type: "live-asks", provenance_class: "authored" }),
    ).toBe("authored");
    expect(
      panelProvenanceClass({ type: "explanation", provenance_class: "live" }),
    ).toBe("live");
    // Anything that is not one of the two classes is not an instruction.
    expect(
      panelProvenanceClass({ type: "explanation", provenance_class: "guess" }),
    ).toBe("authored");
  });
});

describe("live panels", () => {
  it("states the read age, with the instant on hover", () => {
    const model = panelProvenance(live(), "current", NOW);
    expect(model.state).toBe("live");
    expect(model.label).toBe("Live · updated 2m ago");
    expect(model.title).toContain("Read");
    expect(model.datetime).toBe(ago(2 * 60_000));
    expect(model.dueForReview).toBe(false);
  });

  it("never asks a reader to review a computed panel", () => {
    expect(panelProvenance(live(), "current", NOW).reviewBy).toBeNull();
  });

  it("says a read is still running rather than claiming an age", () => {
    const model = panelProvenance(
      live({ observed_at: null, live: { status: "loading", data: {} } }),
      "unavailable",
      NOW,
    );
    expect(model.state).toBe("live-pending");
    expect(model.age).toBe("");
  });

  it("says a read failed rather than showing the last age it had", () => {
    const model = panelProvenance(
      live({ observed_at: null, live: { status: "unavailable", data: {} } }),
      "unavailable",
      NOW,
    );
    expect(model.state).toBe("live-unavailable");
    expect(model.label).toBe("Live · read failed");
  });

  it("warns in words when a bound series has stopped being published", () => {
    const model = panelProvenance(
      { type: "metric", source: { stream: "a" }, observed_at: ago(3 * DAY) },
      "stale",
      NOW,
    );
    expect(model.state).toBe("live-stale");
    // Not "Live · updated 3d ago": that is word-for-word what a healthy
    // series says, leaving the amber as the only signal.
    expect(model.label).toBe("May be stale · last read 3d ago");
    expect(model.title).toContain("expected interval");
  });

  it("keeps an author's own stale declaration visible", () => {
    // `freshness: "stale"` written into the document is the author warning a
    // reader deliberately. Waiting for a review deadline to agree would leave
    // the report's stale counter and filter pointing at a panel that presents
    // as an ordinary note.
    const model = panelProvenance(
      authored({ authored_at: ago(2 * DAY), freshness: "stale" }),
      "stale",
      NOW,
    );
    expect(model.state).toBe("due-for-review");
    expect(model.label).toBe("May be stale · written 2d ago");
    expect(model.title).toContain("marked this panel stale");
  });

  it("does not call a panel stale just for being a day old", () => {
    // The 24-hour evidence window is not a review promise. `freshness` is
    // `current`; only `getPanelFreshness` demotes it, and that demotion is
    // what drives the filter, not this line.
    const model = panelProvenance(
      authored({ authored_at: ago(2 * DAY), freshness: "current" }),
      "stale",
      NOW,
    );
    expect(model.state).toBe("authored");
  });

  it("calls a series showing its authored snapshot hand-written", () => {
    // `withSeriesObservation` falls back to the document's own numbers with
    // their original as-of time. Labelling that "Live" is the confusion this
    // line exists to remove.
    const model = panelProvenance(
      {
        type: "metric",
        author: "claude",
        source: { stream: "a" },
        fallback: { as_of: ago(9 * DAY), data: { value: "94%" } },
        observed_at: ago(9 * DAY),
        seriesFallback: true,
        seriesObservation: { status: "unavailable" },
      },
      "stale",
      NOW,
    );
    expect(model.class).toBe("authored");
    expect(model.label).toBe("May be stale · written 9d ago");
  });

  it("says a standing-in snapshot is stale however young it is", () => {
    // The binding did not answer. A quiet "Written by claude · 2d ago" would
    // leave the broken series invisible in the header.
    const model = panelProvenance(
      {
        type: "metric",
        author: "claude",
        source: { stream: "a" },
        fallback: { as_of: ago(2 * DAY), data: { value: "94%" } },
        observed_at: ago(2 * DAY),
        seriesFallback: true,
        seriesObservation: { status: "unavailable" },
      },
      "stale",
      NOW,
    );
    expect(model.state).toBe("due-for-review");
    expect(model.title).toContain("could not be read");
  });

  it("does not flip a series to hand-written while its first read runs", () => {
    // `seriesFallback` is set from the moment there is no observation, so a
    // panel with a fallback would read hand-written on every page load and
    // then jump to live.
    const model = panelProvenance(
      {
        type: "metric",
        author: "claude",
        source: { stream: "a" },
        fallback: { as_of: ago(2 * DAY), data: {} },
        observed_at: ago(2 * DAY),
        seriesFallback: true,
        seriesObservation: { status: "loading" },
      },
      "stale",
      NOW,
    );
    expect(model.class).toBe("live");
    expect(model.state).toBe("live-pending");
  });

  it("says a series is still being read rather than that it failed", () => {
    const model = panelProvenance(
      {
        type: "metric",
        source: { stream: "a" },
        observed_at: null,
        seriesFallback: false,
        seriesObservation: { status: "loading" },
      },
      "unavailable",
      NOW,
    );
    expect(model.state).toBe("live-pending");
  });
});

describe("authored panels", () => {
  it("names the principal and the age of the writing", () => {
    const model = panelProvenance(authored(), "stale", NOW);
    expect(model.state).toBe("authored");
    expect(model.label).toBe("Written by claude · 3d ago");
    expect(model.class).toBe("authored");
  });

  it("turns amber once the review date has passed", () => {
    const model = panelProvenance(
      authored({ authored_at: ago(9 * DAY) }),
      "stale",
      NOW,
    );
    expect(model.state).toBe("due-for-review");
    expect(model.label).toBe("May be stale · written 9d ago");
    expect(model.dueForReview).toBe(true);
  });

  it("honours a declared review date, as an instant or a plain date", () => {
    const soon = panelProvenance(
      authored({ authored_at: ago(9 * DAY), review_by: "2026-12-01" }),
      "stale",
      NOW,
    );
    // Nine days old but reviewed in December: the author set the window, and
    // the panel's own age does not override it.
    expect(soon.state).toBe("authored");
    expect(soon.reviewDefaulted).toBe(false);

    const overdue = panelProvenance(
      authored({ authored_at: ago(2 * DAY), review_by: ago(HOUR) }),
      "current",
      NOW,
    );
    expect(overdue.state).toBe("due-for-review");
    expect(overdue.label).toBe("May be stale · written 2d ago");
  });

  it("defaults a missing review date to a week after writing", () => {
    const panel = authored({ authored_at: ago(2 * DAY) });
    expect(panelReviewDeadline(panel)).toEqual({
      at: Date.parse(ago(2 * DAY)) + DEFAULT_REVIEW_AFTER_MS,
      defaulted: true,
      unreadable: false,
    });
    expect(panelProvenance(panel, "stale", NOW).title).toContain(
      "defaulted to 7 days after writing",
    );
  });

  it("does not blame the author for a review date it could not read", () => {
    const panel = authored({
      authored_at: ago(2 * DAY),
      review_by: "next sprint",
    });
    const deadline = panelReviewDeadline(panel);
    expect(deadline.defaulted).toBe(true);
    expect(deadline.unreadable).toBe(true);
    const title = panelProvenance(panel, "stale", NOW).title;
    expect(title).toContain("could not be read");
    expect(title).not.toContain("No review date set");
  });

  it("prefers authored_at over the observation time", () => {
    expect(panelAuthoredAt(authored({ authored_at: ago(DAY) }))).toBe(ago(DAY));
    expect(panelAuthoredAt(authored())).toBe(ago(3 * DAY));
  });

  it("asks for no review at all when nothing dates the writing", () => {
    const panel = authored({ observed_at: null, freshness: "unknown" });
    expect(panelReviewDeadline(panel)).toEqual({
      at: null,
      defaulted: false,
      unreadable: false,
    });
    const model = panelProvenance(panel, "unknown", NOW);
    expect(model.state).toBe("authored");
    expect(model.label).toBe("Written by claude");
  });

  it("says hand-written when no principal is named", () => {
    const model = panelProvenance(
      authored({ author: "", observed_at: null }),
      "unknown",
      NOW,
    );
    expect(model.label).toBe("Hand-written");
  });

  it("skips the review promise for narrative that cannot go stale", () => {
    const model = authoredProvenance(
      { authoredAt: ago(90 * DAY), reviewable: false },
      NOW,
    );
    expect(model.state).toBe("authored");
    expect(model.reviewBy).toBeNull();
    expect(model.label).toBe("Written · 12w ago");
  });
});

describe("the line a renderer draws", () => {
  it("splits again around the author, so only the name can be clamped", () => {
    // A long principal label has to truncate without taking "Written by" or
    // the age with it. Built from how the lead was assembled, never searched
    // for: an author called "W" or "Written" matches inside the words around
    // it, and the clamp would land on a slice of "Written by".
    for (const author of ["claude", "W", "Written", "Written by", "", "  "]) {
      const model = panelProvenance(
        authored({ author, authored_at: ago(3 * DAY), review_by: "30d" }),
        "current",
        NOW,
      );
      expect(model.leadBefore + model.authorLabel + model.leadAfter).toBe(
        model.lead,
      );
      if (author.trim())
        expect([model.leadBefore, model.authorLabel]).toEqual([
          "Written by ",
          author.trim(),
        ]);
    }
    // Every state carries the three parts, including the live ones and the
    // overdue line, which names no principal.
    for (const model of [
      panelProvenance(live(), "current", NOW),
      panelProvenance(authored({ authored_at: ago(9 * DAY) }), "stale", NOW),
      liveProvenance(null, { status: "loading" }, NOW),
      liveProvenance(null, { status: "unavailable" }, NOW),
      authoredProvenance({}, NOW),
    ])
      expect(model.leadBefore + model.authorLabel + model.leadAfter).toBe(
        model.lead,
      );
  });

  it("calls a panel due at the instant its deadline arrives", () => {
    // `review.go` compares with `!now.Before(due)`. A strict comparison here
    // left the chip reading "authored" on exactly the tick the scheduler woke
    // for, which is the one tick it exists to catch.
    const panel = authored({ authored_at: ago(2 * DAY), review_by: "2d" });
    const at = panelReviewDeadline(panel).at;
    expect(panelProvenance(panel, "current", at).state).toBe("due-for-review");
    expect(panelProvenance(panel, "current", at - 1).state).toBe("authored");
  });

  it("schedules nothing for a panel it already shows as due", () => {
    // `nextReviewDeadline` and `panelProvenance` have to agree, or a dashboard
    // arms a timer for a panel that has been amber since it loaded.
    const confirmed = authored({
      authored_at: ago(DAY),
      review_by: "30d",
      review_due: true,
    });
    const declared = authored({
      authored_at: ago(DAY),
      review_by: "30d",
      freshness: "stale",
    });
    for (const panel of [confirmed, declared]) {
      expect(panelProvenance(panel, "stale", NOW).state).toBe("due-for-review");
      expect(nextReviewDeadline([panel], NOW)).toBeNull();
      expect(reviewReadPending([panel], NOW)).toBe(panel === declared);
    }
    // And nothing at all for a deadline past the horizon a tab survives.
    expect(
      nextReviewDeadline(
        [authored({ authored_at: ago(DAY), review_by: "9999-12-31" })],
        NOW,
      ),
    ).toBeNull();
  });

  it("keeps asking until core agrees a deadline passed", () => {
    // Only a read tells core to remind the author, and core decides with its
    // own clock: a reader running fast can read early and be told "not yet".
    const passed = authored({ authored_at: ago(9 * DAY), review_by: "7d" });
    expect(reviewReadPending([passed], NOW)).toBe(true);
    expect(reviewReadPending([{ ...passed, review_due: true }], NOW)).toBe(
      false,
    );
    // A live panel is nobody's deadline, and neither is a future one.
    expect(reviewReadPending([live()], NOW)).toBe(false);
    expect(
      reviewReadPending(
        [authored({ authored_at: ago(DAY), review_by: "30d" })],
        NOW,
      ),
    ).toBe(false);
  });

  it("splits exactly where the <time> element starts", () => {
    for (const model of [
      panelProvenance(live(), "current", NOW),
      panelProvenance(authored(), "stale", NOW),
      panelProvenance(authored({ authored_at: ago(9 * DAY) }), "stale", NOW),
      liveProvenance(null, { status: "loading" }, NOW),
      authoredProvenance({}, NOW),
    ])
      expect(model.lead + model.age).toBe(model.label);
  });

  it("states provenance in words, so colour is never the only signal", () => {
    expect(panelProvenance(live(), "current", NOW).label).toMatch(/^Live/);
    expect(
      panelProvenance(authored({ authored_at: ago(9 * DAY) }), "stale", NOW)
        .label,
    ).toMatch(/^May be stale/);
  });

  it("keeps relative ages relative, however old", () => {
    expect(relativeAge(ago(30_000), NOW)).toBe("just now");
    expect(relativeAge(ago(9 * DAY), NOW)).toBe("9d ago");
    expect(relativeAge(ago(400 * DAY), NOW)).toBe("1y ago");
    expect(relativeAge(new Date(NOW + 2 * DAY).toISOString(), NOW)).toBe(
      "in 2d",
    );
    expect(relativeAge("", NOW)).toBe("");
    expect(relativeAge("not a date", NOW)).toBe("");
  });
});

describe("provenance core resolved", () => {
  it("carries the declared fields onto the panel and nothing else", () => {
    const panel = authored();
    const merged = withRenderedProvenance(panel, {
      id: "standing",
      provenance_class: "authored",
      authored_at: ago(9 * DAY),
      review_by: ago(2 * DAY),
      author: "actor:agent-writer",
      data: { text: "a live observation body" },
      status: "ok",
    });
    expect(merged.review_by).toBe(ago(2 * DAY));
    expect(merged.author).toBe("actor:agent-writer");
    // The document still owns the body.
    expect(merged.data).toEqual(panel.data);
    expect(merged.status).toBeUndefined();
    expect(panelProvenance(merged, "stale", NOW).state).toBe("due-for-review");
  });

  it("leaves the panel alone when core says nothing", () => {
    const panel = authored();
    expect(withRenderedProvenance(panel, undefined)).toBe(panel);
    expect(withRenderedProvenance(panel, { id: "standing" })).toBe(panel);
    expect(withRenderedProvenance(panel, { review_by: null, author: "" })).toBe(
      panel,
    );
  });
});

describe("the deadline the contract defines", () => {
  // `contracts/visualreport/review.go`: a date is midnight UTC, an instant is
  // itself, and a duration is measured from the writing.
  it("reads a review_by written as a duration from the writing", () => {
    for (const reviewBy of ["7d", "168h"]) {
      const panel = authored({
        authored_at: ago(2 * DAY),
        review_by: reviewBy,
      });
      expect(panelReviewDeadline(panel).at).toBe(
        Date.parse(ago(2 * DAY)) + 7 * DAY,
      );
      expect(panelProvenance(panel, "current", NOW).state).toBe("authored");
    }
    // Two days written, three days ago: already overdue.
    const overdue = authored({ authored_at: ago(3 * DAY), review_by: "2d" });
    expect(panelProvenance(overdue, "current", NOW).state).toBe(
      "due-for-review",
    );
  });

  it("reads a plain date as midnight UTC, not as the reader's midnight", () => {
    const panel = authored({
      authored_at: ago(9 * DAY),
      review_by: "2026-10-06",
    });
    expect(panelReviewDeadline(panel).at).toBe(
      Date.parse("2026-10-06T00:00:00Z"),
    );
  });

  it("treats core's verdict as a floor, not a ceiling", () => {
    // `review_due` is computed once, when the report is read. A dashboard left
    // open past its deadline would sit on that `false` until someone reloaded
    // it — which is exactly the panel a reader most needs warning about.
    const stale = authored({
      authored_at: ago(2 * DAY),
      review_by: ago(DAY),
      review_due: false,
    });
    expect(panelProvenance(stale, "current", NOW).state).toBe("due-for-review");

    // And core saying due keeps it due, whatever this reader's clock says.
    const due = panelProvenance(
      authored({ authored_at: ago(DAY), review_by: "30d", review_due: true }),
      "current",
      NOW,
    );
    expect(due.state).toBe("due-for-review");
    expect(due.label).toBe("May be stale · written 1d ago");
  });

  it("names the instant a dashboard should read itself again at", () => {
    const soon = authored({
      id: "soon",
      authored_at: ago(DAY),
      review_by: "2d",
    });
    const later = authored({
      id: "later",
      authored_at: ago(DAY),
      review_by: "30d",
    });
    const passed = authored({
      id: "passed",
      authored_at: ago(9 * DAY),
      review_by: "7d",
    });
    const live = { id: "asks", type: "live-asks", observed_at: ago(0) };
    // The soonest deadline still ahead, and only that one: a second deadline
    // is another read away.
    expect(nextReviewDeadline([later, soon, passed, live], NOW)).toBe(
      Date.parse(ago(DAY)) + 2 * DAY,
    );
    // Nothing ahead means nothing to schedule.
    expect(nextReviewDeadline([passed, live], NOW)).toBeNull();
    expect(nextReviewDeadline([], NOW)).toBeNull();
    expect(nextReviewDeadline(undefined, NOW)).toBeNull();
  });

  it("repeats core's own word on whether a deadline was defaulted", () => {
    const panel = authored({
      authored_at: ago(2 * DAY),
      review_by: new Date(Date.parse(ago(2 * DAY)) + 7 * DAY).toISOString(),
      review_by_defaulted: true,
    });
    expect(panelReviewDeadline(panel).defaulted).toBe(true);
    expect(panelProvenance(panel, "current", NOW).title).toContain(
      "defaulted to 7 days after writing",
    );
  });

  it("dates an undated panel by the report that contains it", () => {
    // Core's rule: `authored_at` falls back to the report's `generated_at`.
    // Reaching for `observed_at` instead would resolve a different deadline
    // from the same document.
    const panel = withReportDefaults(
      { type: "explanation", author: "claude", observed_at: ago(DAY) },
      { generated_at: ago(9 * DAY) },
    );
    expect(panelAuthoredAt(panel)).toBe(ago(9 * DAY));
    expect(panelProvenance(panel, "current", NOW).state).toBe("due-for-review");
  });

  it("carries every field core resolved, and only those", () => {
    const merged = withRenderedProvenance(authored(), {
      id: "standing",
      type: "explanation",
      status: "ok",
      provenance_class: "authored",
      authored_at: ago(9 * DAY),
      review_by: ago(2 * DAY),
      review_by_defaulted: true,
      review_due: true,
      data: { text: "the rendered copy of the body" },
    });
    expect(merged.review_due).toBe(true);
    expect(merged.review_by_defaulted).toBe(true);
    // The document still owns the body.
    expect(merged.data.text).toBe("Release B is in review.");
    expect(panelProvenance(merged, "current", NOW).state).toBe(
      "due-for-review",
    );
  });

  it("calls the new live types live, as core computes them", () => {
    // `provenance_class` is computed from the type and source binding, never
    // claimed by the author.
    for (const type of ["live-cards", "live-timeline"])
      expect(panelProvenanceClass({ type })).toBe("live");
    expect(
      panelProvenanceClass({ type: "live-timeline", provenance_class: "live" }),
    ).toBe("live");
  });
});
