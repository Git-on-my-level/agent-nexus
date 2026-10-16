// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, expect, it } from "vitest";
import LiveInitiativeDetails from "../../src/lib/components/reports/LiveInitiativeDetails.svelte";

afterEach(cleanup);

it("a dashboard initiative header shows the status line only", () => {
  const { container } = render(LiveInitiativeDetails, {
    items: [
      {
        ref: "card:ship",
        title: "Ship",
        priority: "p1",
        plan_health: { state: "moving" },
        possibly_moot: { flagged: true, reason: "Linked PR merged" },
        progress: { done: 2, total: 9 },
        source_status: "In UAT",
        freshness: { status: "stale", observed_at: "2026-10-01T00:00:00Z" },
        attention: { count: 1, truncated: true },
      },
    ],
  });
  const badge = container.querySelector('[data-work-summary="badge"]');
  expect(badge).not.toBeNull();
  expect(badge.querySelector("[data-health]")?.dataset.health).toBe("moving");
  expect(badge.querySelector("[data-moot]")).not.toBeNull();
  // Owner, due, asks, source word and freshness belong to the initiative page.
  expect(badge.querySelector("[data-summary-foot]")).toBeNull();
  expect(badge.textContent).not.toContain("In UAT");
  expect(container.textContent).not.toContain("Waiting status unknown");
  expect(container.textContent).toContain("2/9");
});
