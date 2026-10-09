// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";

import RecentChanges from "../../src/lib/components/overview/RecentChanges.svelte";

vi.mock("$app/stores", async () => {
  const { writable } = await import("svelte/store");
  return {
    page: writable({
      url: new URL("http://localhost/o/local/w/local/overview"),
      params: { organization: "local", workspace: "local" },
    }),
  };
});

const iso = (date) => date.toISOString();
const item = (kind, ref, title) => ({ kind, ref, title, ts: iso(new Date()) });

afterEach(cleanup);

describe("Recent changes", () => {
  it("renders nothing on a first visit, when there is no baseline", () => {
    const { container } = render(RecentChanges, {
      digest: { since: null, items: [] },
    });
    expect(container.querySelector("[data-overview-section]")).toBeNull();
  });

  it("says nothing is new rather than disappearing on a quiet visit", () => {
    const since = new Date();
    since.setHours(since.getHours() - 2);
    const { container, getByText } = render(RecentChanges, {
      digest: { since: iso(since), items: [] },
    });
    expect(
      container.querySelector('[data-overview-section="changes"]'),
    ).toBeTruthy();
    expect(
      container
        .querySelector("[data-overview-changes-empty]")
        .textContent.trim(),
    ).toMatch(/^Nothing new since .+\.$/);
    expect(getByText("Recent changes")).toBeTruthy();
  });

  it("dates a baseline that is not today, so quiet does not read as this morning", () => {
    const since = new Date();
    since.setDate(since.getDate() - 4);
    const { container } = render(RecentChanges, {
      digest: { since: iso(since), items: [] },
    });
    const line = container.querySelector(
      "[data-overview-changes-empty]",
    ).textContent;
    // A time of day alone would claim this morning.
    expect(line).toContain(
      since.toLocaleDateString(undefined, {
        month: "short",
        day: "numeric",
        year: "numeric",
      }),
    );
  });

  it("lists what changed, worst first, each linked to the task it is about", () => {
    const since = new Date();
    since.setHours(since.getHours() - 1);
    const { container } = render(RecentChanges, {
      digest: {
        since: iso(since),
        items: [
          item("step_completed", "card:a", "Publish"),
          item("initiative_blocked", "card:b", "Release B"),
        ],
      },
    });
    const section = container.querySelector(
      '[data-overview-section="changes"]',
    );
    expect(
      [...section.querySelectorAll("[data-since-kind]")].map(
        (node) => node.dataset.sinceKind,
      ),
    ).toEqual(["initiative_blocked", "step_completed"]);
    expect(section.querySelector("a").getAttribute("href")).toBe(
      "/o/local/w/local/tasks/card%3Ab",
    );
    expect(section.textContent).toContain("1 step done");
    expect(container.querySelector("[data-overview-changes-empty]")).toBeNull();
  });
});
