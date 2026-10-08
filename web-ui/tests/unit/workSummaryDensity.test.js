// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import WorkSummary from "../../src/lib/components/WorkSummary.svelte";
import { workSummaryModel } from "../../src/lib/workSummary.js";

afterEach(cleanup);

const NOW = Date.parse("2026-10-04T12:00:00Z");
const ago = (ms) => new Date(NOW - ms).toISOString();

const row = {
  ref: "card:release-b",
  title: "Release B",
  work_summary: {
    status: {
      state: "blocked",
      label: "Blocked",
      reason: "A step on the critical path is blocked.",
    },
    set_status: { state: "in_progress", label: "In progress" },
    progress: { done: 2, total: 5, unit: "steps" },
    next: { id: "ship", title: "Ship it", ref: "", more: 1 },
    attention: { count: 3, oldest_age: 9000 },
    last_movement_at: ago(2 * 3_600_000),
  },
};

const at = (density, overrides = {}) => {
  const { container } = render(WorkSummary, {
    summary: workSummaryModel({ ...row, ...overrides }, { now: NOW }),
    density,
    title: "Release B",
    now: NOW,
  });
  return container;
};

describe("every density says the same thing about the state", () => {
  it.each(["row", "card", "header"])("%s shows the computed status", (d) => {
    const el = at(d).querySelector("[data-health]");
    expect(el.dataset.health).toBe("blocked");
    expect(el.textContent).toContain("Blocked");
  });

  it.each(["row", "card", "header"])("%s shows the stored phase", (d) => {
    expect(
      at(d).querySelector("[data-summary-set-status]").textContent,
    ).toContain("marked in progress");
  });

  it.each(["row", "card", "header"])("%s shows the same progress", (d) => {
    expect(
      at(d).querySelector("[data-summary-progress]").textContent,
    ).toContain("2/5");
  });

  it("renders nothing at all when core computed no status", () => {
    const { container } = render(WorkSummary, { summary: null });
    expect(container.querySelector("[data-work-summary]")).toBeNull();
  });
});

describe("density decides how much fits", () => {
  it("keeps a row to the status, the stored phase, progress and asks", () => {
    /*
     * No age badge: a row-density host is a list, and the Tasks table has a
     * Last checked column of its own. A fourth badge in a 12rem Status cell
     * wrapped every row onto a second line.
     */
    const el = at("row");
    expect(el.querySelector("[data-freshness]")).toBeNull();
    expect(el.querySelector("[data-tile-steps]")).toBeNull();
    expect(el.querySelector("[data-tile-next]")).toBeNull();
    expect(el.querySelector("[data-summary-reason]")).toBeNull();
    expect(el.querySelector("[data-summary-attention]").textContent).toContain(
      "3 asks",
    );
  });

  it("gives a card the age, the next step and a progress bar", () => {
    const el = at("card");
    expect(el.querySelector("[data-freshness]")).not.toBeNull();
    expect(el.querySelector("[data-tile-next]").textContent).toContain(
      "Ship it",
    );
    expect(el.querySelector("progress")).not.toBeNull();
  });

  it("prefers the step lists over the single next step on a card", () => {
    const el = at("card", {
      work_summary: {
        ...row.work_summary,
        steps: {
          window_hours: 168,
          completed: { items: [], more: 0 },
          current: {
            items: [{ id: "build", title: "Build it", status: "blocked" }],
            more: 0,
          },
          next: { items: [], more: 2 },
        },
      },
    });
    expect(el.querySelector('[data-tile-step="build"]').textContent).toContain(
      "Build it",
    );
    expect(el.querySelector("[data-tile-step-blocked]")).not.toBeNull();
    expect(el.querySelector("[data-tile-next]")).toBeNull();
  });

  it("gives a header the computed reason on screen, not only on hover", () => {
    const el = at("header");
    expect(el.querySelector("[data-summary-reason]").textContent).toContain(
      "critical path is blocked",
    );
    // And progress carries its unit, where a row has only room for the count.
    expect(el.querySelector("[data-summary-progress]").textContent).toContain(
      "2/5 steps",
    );
  });
});

describe("parts core omitted are not invented", () => {
  it("shows only the status when that is all core sent", () => {
    const el = at("card", {
      work_summary: { status: { state: "no_plan", label: "No plan" } },
    });
    expect(el.querySelector("[data-health]").dataset.health).toBe("no_plan");
    for (const absent of [
      "[data-summary-set-status]",
      "[data-summary-progress]",
      "[data-summary-attention]",
      "[data-summary-foot]",
      "[data-tile-next]",
      "[data-freshness]",
    ]) {
      expect(el.querySelector(absent), absent).toBeNull();
    }
  });

  it("marks a lower-bound ask count rather than stating it as exact", () => {
    const el = at("row", {
      work_summary: {
        ...row.work_summary,
        attention: { count: 50, oldest_age: 10, truncated: true },
      },
    });
    const badge = el.querySelector("[data-summary-attention]");
    expect(badge.textContent).toContain("50+ asks");
    expect(badge.getAttribute("aria-label")).toContain("50 or more open asks");
  });

  it("marks progress whose linked work could not all be read", () => {
    const el = at("row", {
      work_summary: {
        ...row.work_summary,
        progress: { done: 1, total: 4, unit: "cards", truncated: true },
      },
    });
    /*
     * `1+/4`, not `1/4+`: the total is exact in both of core's branches and
     * only the count of finished work can be a lower bound, so `1/4+` read
     * as "more than four".
     */
    const count = el.querySelector("[data-summary-progress]");
    expect(count.textContent).toBe("1+/4");
    expect(count.getAttribute("aria-label")).toContain(
      "at least 1 of 4 cards done",
    );
  });
});
