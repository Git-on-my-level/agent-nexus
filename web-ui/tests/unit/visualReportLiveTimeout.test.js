// @vitest-environment jsdom
/**
 * A live panel whose render read never answers.
 *
 * The read had no deadline, and the 30-second poll skips while a read is in
 * flight, so one stalled request held every live panel on "Reading
 * workspace…" for as long as the page stayed open. The read now gives up at
 * its deadline, says so, and the next poll asks again.
 */
import { cleanup, render } from "@testing-library/svelte";
import { tick } from "svelte";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import VisualReport from "../../src/lib/components/reports/VisualReport.svelte";
import { LIVE_READ_TIMEOUT_MS } from "../../src/lib/liveReports.js";

const coreClientMock = vi.hoisted(() => ({
  resolveRefs: vi.fn(),
  renderReport: vi.fn(),
}));
vi.mock("$lib/coreClient", () => ({
  coreClient: coreClientMock,
  createWorkspaceReadClient: () => coreClientMock,
}));

const report = {
  schema_version: 1,
  title: "Workspace",
  summary: "",
  generated_at: "2026-10-03T07:00:00Z",
  projects: [{ id: "ws", title: "Workspace", outcome: "", summary: "" }],
  panels: [
    {
      id: "asks",
      project_id: "ws",
      type: "live-asks",
      title: "Open asks",
      author: "ANX",
      provenance: "reported",
      observed_at: null,
      freshness: "unknown",
      source_ids: [],
      data: {},
    },
  ],
  sources: [],
};

const settled = async () => {
  for (let pass = 0; pass < 4; pass += 1) {
    await Promise.resolve();
    await tick();
  }
};

beforeEach(() => {
  vi.useFakeTimers();
  coreClientMock.resolveRefs.mockReset();
  coreClientMock.renderReport.mockReset();
  coreClientMock.resolveRefs.mockResolvedValue({ items: [] });
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

it("stops reading the workspace at the deadline and tries again", async () => {
  coreClientMock.renderReport.mockReturnValue(new Promise(() => {}));
  const { container } = render(VisualReport, {
    report,
    documentId: "dashboard",
    compact: true,
  });
  await settled();
  expect(container.textContent).toContain("Reading workspace…");

  await vi.advanceTimersByTimeAsync(LIVE_READ_TIMEOUT_MS);
  await settled();
  expect(container.textContent).not.toContain("Reading workspace…");
  expect(container.textContent).toContain(
    "The workspace is taking too long to answer. Trying again shortly.",
  );
  expect(coreClientMock.renderReport).toHaveBeenCalledTimes(1);

  // The stalled read no longer blocks the poll.
  await vi.advanceTimersByTimeAsync(30_000 - LIVE_READ_TIMEOUT_MS + 1000);
  await settled();
  expect(coreClientMock.renderReport).toHaveBeenCalledTimes(2);
});
