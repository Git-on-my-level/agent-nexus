// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/svelte";
import { readable } from "svelte/store";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const cache = vi.hoisted(() => ({
  readWorkSnapshot: vi.fn(),
  prefetchWork: vi.fn(),
}));
vi.mock("$app/stores", () => ({
  page: readable({
    url: new URL("http://localhost/o/acme/w/main/watch"),
    params: { organization: "acme", workspace: "main" },
  }),
}));
vi.mock("$lib/coreClient", () => ({ coreClient: { getWork: vi.fn() } }));
vi.mock("$lib/workCache.js", () => cache);

import InitiativeDetails from "../../src/lib/components/watch/InitiativeDetails.svelte";
import { BRIEF_COMPUTED_FIELDS } from "../../src/lib/brief.js";

const NOW = Date.parse("2026-10-15T09:00:00Z");
const initiative = {
  ref: "card:capture",
  id: "capture",
  title: "Every word captured",
  state: "moving",
  promise: { at: "2026-10-21T00:00:00Z", what: "Speaker ID live" },
  lastEvidenceAt: "2026-10-15T06:00:00Z",
  note: null,
  rollup: {
    total: 3,
    byState: { done: 1, in_progress: 2 },
    pastPromise: 0,
    needingViewer: 0,
    children: [
      {
        ref: "card:speaker",
        title: "Speaker identification",
        state: "in_progress",
        pastPromise: false,
        needsViewer: false,
      },
    ],
    workstreams: [],
    hasMore: false,
  },
};
const work = {
  ref: "card:capture",
  owner: "actor:luna",
  plan: { steps: [{ id: "a", title: "Wire the diarizer" }] },
  plan_state: { next_steps: ["a"] },
  summary: { status: { state: "moving" } },
};

beforeEach(() => {
  cache.readWorkSnapshot.mockReset().mockReturnValue(null);
  cache.prefetchWork.mockReset();
});
afterEach(cleanup);

describe("InitiativeDetails", () => {
  it("shows the brief's promise, evidence and rollup at once", () => {
    cache.prefetchWork.mockReturnValue(new Promise(() => {}));
    const { container } = render(InitiativeDetails, {
      initiative,
      href: "/o/acme/w/main/initiatives/capture",
      childHref: (ref) => `/c/${ref}`,
      now: NOW,
    });
    expect(screen.getByText("Speaker ID live · by Oct 21")).toBeTruthy();
    expect(screen.getByText("Last evidence 3h ago")).toBeTruthy();
    expect(screen.getByText("3 cards inside")).toBeTruthy();
    expect(screen.getByText("2 more on the initiative")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Speaker identification" }).href,
    ).toContain("/c/card:speaker");
    expect(
      screen.getByRole("link", { name: "Open initiative →" }).href,
    ).toContain("/initiatives/capture");
    expect(screen.getByText("Loading the plan…")).toBeTruthy();
    // Every computed piece names a brief field (I1).
    for (const el of container.querySelectorAll("[data-source]"))
      expect(BRIEF_COMPUTED_FIELDS).toContain(
        el.dataset.source.replace(/^computed:/, ""),
      );
  });

  it("reads the card once, lazily, for next steps and owner", async () => {
    cache.prefetchWork.mockResolvedValue(work);
    render(InitiativeDetails, { initiative, now: NOW });
    expect(await screen.findByText("Wire the diarizer")).toBeTruthy();
    expect(screen.getByText("luna", { exact: false })).toBeTruthy();
    expect(cache.prefetchWork).toHaveBeenCalledTimes(1);
    expect(cache.prefetchWork.mock.calls[0][0]).toBe("card:capture");
  });

  it("names the brief's owner before the card read lands", () => {
    cache.prefetchWork.mockReturnValue(new Promise(() => {}));
    render(InitiativeDetails, {
      initiative: { ...initiative, owner: "actor:sol" },
      now: NOW,
    });
    expect(screen.getByText("sol", { exact: false })).toBeTruthy();
  });

  it("paints from the cache without reading again", async () => {
    cache.readWorkSnapshot.mockReturnValue({ work, full: true });
    render(InitiativeDetails, { initiative, now: NOW });
    expect(await screen.findByText("Wire the diarizer")).toBeTruthy();
    expect(cache.prefetchWork).not.toHaveBeenCalled();
  });

  it("offers a retry when the read fails, without core's text", async () => {
    cache.prefetchWork.mockResolvedValueOnce(null).mockResolvedValueOnce(work);
    render(InitiativeDetails, { initiative, now: NOW });
    const retry = await screen.findByRole("button", { name: "Try again" });
    retry.click();
    expect(await screen.findByText("Wire the diarizer")).toBeTruthy();
    expect(cache.prefetchWork).toHaveBeenCalledTimes(2);
  });
});
