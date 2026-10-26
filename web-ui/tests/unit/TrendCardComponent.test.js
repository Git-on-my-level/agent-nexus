// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, expect, it } from "vitest";

import TrendCard from "../../src/lib/components/watch/TrendCard.svelte";

afterEach(cleanup);

const answerTime = {
  id: "answer_time",
  title: "Time to answer",
  unit: "seconds",
  direction: "down_good",
  value: 2.3 * 3600,
  previous: 3600,
  truncated: false,
  series: [{ key: "median", label: "Median wait", points: [3600, null, 7200] }],
  rows: [],
};

it("shows the value, a worse-is-red delta, the takeaway and a gapped sparkline", () => {
  const { container } = render(TrendCard, { panel: answerTime, days: 30 });
  const card = container.querySelector('[data-trend="answer_time"]');
  expect(card.dataset.source).toBe("computed:watch_trends.answer_time");
  expect(card.querySelector("[data-trend-value]").textContent).toBe("2.3 h");
  const delta = card.querySelector("[data-trend-delta]");
  expect(delta.textContent).toBe("▲ 2.3×");
  expect(delta.className).toContain("delta--bad");
  expect(card.querySelector("[data-trend-takeaway]").textContent).toBe(
    "Answer time up 2.3× vs the previous 30 days, now 2.3 h.",
  );
  const path = card.querySelector("[data-trend-spark] path").getAttribute("d");
  expect(path.match(/M/g)).toHaveLength(2);
  expect(
    card.querySelector("[data-trend-spark]").getAttribute("aria-label"),
  ).toContain("unknown");
  expect(card.querySelector("[data-trend-partial]")).toBeNull();
});

it("marks a capped read and draws ranked rows instead of a chart", () => {
  const { container } = render(TrendCard, {
    panel: {
      id: "ask_sources",
      title: "Who asks for help most",
      unit: "count",
      direction: "down_good",
      value: null,
      previous: null,
      truncated: true,
      series: [],
      rows: [
        { principal: { id: "a", display: "builder" }, value: 9, previous: 3 },
        {
          principal: { id: "b", display: "reviewer" },
          value: 2,
          previous: null,
        },
      ],
    },
  });
  expect(container.querySelector("[data-trend-partial]")).not.toBeNull();
  expect(container.querySelector("[data-trend-value]")).toBeNull();
  const rows = container.querySelectorAll("[data-trend-rows] li");
  expect(rows).toHaveLength(2);
  expect(rows[0].textContent.replace(/\s+/g, " ")).toContain(
    "builder 9 · was 3",
  );
  expect(rows[1].textContent).not.toContain("was");
});
