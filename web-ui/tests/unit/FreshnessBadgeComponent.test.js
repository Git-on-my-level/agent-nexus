// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it } from "vitest";

import FreshnessBadge from "../../src/lib/components/FreshnessBadge.svelte";

const NOW = Date.parse("2026-10-07T12:00:00Z");
const ago = (hours) => new Date(NOW - hours * 3_600_000).toISOString();

afterEach(cleanup);

describe("FreshnessBadge", () => {
  it("renders the relative time in the tone its expectation gives it", () => {
    const { container } = render(FreshnessBadge, {
      at: ago(30),
      kind: "in_progress",
      now: NOW,
    });
    const badge = container.querySelector("time");
    expect(badge?.textContent?.trim()).toBe("1d");
    expect(badge?.className).toContain("ui-badge--warn");
    expect(badge?.getAttribute("data-freshness")).toBe("late");
    expect(badge?.getAttribute("datetime")).toBe(ago(30));
  });

  it("is green inside the expectation and red well past it", () => {
    const { container: ok } = render(FreshnessBadge, {
      at: ago(3),
      kind: "in_progress",
      now: NOW,
    });
    expect(ok.querySelector("time")?.className).toContain("ui-badge--ok");

    const { container: bad } = render(FreshnessBadge, {
      at: ago(96),
      kind: "in_progress",
      now: NOW,
    });
    expect(bad.querySelector("time")?.className).toContain("ui-badge--danger");
  });

  it("carries the expectation in an instant tooltip, not a slow `title`", () => {
    const { container } = render(FreshnessBadge, {
      at: ago(2),
      kind: "initiative",
      verb: "moved",
      now: NOW,
    });
    const badge = container.querySelector("time");
    expect(badge?.getAttribute("title")).toBeNull();
    expect(badge?.getAttribute("data-tooltip")).toContain(
      "within the expected 3d",
    );
    expect(badge?.getAttribute("aria-label")).toMatch(/^Moved /);
  });

  it("renders nothing for finished work, or for no instant at all", () => {
    const { container: closed } = render(FreshnessBadge, {
      at: ago(900),
      kind: "closed",
      now: NOW,
    });
    expect(closed.querySelector("time")).toBeNull();

    const { container: unknown } = render(FreshnessBadge, {
      at: "",
      kind: "in_progress",
      now: NOW,
    });
    expect(unknown.querySelector("time")).toBeNull();
  });
});
