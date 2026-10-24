// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import LiveReportPanel from "../../src/lib/components/reports/LiveReportPanel.svelte";

const NOW = Date.parse("2026-10-09T12:00:00Z");

afterEach(cleanup);

function fleetPanel(enrollments) {
  return {
    type: "live-fleet-health",
    live: {
      status: "ok",
      data: {
        active_host_count: 0,
        host_count: 0,
        hosts: [],
        enrollment_available: true,
        enrollment_count: enrollments.length,
        enrollments,
        series: [],
      },
    },
  };
}

describe("LiveReportPanel fleet enrollment expiry", () => {
  it("names remaining duration, and expired once the deadline has passed", () => {
    const { container } = render(LiveReportPanel, {
      panel: fleetPanel([
        {
          requested_slug: "future-host",
          status: "pending",
          expires_at: new Date(NOW + 41 * 60_000).toISOString(),
        },
        {
          requested_slug: "past-host",
          status: "pending",
          expires_at: new Date(NOW - 3 * 3_600_000).toISOString(),
        },
      ]),
      now: NOW,
    });
    const text = container.textContent.replace(/\s+/g, " ");
    expect(text).toContain("future-host");
    expect(text).toContain("pending · expires in 41m");
    expect(text).toContain("past-host");
    expect(text).toContain("pending · expired");
    expect(text).not.toMatch(/expires \d+ h ago/);
    expect(text).not.toContain("expires expired");
  });
});

describe("clamped live prose", () => {
  it("does not leave a link in the tab order", () => {
    const { container } = render(LiveReportPanel, {
      panel: {
        type: "live-asks",
        live: {
          status: "ok",
          data: {
            items: [
              {
                id: "ask-1",
                title: "Ship the brief",
                status: "open",
                age_seconds: 60,
                response_text:
                  "Details in [the notes](https://example.com/notes).",
              },
            ],
          },
        },
      },
      organizationSlug: "acme",
      workspaceSlug: "ops",
      now: NOW,
    });
    const prose = container.querySelector(".row-prose");
    expect(prose.textContent).toContain("the notes");
    expect(
      prose.querySelector(
        "a:not([tabindex='-1']), button:not([tabindex='-1']), input, select, textarea, [tabindex]:not([tabindex='-1'])",
      ),
    ).toBeNull();
    expect(container.querySelector("a")?.textContent).toContain(
      "Ship the brief",
    );
  });

  it("does not leave an initiative summary link in the tab order", () => {
    const { container } = render(LiveReportPanel, {
      panel: {
        type: "live-initiatives",
        live: {
          status: "ok",
          data: {
            items: [
              {
                ref: "card:ship",
                title: "Ship the brief",
                summary_text:
                  "Read [the plan](https://example.com/plan) first.",
              },
            ],
          },
        },
      },
      organizationSlug: "acme",
      workspaceSlug: "ops",
      now: NOW,
    });
    const summary = container.querySelector(".markdown-rendered.summary");
    expect(summary.textContent).toContain("the plan");
    expect(
      summary.querySelector(
        "a:not([tabindex='-1']), button:not([tabindex='-1']), input, select, textarea, [tabindex]:not([tabindex='-1'])",
      ),
    ).toBeNull();
    expect(container.querySelector("a")?.textContent).toContain(
      "Ship the brief",
    );
  });
});

describe("loop silence banner", () => {
  it("dates the silence from the silent loops, not one that ran today", () => {
    const loop = (health, at) => ({
      health,
      last_run_at: at,
      runs_7d: {},
    });
    const { container } = render(LiveReportPanel, {
      panel: {
        type: "live-loops",
        live: {
          status: "ok",
          data: {
            loops: [
              ...["a", "b", "c", "d"].map((id) => ({
                ref: `card:${id}`,
                title: id,
                loop: loop("overdue", "2026-10-02T15:45:00Z"),
              })),
              {
                ref: "card:e",
                title: "e",
                loop: loop("healthy", "2026-10-09T11:00:00Z"),
              },
            ],
          },
        },
      },
      now: NOW,
    });
    const text = container.textContent.replace(/\s+/g, " ");
    expect(text).toContain("4 of 5 loops stopped reporting around Oct 2");
    expect(text).not.toContain("around Oct 9");
  });
});

describe("live collection truncation", () => {
  const panel = (
    hasMore,
    rows = [{ ref: "agent:a", title: "a", state: "working" }],
  ) => ({
    type: "live-collection",
    title: "Agents",
    live: {
      status: "ok",
      data: {
        states: [{ key: "working", label: "Working", tone: "good" }],
        groups: [
          {
            key: "all",
            rows,
          },
        ],
        has_more: hasMore,
      },
    },
  });
  it("says when core returned only part of the collection", () => {
    const { container } = render(LiveReportPanel, {
      panel: panel(true),
      now: NOW,
    });
    expect(
      container.querySelector("[data-live-collection-more]")?.textContent,
    ).toMatch(/Showing 1; there are more\./);
  });
  it("says so even when none of the rows core read matched", () => {
    const { container } = render(LiveReportPanel, {
      panel: panel(true, []),
      now: NOW,
    });
    expect(
      container.querySelector("[data-live-collection-more]")?.textContent,
    ).toMatch(/Showing 0; there are more\./);
    expect(container.textContent).not.toContain("Nothing in this view yet.");
  });
  it("stays quiet when the collection is complete", () => {
    const { container } = render(LiveReportPanel, {
      panel: panel(false),
      now: NOW,
    });
    expect(container.querySelector("[data-live-collection-more]")).toBeNull();
  });
});
