// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, expect, it } from "vitest";
import LiveInitiativeDetails from "../../src/lib/components/reports/LiveInitiativeDetails.svelte";

afterEach(cleanup);

it.each([
  [{ health: "stalled" }, "stale"],
  [{ health: "on_track", plan_health: { state: "done" } }, "done"],
  [{ health: "on_track", plan_state: { health_state: "at_risk" } }, "at_risk"],
])("report detail preserves canonical health %j", (health, state) => {
  const { container } = render(LiveInitiativeDetails, {
    items: [{ ref: "card:ship", title: "Ship", ...health }],
  });
  expect(container.querySelector("[data-health]")?.dataset.health).toBe(state);
});
