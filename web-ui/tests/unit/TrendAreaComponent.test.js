// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, expect, it } from "vitest";

import TrendArea from "../../src/lib/components/watch/TrendArea.svelte";
import TrendPeople from "../../src/lib/components/watch/TrendPeople.svelte";

afterEach(cleanup);

const loops = {
  schema: "watch_trends.v1",
  group: "loops",
  range_days: 7,
  bucket: "day",
  buckets: [
    "2026-10-31T00:00:00Z",
    "2026-11-01T00:00:00Z",
    "2026-11-02T00:00:00Z",
    "2026-11-03T00:00:00Z",
    "2026-11-04T00:00:00Z",
    "2026-11-05T00:00:00Z",
    "2026-11-06T00:00:00Z",
  ],
  panels: [
    {
      id: "loop_runs",
      title: "Loop runs",
      unit: "count",
      direction: "up_good",
      value: null,
      previous: null,
      failed: null,
      truncated: true,
      series: [
        { key: "runs", label: "Runs", points: [null, null, null, 4, 5, 6, 2] },
        {
          key: "failed",
          label: "Failed to run",
          points: [null, null, null, 0, 1, 0, 0],
        },
      ],
      rows: [],
    },
    {
      id: "loop_health",
      title: "Loops reporting on time",
      unit: "percent",
      direction: "up_good",
      value: 82,
      previous: 95,
      truncated: false,
      series: [
        {
          key: "on_time",
          label: "Reporting on time",
          points: [95, 90, 90, 88, 85, 82, 82],
        },
      ],
      rows: [],
    },
  ],
};

it("draws the main chart, shades unread days and totals what was read", () => {
  const { container } = render(TrendArea, {
    group: { key: "loops", label: "Loops" },
    state: { status: "ok", data: loops },
    days: 7,
  });
  const area = container.querySelector('[data-trend-group="loops"]');
  expect(area.querySelector("[data-trend-verdict]").textContent).toBe(
    "82% on time",
  );
  expect(area.querySelector("[data-trend-takeaway]").textContent).toContain(
    "17 runs in the last 4 days",
  );
  expect(area.querySelector("[data-trend-unread]").textContent).toBe(
    "Before Nov 3: not read",
  );
  const runs = area.querySelector('[data-trend="loop_runs"]');
  expect(runs.dataset.source).toBe("computed:watch_trends.loop_runs");
  expect(runs.querySelector("svg").getAttribute("aria-label")).toContain(
    "unknown",
  );
  const health = area.querySelector('[data-trend="loop_health"]');
  expect(health.querySelector("[data-trend-value]").textContent).toBe("82%");
  expect(health.querySelector("[data-trend-delta]").textContent).toBe(
    "▼ 13 pts",
  );
  const failed = area.querySelector('[data-trend="loop_failed"]');
  expect(failed.querySelector("[data-trend-value]").textContent).toBe("1");
  expect(failed.querySelector("[data-trend-partial]").textContent).toBe(
    "last 4 days",
  );
});

it("offers a retry when the read fails", () => {
  const { container, getByRole } = render(TrendArea, {
    group: { key: "loops", label: "Loops" },
    state: { status: "failed" },
  });
  expect(container.querySelector("[data-trend-error]")).not.toBeNull();
  expect(getByRole("button", { name: "Retry" })).toBeTruthy();
});

it("ranks people and says when the period was too busy to rank", () => {
  const { container, rerender } = render(TrendPeople, {
    panel: {
      id: "ask_sources",
      title: "Who asks for help most",
      truncated: false,
      rows: [
        { principal: { id: "a", display: "builder" }, value: 9, previous: 3 },
        { principal: { id: "b", display: "reviewer" }, value: 2, previous: 0 },
      ],
    },
  });
  const items = container.querySelectorAll("[data-trend-rows] li");
  expect(items[0].textContent).toContain("builder");
  expect(items[0].textContent).toContain("up from 3");
  expect(items[1].textContent).toContain("new this period");
  rerender({
    panel: {
      id: "ask_sources",
      title: "Who asks for help most",
      truncated: true,
      rows: [],
    },
  });
  expect(container.textContent).toContain("Too busy to rank");
});
