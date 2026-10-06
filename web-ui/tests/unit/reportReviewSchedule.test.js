// @vitest-environment jsdom
/**
 * When a dashboard of hand-written panels reads itself again.
 *
 * A report with nothing live is read once when it opens, and that read is what
 * tells core an author is due a reminder. Left alone, a dashboard on a wall
 * display would sit on the `review_due: false` from that first read for as
 * long as the tab stayed open — past the deadline it describes, on exactly the
 * panel a reader most needs warning about.
 *
 * Minutes, not days. Nothing here depends on the size of the gaps, only on
 * their order, and `VisualReport` keeps a 60-second interval to tick its
 * clock: advancing a day of fake time runs that interval 1440 times, each one
 * a state write, a re-render and a yield to the real event loop. That is real
 * wall time, and on a loaded machine it was enough to blow the default
 * timeout. Keep every span here within a few interval ticks of what it is
 * proving, and the whole file costs milliseconds whatever else is running.
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
const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const WRITTEN = new Date(NOW - MINUTE).toISOString();
/** Written a minute ago, reviewed two minutes from now. */
const DUE_AT = NOW + 2 * MINUTE;
/**
 * The retry ladder, as `VisualReport` sets it: eight tries doubling from a
 * minute and capping at an hour. Its span is a production constant, so the two
 * tests that exhaust it have to cross it in fake time.
 *
 * Spelled out rather than rounded, because every wake is armed a second late
 * (`armReviewDeadline` adds 1000ms so the clock is past the target when the
 * callback runs). A constant that was short by those eight seconds would still
 * pass today on the slack around it, and would silently stop reaching the last
 * rung the moment that slack was trimmed.
 */
const LADDER_RUNGS = [1, 2, 4, 8, 16, 32, 60, 60].map((m) => m * MINUTE);
const ARM_SLACK = 1000;
const LADDER_MS =
  LADDER_RUNGS.reduce((total, rung) => total + rung, 0) +
  LADDER_RUNGS.length * ARM_SLACK;
/**
 * Long enough to cross the ladder's first rung.
 *
 * Exactly one rung is not: a wake armed for `t` fires at `t + ARM_SLACK`, so a
 * span of exactly 60 seconds stops a second short of the first retry and
 * proves nothing about it.
 */
const PAST_FIRST_RUNG = LADDER_RUNGS[0] + ARM_SLACK + 4000;

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
      review_by: new Date(DUE_AT).toISOString(),
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

  // Nothing live, so nothing polls. This is past the 60-second clock interval
  // this report does mount and past the 30-second refresh it would have
  // mounted had any panel been live, so either would have read again by now —
  // and still short of the deadline, which is the only thing that should.
  await vi.advanceTimersByTimeAsync(PAST_FIRST_RUNG);
  expect(Date.now()).toBeLessThan(DUE_AT);
  await settled();
  expect(coreClientMock.renderReport).toHaveBeenCalledTimes(1);

  // Past the deadline, core now says due, and the read that asks it is the
  // read that files the author's reminder.
  coreClientMock.renderReport.mockResolvedValue(rendered(true));
  await vi.advanceTimersByTimeAsync(DUE_AT - Date.now() + 2000);
  await settled();
  expect(coreClientMock.renderReport).toHaveBeenCalledTimes(2);
  expect(chip(container)).toBe("due-for-review");

  // And it stops: core has agreed, so there is nothing left to tell it. Past
  // the first two rungs of the retry ladder, which is where a read would land
  // if the deadline were still considered unconfirmed.
  await vi.advanceTimersByTimeAsync(5 * MINUTE);
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

it("asks again when core will not confirm, backing off and then stopping", async () => {
  // A read that failed, or a reader's clock running ahead of core's: the
  // deadline is not spent until core agrees it passed. A fixed minute only
  // covers a minute of clock disagreement, and a machine without NTP can be
  // out by much more, so each wait doubles until it caps.
  const readAt = [];
  coreClientMock.renderReport.mockImplementation(async () => {
    readAt.push(Date.now());
    return rendered(false);
  });
  open();
  await settled();
  await vi.advanceTimersByTimeAsync(DUE_AT - Date.now() + 2000);
  await settled();
  expect(readAt).toHaveLength(2);

  // Past the whole ladder.
  await vi.advanceTimersByTimeAsync(LADDER_MS + 5 * MINUTE);
  await settled();

  // Bounded: the deadline read, eight tries, then silence.
  expect(readAt).toHaveLength(10);
  const gaps = readAt.slice(2).map((at, i) => at - readAt[i + 1]);
  // Each wait is at least a minute, never shrinks, and caps at an hour.
  expect(Math.min(...gaps)).toBeGreaterThanOrEqual(60_000);
  expect(Math.max(...gaps)).toBeLessThanOrEqual(HOUR + 2000);
  expect(gaps).toEqual([...gaps].sort((a, b) => a - b));
  expect(gaps.at(-1)).toBeGreaterThan(gaps[0]);
  // And the ladder reaches hours, not minutes: a clock out by half an hour is
  // still caught.
  expect(readAt.at(-1) - readAt[1]).toBeGreaterThan(2 * HOUR);

  // Longer than the ladder's last rung, so a ninth try would have landed.
  await vi.advanceTimersByTimeAsync(90 * MINUTE);
  await settled();
  expect(readAt).toHaveLength(10);
});

