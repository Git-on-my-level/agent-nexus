import { describe, expect, it } from "vitest";

import {
  getShellContentConfig,
  isKnownSection,
  navigationItems,
} from "../../src/lib/navigation.js";

describe("navigation model", () => {
  it("includes expected top-level labels", () => {
    expect(navigationItems.map((item) => item.label)).toEqual([
      "Home",
      "Inbox",
      "Threads",
      "Boards",
      "Artifacts",
      "Access",
      "Docs",
    ]);
  });

  it("detects known routes", () => {
    expect(isKnownSection("/")).toBe(true);
    expect(isKnownSection("/threads")).toBe(true);
    expect(isKnownSection("/boards")).toBe(true);
    expect(isKnownSection("/access")).toBe(true);
    expect(isKnownSection("/missing")).toBe(false);
  });

  it("provides shell content config for access route", () => {
    const config = getShellContentConfig("/access");
    expect(config.mode).toBe("wide");
    expect(config.maxWidth).toBe("84rem");
  });
});
