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
