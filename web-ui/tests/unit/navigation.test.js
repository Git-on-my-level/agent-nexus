import { describe, expect, it } from "vitest";

import {
  getShellContentConfig,
  isKnownSection,
  isMoreHubActivePath,
  navigationItems,
  settingsNavGroups,
  settingsNavItems,
} from "../../src/lib/navigation.js";

describe("navigation model", () => {
  it("includes expected primary nav labels", () => {
    expect(navigationItems.map((item) => item.label)).toEqual([
      "Inbox",
      "Tasks",
      "Docs",
    ]);
  });

  it("groups Settings apart from Diagnostics", () => {
    expect(
      settingsNavGroups.map((group) => [
        group.label,
        group.items.map((item) => item.label),
      ]),
    ).toEqual([
      ["Settings", ["Access", "Secrets", "Integrations"]],
      // Infrastructure surfaces, explicitly labelled rather than reachable
      // only by URL.
      ["Diagnostics", ["Audit", "Threads"]],
    ]);
    expect(settingsNavItems.map((item) => item.label)).toEqual([
      "Access",
      "Secrets",
      "Integrations",
      "Audit",
      "Threads",
    ]);
  });

  it("detects known routes", () => {
    expect(isKnownSection("/inbox")).toBe(true);
    expect(isKnownSection("/tasks")).toBe(true);
    expect(isKnownSection("/docs")).toBe(true);
    expect(isKnownSection("/pm")).toBe(true);
    expect(isKnownSection("/access")).toBe(true);
    expect(isKnownSection("/secrets")).toBe(true);
    expect(isKnownSection("/events")).toBe(true);
    expect(isKnownSection("/topics")).toBe(false);
    expect(isKnownSection("/boards")).toBe(false);
    expect(isKnownSection("/artifacts")).toBe(false);
    expect(isKnownSection("/trash")).toBe(false);
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
    expect(isMoreHubActivePath("/access")).toBe(true);
    expect(isMoreHubActivePath("/secrets")).toBe(true);
    expect(isMoreHubActivePath("/events")).toBe(true);
    expect(isMoreHubActivePath("/inbox")).toBe(false);
    expect(isMoreHubActivePath("/tasks")).toBe(false);
  });
});
