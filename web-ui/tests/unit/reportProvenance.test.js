import { describe, expect, it } from "vitest";

import {
  DEFAULT_REVIEW_AFTER_MS,
  authoredProvenance,
  liveProvenance,
  panelAuthoredAt,
  panelProvenance,
  panelProvenanceClass,
  panelReviewDeadline,
  relativeAge,
  withRenderedProvenance,
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
  freshness: "stale",
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

  it("warns when a bound series has stopped being published", () => {
    const model = panelProvenance(
      { type: "metric", source: { stream: "a" }, observed_at: ago(3 * DAY) },
      "stale",
      NOW,
    );
    expect(model.state).toBe("live-stale");
    expect(model.label).toBe("Live · updated 3d ago");
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
    });
    expect(panelProvenance(panel, "stale", NOW).title).toContain(
      "defaulted to 7 days after writing",
    );
  });

  it("prefers authored_at over the observation time", () => {
    expect(panelAuthoredAt(authored({ authored_at: ago(DAY) }))).toBe(ago(DAY));
    expect(panelAuthoredAt(authored())).toBe(ago(3 * DAY));
  });

  it("asks for no review at all when nothing dates the writing", () => {
    const panel = authored({ observed_at: null, freshness: "unknown" });
    expect(panelReviewDeadline(panel)).toEqual({ at: null, defaulted: false });
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
