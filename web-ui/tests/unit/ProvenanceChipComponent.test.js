// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/svelte";
import { afterEach, expect, it } from "vitest";

import ProvenanceChip from "../../src/lib/components/ProvenanceChip.svelte";
import VisualReportPanel from "../../src/lib/components/reports/VisualReportPanel.svelte";

afterEach(cleanup);

const NOW = Date.parse("2026-10-06T12:00:00Z");
const ago = (days) => new Date(NOW - days * 86_400_000).toISOString();

const panel = (extra = {}) => ({
  id: "standing",
  project_id: "anx",
  type: "explanation",
  title: "Where things stand",
  author: "claude",
  provenance: "reported",
  observed_at: ago(3),
  // Not declared stale: that is the author warning a reader, and these
  // fixtures are ordinary notes that happen to be older than a day.
  freshness: "current",
  source_ids: [],
  data: { text: "Release B is in review." },
  ...extra,
});

it("renders the age as a <time> the machine can read", () => {
  const { container } = render(ProvenanceChip, {
    panel: panel(),
    freshness: "stale",
    now: NOW,
  });
  const chip = container.querySelector("[data-anx-provenance]");
  expect(chip.textContent.replace(/\s+/g, " ").trim()).toBe(
    "Written by claude · 3d ago",
  );
  expect(chip.dataset.anxProvenance).toBe("authored");
  expect(chip.dataset.anxProvenanceClass).toBe("authored");
  const time = chip.querySelector("time");
  expect(time.getAttribute("datetime")).toBe(ago(3));
  // The instant is the accessible name, so "3d ago" is never the only answer
  // a screen reader can give.
  expect(time.getAttribute("aria-label")).toContain("Written");
});

it("says a panel past its review date may be stale, in words", () => {
  const { container } = render(ProvenanceChip, {
    panel: panel({ authored_at: ago(9) }),
    freshness: "stale",
    now: NOW,
  });
  const chip = container.querySelector("[data-anx-provenance]");
  expect(chip.dataset.anxProvenance).toBe("due-for-review");
  expect(chip.textContent).toContain("May be stale");
  expect(chip.textContent).toContain("9d ago");
});

it("marks a live panel in the header, in the compact embed too", () => {
  const live = {
    ...panel({
      id: "asks",
      type: "live-asks",
      data: { limit: 5 },
      observed_at: new Date(NOW - 120_000).toISOString(),
      freshness: "current",
    }),
    live: {
      status: "ok",
      observed_at: new Date(NOW - 120_000).toISOString(),
      data: { items: [] },
    },
  };
  const { container } = render(VisualReportPanel, {
    compact: true,
    panel: live,
    freshness: "current",
    now: NOW,
    oninspect: () => {},
  });
  const section = container.querySelector("[data-report-panel='asks']");
  expect(section.dataset.provenanceClass).toBe("live");
  expect(section.dataset.provenanceState).toBe("live");
  const chip = section.querySelector(
    ".report-panel-header [data-anx-provenance]",
  );
  expect(chip.textContent.replace(/\s+/g, " ").trim()).toBe(
    "Live · updated 2m ago",
  );
  // The read time is no longer repeated at the foot of the panel body.
  expect(section.textContent).not.toContain("Live as of");
});

it("marks a hand-written panel in the compact embed, which has no footer", () => {
  const { container } = render(VisualReportPanel, {
    compact: true,
    panel: panel({ authored_at: ago(9) }),
    freshness: "stale",
    now: NOW,
    oninspect: () => {},
  });
  const section = container.querySelector("[data-report-panel='standing']");
  expect(section.dataset.provenanceState).toBe("due-for-review");
  expect(section.querySelector(".report-panel-footer")).toBeNull();
  expect(screen.getByText(/May be stale/, { exact: false })).toBeTruthy();
  // "Stale snapshot" said less, in a second chip beside this one.
  expect(section.textContent).not.toContain("Stale snapshot");
});

it("does not badge a live panel Unavailable while its read is in flight", () => {
  // `withLiveObservation` reports `freshness: "unavailable"` for any status
  // but `ok`, loading included. The freshness badge used to be suppressed for
  // live panels; it has to stay suppressed, or every live panel header reads
  // "Live · reading workspace… | Unavailable" until the read lands.
  for (const status of ["loading", "unavailable"]) {
    const { container, unmount } = render(VisualReportPanel, {
      compact: true,
      panel: {
        ...panel({ id: "asks", type: "live-asks", data: { limit: 5 } }),
        observed_at: null,
        freshness: "unavailable",
        live: { status, data: {} },
      },
      freshness: "unavailable",
      now: NOW,
      oninspect: () => {},
    });
    const section = container.querySelector("[data-report-panel='asks']");
    expect(section.textContent).not.toContain("Unavailable");
    expect(
      section.querySelector("[data-anx-provenance]").dataset.anxProvenance,
    ).toBe(status === "loading" ? "live-pending" : "live-unavailable");
    unmount();
  }
});

