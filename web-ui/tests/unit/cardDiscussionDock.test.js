import { describe, expect, it } from "vitest";

import {
  cardDiscussionDockHostEnabled,
  cardDiscussionDockPlacement,
} from "$lib/cardDiscussionDock";

describe("card discussion dock placement", () => {
  it("keeps full-page cards on the viewport dock path", () => {
    expect(cardDiscussionDockPlacement("page")).toBe("viewport");
    expect(cardDiscussionDockHostEnabled("page", "thread-123")).toBe(false);
  });

  it("keeps modal cards on the embedded dock host", () => {
    expect(cardDiscussionDockPlacement("modal")).toBe("embedded");
    expect(cardDiscussionDockHostEnabled("modal", "thread-123")).toBe(true);
  });

  it("does not enable an embedded host without a thread", () => {
    expect(cardDiscussionDockHostEnabled("modal", "")).toBe(false);
  });
});
