// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, expect, it } from "vitest";
import LiveInitiativeDetails from "../../src/lib/components/reports/LiveInitiativeDetails.svelte";

afterEach(cleanup);

it.each([
  [{ plan_health: { state: "past_promise" } }, "past_promise"],
  [{ plan_health: { state: "moving" } }, "moving"],
  [{ plan_health: { state: "no_promise" } }, "no_promise"],
  [{ plan_health: { state: "done" } }, "done"],
])("report detail preserves canonical health %j", (health, state) => {
  const { container } = render(LiveInitiativeDetails, {
    items: [{ ref: "card:ship", title: "Ship", ...health }],
  });
  expect(container.querySelector("[data-health]")?.dataset.health).toBe(state);
});
