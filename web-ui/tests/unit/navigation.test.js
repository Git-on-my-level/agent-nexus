import { describe, expect, it } from "vitest";

import { isKnownSection, navigationItems } from "../../src/lib/navigation.js";

describe("navigation model", () => {
  it("includes expected top-level labels", () => {
    expect(navigationItems.map((item) => item.label)).toEqual([
      "Home",
      "Inbox",
      "Threads",
      "Artifacts",
    ]);
  });

  it("detects known routes", () => {
    expect(isKnownSection("/")).toBe(true);
    expect(isKnownSection("/threads")).toBe(true);
    expect(isKnownSection("/missing")).toBe(false);
  });
});