it("still badges a stored panel whose evidence the report calls unavailable", () => {
  const { container } = render(VisualReportPanel, {
    compact: true,
    panel: panel({ freshness: "unavailable", observed_at: null }),
    freshness: "unavailable",
    now: NOW,
    oninspect: () => {},
  });
  const section = container.querySelector("[data-report-panel='standing']");
  expect(section.textContent).toContain("Unavailable");
});

it("separates when a note was written from how old its evidence is", () => {
  // A note typed this morning can cite an observation from September. The
  // provenance line answers the first question and the freshness badge the
  // second; neither can answer the other.
  const { container } = render(VisualReportPanel, {
    compact: true,
    panel: panel({ authored_at: new Date(NOW - 3_600_000).toISOString() }),
    freshness: "stale",
    now: NOW,
    oninspect: () => {},
  });
  const section = container.querySelector("[data-report-panel='standing']");
  expect(section.querySelector("[data-anx-provenance]").textContent).toContain(
    "Written by claude · 1h ago",
  );
  expect(section.querySelector(".report-state").textContent).toContain(
    "Stale evidence",
  );
});

it("does not warn twice on a panel that is already overdue", () => {
  const { container } = render(VisualReportPanel, {
    compact: true,
    panel: panel({ authored_at: ago(9) }),
    freshness: "stale",
    now: NOW,
    oninspect: () => {},
  });
  const section = container.querySelector("[data-report-panel='standing']");
  expect(section.textContent).toContain("May be stale");
  expect(section.querySelector(".report-state")).toBeNull();
});

it("keeps showing a release timeline that has gone quiet", () => {
  // An irregular stream — releases, deploys — is stale between events by
  // definition: core computes staleness from the adapter's expected interval.
  // Core still returns the events it has, and blanking the panel exactly when
  // the last release matters would be the opposite of the point.
  const { container } = render(VisualReportPanel, {
    compact: true,
    panel: {
      ...panel({ id: "releases", type: "live-timeline", title: "Releases" }),
      source: { series: "releases", range: "30d" },
      // As `withSeriesObservation` leaves it for a stale read with no
      // fallback to date: empty data, and no observation time on the panel.
      data: {},
      observed_at: null,
      freshness: "stale",
      seriesFallback: false,
      seriesObservation: {
        status: "stale",
        observed_at: ago(4),
        data: {
          items: [
            { at: ago(4), label: "channel=stable", value: "v0.12.12" },
            { at: ago(9), label: "channel=stable", value: "v0.12.11" },
          ],
        },
      },
    },
    freshness: "stale",
    now: NOW,
    oninspect: () => {},
  });
  const section = container.querySelector("[data-report-panel='releases']");
  expect(section.textContent).toContain("v0.12.12");
  expect(section.textContent).toContain("v0.12.11");
  expect(section.dataset.provenanceClass).toBe("live");
  // And the header agrees with the rows beside it. A series that answered
  // `stale` answered; saying "read failed" over two visible releases is the
  // contradiction this line exists to remove.
  expect(section.querySelector("[data-anx-provenance]").textContent).toContain(
    "May be stale · last read 4d ago",
  );
  expect(section.dataset.provenanceState).toBe("live-stale");
});

it("clamps a long principal label without losing the words around it", () => {
  // `author` is author-supplied text up to 200 characters, and a report
  // generator will put a sentence there. "Written by" and the age are what
  // make the line readable; the name is the part that can be cut.
  const long =
    "A long principal label, the kind a report generator produces when it names a pipeline rather than a person";
  const { container } = render(ProvenanceChip, {
    panel: panel({ author: long, authored_at: ago(3) }),
    freshness: "current",
    now: NOW,
  });
  const chip = container.querySelector("[data-anx-provenance]");
  const name = chip.querySelector(".provenance-author");
  expect(name.textContent).toBe(long);
  // Clamped by CSS, so the full label is still in the DOM and in a tooltip.
  expect(name.getAttribute("data-tooltip")).toBe(long);
  expect(chip.textContent).toContain("Written by");
  expect(chip.textContent).toContain("3d ago");
  expect(chip.querySelector("time").textContent).toBe("3d ago");
});

it("stops trusting a stale review_due as the clock passes the deadline", () => {
  // Core computes `review_due` once, when the report is read. A dashboard left
  // open would otherwise sit on `false` past its own deadline.
  const { container } = render(VisualReportPanel, {
    compact: true,
    panel: panel({
      authored_at: ago(9),
      review_by: ago(2),
      review_due: false,
    }),
    freshness: "current",
    now: NOW,
    oninspect: () => {},
  });
  const section = container.querySelector("[data-report-panel='standing']");
  expect(section.dataset.provenanceState).toBe("due-for-review");
  expect(section.textContent).toContain("May be stale");
});