it("keeps waiting for one deadline while another will never be confirmed", async () => {
  // A panel the document declares stale is amber for a reason that is not a
  // deadline, and core will never answer `review_due` for it. Treating it as
  // an unconfirmed deadline made it retry for ever and silenced the panel
  // beside it, whose deadline nobody then read.
  const twoPanels = structuredClone(report);
  twoPanels.panels.push({
    ...report.panels[0],
    id: "declared",
    title: "Declared stale",
    review_by: new Date(NOW + 30 * HOUR).toISOString(),
    freshness: "stale",
  });
  const answer = rendered(false);
  answer.panels.push({
    ...answer.panels[0],
    id: "declared",
    review_by: new Date(NOW + 30 * HOUR).toISOString(),
  });
  coreClientMock.renderReport.mockResolvedValue(answer);
  render(VisualReport, {
    report: twoPanels,
    documentId: "dashboard",
    revisionRef: "document_revision:dashboard-r1",
  });
  await settled();
  expect(coreClientMock.renderReport).toHaveBeenCalledTimes(1);

  // No retry storm before the deadline: this is past the ladder's first rung,
  // where a wrongly pending panel would have read again.
  await vi.advanceTimersByTimeAsync(PAST_FIRST_RUNG);
  expect(Date.now()).toBeLessThan(DUE_AT);
  await settled();
  expect(coreClientMock.renderReport).toHaveBeenCalledTimes(1);

  // ...and the other panel's deadline is still read when it arrives.
  await vi.advanceTimersByTimeAsync(DUE_AT - Date.now() + 2000);
  await settled();
  expect(coreClientMock.renderReport).toHaveBeenCalledTimes(2);
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

it("clears its timer when the report goes away", async () => {
  coreClientMock.renderReport.mockResolvedValue(rendered(false));
  const { unmount } = open();
  await settled();
  expect(vi.getTimerCount()).toBeGreaterThan(0);
  unmount();
  // Cleared, not merely guarded: a timer left armed on every navigation
  // accumulates for as long as the tab lives. Zero, because the component
  // owns both the review timeout and the clock interval beside it.
  expect(vi.getTimerCount()).toBe(0);
  await vi.advanceTimersByTimeAsync(DUE_AT - Date.now() + 5 * MINUTE);
  await settled();
  expect(coreClientMock.renderReport).toHaveBeenCalledTimes(1);
});

it("spends its tries on one deadline without costing the next its read", async () => {
  // A reader clock running fast on a kiosk: the first panel is past its
  // deadline as far as this browser is concerned, and core keeps saying "not
  // yet". The second panel falls due after the whole ladder has been spent,
  // and its author must still be reminded — the budget bounds re-asking about
  // one deadline, not asking about the next.
  const later = structuredClone(report);
  later.panels.push({
    ...report.panels[0],
    id: "later",
    title: "Falls due later",
    // Past the first panel's whole retry ladder.
    review_by: new Date(DUE_AT + LADDER_MS + 10 * MINUTE).toISOString(),
  });
  const answer = rendered(false);
  answer.panels.push({
    ...answer.panels[0],
    id: "later",
    review_by: later.panels[1].review_by,
  });
  const readAt = [];
  coreClientMock.renderReport.mockImplementation(async () => {
    readAt.push(Date.now());
    return answer;
  });
  render(VisualReport, {
    report: later,
    documentId: "dashboard",
    revisionRef: "document_revision:dashboard-r1",
  });
  await settled();

  // The first deadline, then its retry ladder, which runs out after ~3h.
  await vi.advanceTimersByTimeAsync(DUE_AT - Date.now() + 2000);
  await settled();
  await vi.advanceTimersByTimeAsync(LADDER_MS + 5 * MINUTE);
  await settled();
  const spent = readAt.length;
  expect(spent).toBeGreaterThan(5);

  // And the second deadline is still read when it arrives.
  const laterDue = Date.parse(later.panels[1].review_by);
  expect(Date.now()).toBeLessThan(laterDue);
  await vi.advanceTimersByTimeAsync(laterDue - Date.now() + 2000);
  await settled();
  expect(readAt.length).toBeGreaterThan(spent);
  expect(readAt.at(-1)).toBeGreaterThanOrEqual(laterDue);
});
