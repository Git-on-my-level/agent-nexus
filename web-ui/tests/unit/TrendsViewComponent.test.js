// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/svelte";
import { tick } from "svelte";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import TrendsView from "../../src/lib/components/watch/TrendsView.svelte";

const client = vi.hoisted(() => ({ getWatchTrends: vi.fn() }));
vi.mock("$lib/coreClient", () => ({
  coreClient: client,
  createWorkspaceReadClient: () => client,
}));

const settled = async () => {
  for (let pass = 0; pass < 6; pass += 1) {
    await Promise.resolve();
    await tick();
  }
};

const group = (name, panels) => ({
  schema: "watch_trends.v1",
  group: name,
  range_days: 30,
  bucket: "day",
  buckets: [],
  panels,
});
const panel = (id, title) => ({
  id,
  title,
  unit: "count",
  direction: "up_good",
  value: 3,
  previous: 1,
  truncated: false,
  series: [{ key: "n", label: "N", points: [1, 2, 3] }],
  rows: [],
});

beforeEach(() => client.getWatchTrends.mockReset());
afterEach(cleanup);

it("loads each group on its own and keeps the others when one fails", async () => {
  client.getWatchTrends.mockImplementation(async ({ group: name, range }) => {
    expect(range).toBe("7d");
    if (name === "loops") throw new Error("upstream detail");
    return group(name, [panel(`${name}_panel`, `${name} trend`)]);
  });
  const { container } = render(TrendsView, { days: 7 });
  await settled();
  expect(client.getWatchTrends).toHaveBeenCalledTimes(3);
  expect(
    container.querySelector('[data-trend="decisions_panel"]'),
  ).not.toBeNull();
  expect(
    container.querySelector('[data-trend="initiatives_panel"]'),
  ).not.toBeNull();
  const loops = container.querySelector('[data-trend-group="loops"]');
  expect(loops.querySelector("[data-trend-error]")).not.toBeNull();
  expect(container.textContent).not.toContain("upstream detail");

  client.getWatchTrends.mockResolvedValue(
    group("loops", [panel("loops_panel", "Loop trend")]),
  );
  screen.getByRole("button", { name: "Retry" }).click();
  await settled();
  expect(container.querySelector('[data-trend="loops_panel"]')).not.toBeNull();
});
