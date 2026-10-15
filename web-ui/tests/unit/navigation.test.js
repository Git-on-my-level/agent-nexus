import { describe, expect, it } from "vitest";

import {
  getDestinationSkeleton,
  getShellContentConfig,
  isKnownSection,
  isMoreHubActivePath,
  navigationItems,
  settingsNavGroups,
  settingsNavItems,
} from "../../src/lib/navigation.js";

describe("navigation model", () => {
  it("has exactly three primary destinations", () => {
    expect(
      navigationItems.map((item) => [item.label, item.href, item.hint]),
    ).toEqual([
      ["Decide", "/decide", "Waiting on you"],
      ["Watch", "/watch", "Outcomes"],
      ["Ask", "/pm", "Your PM"],
    ]);
  });

  it("flags only Ask as needing a connected PM", () => {
    expect(
      navigationItems.filter((item) => item.requiresPm).map((i) => i.label),
    ).toEqual(["Ask"]);
  });

  it("groups Settings apart from Diagnostics", () => {
    expect(
      settingsNavGroups.map((group) => [
        group.label,
        group.items.map((item) => item.label),
      ]),
    ).toEqual([
      ["Workspace", ["Initiatives", "Docs", "Agents", "Archive"]],
      [
        "Settings",
        ["Access", "Secrets", "Integrations", "Notifications", "PM setup"],
      ],
      // Infrastructure surfaces, explicitly labelled rather than reachable
      // only by URL.
      ["Diagnostics", ["Audit", "Threads"]],
    ]);
    expect(settingsNavItems.map((item) => item.label)).toEqual([
      "Initiatives",
      "Docs",
      "Agents",
      "Archive",
      "Access",
      "Secrets",
      "Integrations",
      "Notifications",
      "PM setup",
      "Audit",
      "Threads",
    ]);
  });

  it("marks which items carry a count, so the shell renders one badge each", () => {
    expect(
      [...navigationItems, ...settingsNavItems]
        .filter((item) => item.count)
        .map((item) => [item.href, item.count]),
    ).toEqual([
      ["/decide", "decide-waiting"],
      ["/agents", "agents-working"],
      // Access sits inside the account menu, so the shell also puts this
      // number on the menu trigger.
      ["/access", "access-pending"],
    ]);
  });

  it("detects known routes", () => {
    expect(isKnownSection("/decide")).toBe(true);
    expect(isKnownSection("/watch")).toBe(true);
    expect(isKnownSection("/initiatives")).toBe(true);
    expect(isKnownSection("/agents")).toBe(true);
    expect(isKnownSection("/pm/setup")).toBe(true);
    expect(isKnownSection("/overview")).toBe(false);
    expect(isKnownSection("/inbox")).toBe(false);
    expect(isKnownSection("/tasks")).toBe(false);
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

  it("names a list destination's skeleton so the first paint says where you are", () => {
    expect(getDestinationSkeleton("/initiatives")).toEqual({
      title: "Initiatives",
      rows: 8,
    });
    expect(getDestinationSkeleton("/docs")).toEqual({ title: "Docs", rows: 8 });
    expect(getDestinationSkeleton("/docs/")).toEqual({
      title: "Docs",
      rows: 8,
    });
    expect(getDestinationSkeleton("/threads")).toEqual({
      title: "Threads",
      rows: 6,
    });
  });

  it("has no skeleton for pages that are not a list of rows", () => {
    // A guessed shape is worse than a blank: the card page, Overview and the
    // Inbox detail all paint something a row skeleton would misdescribe.
    expect(getDestinationSkeleton("/initiatives/card-1")).toBeNull();
    expect(getDestinationSkeleton("/docs/doc-1")).toBeNull();
    expect(getDestinationSkeleton("/watch")).toBeNull();
    expect(getDestinationSkeleton("/decide")).toBeNull();
  });

  it("marks mobile More tab active for hub and settings routes", () => {
    expect(isMoreHubActivePath("/more")).toBe(true);
    expect(isMoreHubActivePath("/more/")).toBe(true);
    expect(isMoreHubActivePath("/access")).toBe(true);
    expect(isMoreHubActivePath("/secrets")).toBe(true);
    expect(isMoreHubActivePath("/events")).toBe(true);
    expect(isMoreHubActivePath("/initiatives")).toBe(true);
    expect(isMoreHubActivePath("/initiatives/work-1")).toBe(true);
    expect(isMoreHubActivePath("/docs")).toBe(true);
    expect(isMoreHubActivePath("/agents")).toBe(true);
    expect(isMoreHubActivePath("/pm/setup")).toBe(true);
    expect(isMoreHubActivePath("/decide")).toBe(false);
    expect(isMoreHubActivePath("/watch")).toBe(false);
    expect(isMoreHubActivePath("/pm")).toBe(false);
  });

  it("keeps the fluid width for Decide, Watch and Initiatives", () => {
    for (const path of ["/decide", "/watch", "/initiatives", "/initiatives/x"])
      expect(getShellContentConfig(path).mode).toBe("fluid");
    expect(getShellContentConfig("/agents").mode).toBe("wide");
  });
});
