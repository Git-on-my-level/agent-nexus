import { describe, expect, it } from "vitest";

import {
  getShellContentConfig,
  isKnownSection,
  isMoreHubActivePath,
  navigationItems,
  settingsNavItems,
} from "../../src/lib/navigation.js";

describe("navigation model", () => {
  it("includes expected primary nav labels", () => {
    expect(navigationItems.map((item) => item.label)).toEqual([
      "Home",
      "Inbox",
      "Topics",
      "Boards",
      "Docs",
    ]);
  });

  it("includes settings nav labels", () => {
    expect(settingsNavItems.map((item) => item.label)).toEqual([
      "Artifacts",
      "Trash",
      "Access",
    ]);
  });

  it("detects known routes", () => {
    expect(isKnownSection("/")).toBe(true);
    expect(isKnownSection("/topics")).toBe(true);
    expect(isKnownSection("/boards")).toBe(true);
    expect(isKnownSection("/docs")).toBe(true);
    expect(isKnownSection("/artifacts")).toBe(true);
    expect(isKnownSection("/trash")).toBe(true);
    expect(isKnownSection("/access")).toBe(true);
    expect(isKnownSection("/missing")).toBe(false);
  });

  it("provides shell content config for access route", () => {
    const config = getShellContentConfig("/access");
    expect(config.mode).toBe("wide");
    expect(config.maxWidth).toBe("84rem");
  });

  it("marks mobile More tab active for hub and settings routes", () => {
    expect(isMoreHubActivePath("/more")).toBe(true);
    expect(isMoreHubActivePath("/more/")).toBe(true);
    expect(isMoreHubActivePath("/artifacts")).toBe(true);
    expect(isMoreHubActivePath("/artifacts/abc")).toBe(true);
    expect(isMoreHubActivePath("/trash")).toBe(true);
    expect(isMoreHubActivePath("/access")).toBe(true);
    expect(isMoreHubActivePath("/inbox")).toBe(false);
    expect(isMoreHubActivePath("/")).toBe(false);
  });
});
