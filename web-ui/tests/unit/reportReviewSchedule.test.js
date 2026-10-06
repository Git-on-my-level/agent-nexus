// @vitest-environment jsdom
/**
 * When a dashboard of hand-written panels reads itself again.
 *
 * A report with nothing live is read once when it opens, and that read is what
 * tells core an author is due a reminder. Left alone, a dashboard on a wall
 * display would sit on the `review_due: false` from that first read for as
 * long as the tab stayed open — past the deadline it describes, on exactly the
 * panel a reader most needs warning about.
 */
import { cleanup, render } from "@testing-library/svelte";
import { tick } from "svelte";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import VisualReport from "../../src/lib/components/reports/VisualReport.svelte";

const coreClientMock = vi.hoisted(() => ({
  resolveRefs: vi.fn(),
  renderReport: vi.fn(),
}));
vi.mock("$lib/coreClient", () => ({ coreClient: coreClientMock }));

const NOW = Date.parse("2026-10-06T12:00:00Z");
const DAY = 86_400_000;
const WRITTEN = new Date(NOW - DAY).toISOString();
/** Written a day ago, reviewed in two: due a day from now. */
const DUE_AT = Date.parse(WRITTEN) + 2 * DAY;

const report = {
  schema_version: 1,
  title: "Dashboard",
  summary: "",
  generated_at: WRITTEN,
  projects: [{ id: "delivery", title: "Delivery", outcome: "", summary: "" }],
  sources: [],
  panels: [
    {
      id: "standing",
      project_id: "delivery",
      type: "explanation",
      title: "Where things stand",
      author: "claude",
      provenance: "reported",
      observed_at: WRITTEN,
      authored_at: WRITTEN,
      review_by: "2d",
      freshness: "current",
      source_ids: [],
      data: { text: "Release B is in review." },
    },
  ],
};

/** What core answers for that panel, with the verdict it has reached. */
const rendered = (reviewDue) => ({
  document_ref: "document:dashboard",
  revision_ref: "document_revision:dashboard-r1",
  observed_at: new Date(Date.now()).toISOString(),
  panels: [
    {
      id: "standing",
      type: "explanation",
      status: "ok",
      observed_at: WRITTEN,
      truncated: false,
      data: report.panels[0].data,
      provenance_class: "authored",
      author: "claude",
      authored_at: WRITTEN,
      review_by: new Date(DUE_AT).toISOString(),
      review_by_defaulted: false,
      review_due: reviewDue,
    },
  ],
});

const settled = async () => {
  for (let pass = 0; pass < 4; pass += 1) {
    await Promise.resolve();
    await tick();
  }
};
const open = () =>
  render(VisualReport, {
    report,
    documentId: "dashboard",
    revisionRef: "document_revision:dashboard-r1",
  });
const chip = (container) =>
  container.querySelector("[data-anx-provenance]")?.dataset.anxProvenance;

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(NOW);
  coreClientMock.resolveRefs.mockReset();
  coreClientMock.renderReport.mockReset();
  coreClientMock.resolveRefs.mockResolvedValue({ items: [] });
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

it("reads itself again when its soonest deadline passes", async () => {
  coreClientMock.renderReport.mockResolvedValue(rendered(false));
  const { container } = open();
  await settled();
  expect(coreClientMock.renderReport).toHaveBeenCalledTimes(1);
  expect(chip(container)).toBe("authored");

  // Nothing live, so nothing polls: an hour later it has still read once.
  await vi.advanceTimersByTimeAsync(3_600_000);
  await settled();
  expect(coreClientMock.renderReport).toHaveBeenCalledTimes(1);

  // Past the deadline, core now says due, and the read that asks it is the
  // read that files the author's reminder.
  coreClientMock.renderReport.mockResolvedValue(rendered(true));
  await vi.advanceTimersByTimeAsync(DUE_AT - Date.now() + 2000);
  await settled();
  expect(coreClientMock.renderReport).toHaveBeenCalledTimes(2);
  expect(chip(container)).toBe("due-for-review");

  // And it stops: core has agreed, so there is nothing left to tell it.
  await vi.advanceTimersByTimeAsync(10 * 60_000);
  await settled();
  expect(coreClientMock.renderReport).toHaveBeenCalledTimes(2);
});

it("turns amber on the clock even if the read never lands", async () => {
  // The deadline is the reader's to see; only the reminder needs core.
  coreClientMock.renderReport.mockResolvedValue(rendered(false));
  const { container } = open();
  await settled();
  coreClientMock.renderReport.mockRejectedValue(new Error("core is down"));
  await vi.advanceTimersByTimeAsync(DUE_AT - Date.now() + 2000);
  await settled();
  expect(chip(container)).toBe("due-for-review");
});

it("asks again when core will not confirm, and gives up rather than polling", async () => {
  // A read that failed, or a reader's clock running ahead of core's: the
  // deadline is not spent until core agrees it passed.
  coreClientMock.renderReport.mockResolvedValue(rendered(false));
  open();
  await settled();
  await vi.advanceTimersByTimeAsync(DUE_AT - Date.now() + 2000);
  await settled();
  const atDeadline = coreClientMock.renderReport.mock.calls.length;
  expect(atDeadline).toBe(2);

  await vi.advanceTimersByTimeAsync(5 * 61_000);
  await settled();
  const afterRetries = coreClientMock.renderReport.mock.calls.length;
  expect(afterRetries).toBeGreaterThan(atDeadline);
  expect(afterRetries).toBeLessThanOrEqual(atDeadline + 5);

  // Bounded: a document that has gone for good stops being asked about.
  await vi.advanceTimersByTimeAsync(60 * 60_000);
  await settled();
  expect(coreClientMock.renderReport).toHaveBeenCalledTimes(afterRetries);
});

it("keeps what core resolved when a later read fails", async () => {
  // Core corrects the author to the principal that wrote the revision. A
  // failed request is not news about a hand-written panel: starting over from
  // the document would quietly contradict what core already said.
  const corrected = rendered(false);
  corrected.panels[0].author = "actor:agent-writer";
  coreClientMock.renderReport.mockResolvedValue(corrected);
  const { container } = open();
  await settled();
  expect(container.querySelector(".provenance-author")?.textContent).toBe(
    "actor:agent-writer",
  );

  coreClientMock.renderReport.mockRejectedValue(new Error("core is down"));
  await vi.advanceTimersByTimeAsync(DUE_AT - Date.now() + 2000);
  await settled();
  expect(container.querySelector("[data-anx-provenance]").title).toContain(
    "actor:agent-writer",
  );
});

it("stops its timer when the report goes away", async () => {
  coreClientMock.renderReport.mockResolvedValue(rendered(false));
  const { unmount } = open();
  await settled();
  unmount();
  await vi.advanceTimersByTimeAsync(DUE_AT - Date.now() + 10 * 60_000);
  await settled();
  expect(coreClientMock.renderReport).toHaveBeenCalledTimes(1);
});
