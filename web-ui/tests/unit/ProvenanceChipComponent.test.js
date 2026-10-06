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
  freshness: "stale",
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
