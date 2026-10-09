// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { mount, unmount } from "svelte";
import { afterEach, describe, expect, it } from "vitest";

import Time from "../../src/lib/time/Time.svelte";

const NOW = Date.parse("2026-10-09T12:00:00Z");
const PAST = new Date(NOW - 3 * 3_600_000).toISOString();

afterEach(cleanup);

describe("Time", () => {
  it("does not paint fallback while waiting to format a real instant", () => {
    const target = document.createElement("div");
    const component = mount(Time, {
      target,
      props: { value: PAST, fallback: "—", now: NOW },
    });
    expect(target.textContent).not.toContain("—");
    expect(target.querySelector("time")?.getAttribute("datetime")).toBe(PAST);
    expect(target.querySelector("time")?.textContent?.trim()).toBe("");
    unmount(component);
  });

  it("paints fallback only when there is no instant", () => {
    const target = document.createElement("div");
    const component = mount(Time, {
      target,
      props: { value: "", fallback: "—", now: NOW },
    });
    expect(target.textContent).toContain("—");
    expect(target.querySelector("time")).toBeNull();
    unmount(component);
  });

  it("shows the local phrase after mount, not the missing-time fallback", () => {
    const { container } = render(Time, {
      value: PAST,
      fallback: "—",
      now: NOW,
    });
    expect(container.textContent).not.toContain("—");
    expect(container.querySelector("time")?.textContent?.trim()).toBe(
      "3 h ago",
    );
  });
});
