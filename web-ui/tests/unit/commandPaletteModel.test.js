import { describe, expect, it, vi } from "vitest";
import {
  fuzzyScore,
  goToCommands,
  rankCommands,
} from "../../src/lib/commandPaletteModel.js";

import { settingsNavGroups } from "../../src/lib/navigation.js";
describe("command palette model", () => {
  it("matches subsequences across words and rejects non-matches", () => {
    expect(fuzzyScore("assign leo", "Assign to Leo Park")).not.toBeNull();
    expect(fuzzyScore("assign leo", "Assign to Nina Vale")).toBeNull();
    expect(fuzzyScore("mov rev", "Move to In review")).not.toBeNull();
    expect(fuzzyScore("xyz", "Inbox")).toBeNull();
  });

  it("prefers prefixes and word starts over scattered matches", () => {
    expect(fuzzyScore("ts", "Tasks")).toBeGreaterThan(
      fuzzyScore("ts", "Secrets"),
    );
    expect(fuzzyScore("int", "Integrations")).toBeGreaterThan(
      fuzzyScore("int", "Ask PM about this task") ?? -Infinity,
    );
  });

  it("keeps group order and ranks within a group", () => {
    const commands = [
      { id: "a", group: "Actions", label: "Copy link" },
      { id: "b", group: "Actions", label: "Move to In review" },
      { id: "c", group: "Go to", label: "Inbox" },
      { id: "d", group: "Go to", label: "Integrations" },
    ];
    expect(rankCommands(commands, "").map((c) => c.id)).toEqual([
      "a",
      "b",
      "c",
      "d",
    ]);
    // "In review" starts a word; "link" only contains the letters.
    expect(rankCommands(commands, "in").map((c) => c.id)).toEqual([
      "b",
      "a",
      "c",
      "d",
    ]);
    expect(rankCommands(commands, "inb").map((c) => c.id)).toEqual(["c"]);
    // Keywords match too, a little below the label.
    expect(
      rankCommands(
        [{ id: "s", group: "Go to", label: "Access", keywords: ["Settings"] }],
        "settings",
      ).map((c) => c.id),
    ).toEqual(["s"]);
  });

  it("lists destinations with the shortcuts the palette binds", () => {
    const go = vi.fn();
    const commands = goToCommands({
      go,
      mod: "Ctrl",
      settingsGroups: [
        { label: "Settings", items: [{ label: "Access", href: "/access" }] },
        { label: "Diagnostics", items: [{ label: "Audit", href: "/events" }] },
      ],
    });
    expect(
      commands.map((c) => [c.label, (c.shortcut || []).join(" ")]),
    ).toEqual([
      ["Decide", "G D"],
      ["Watch", "G W"],
      ["Ask", "Ctrl J"],
      ["Access", ""],
      ["Audit", ""],
    ]);
    commands.find((c) => c.label === "Audit").run();
    expect(go).toHaveBeenCalledWith("/events");
  });

  /*
   * A PM agent runs on the reader's own computer, so a workspace can have
   * none. Ask is then left out; PM setup is a Settings destination.
   */
  it("leaves out Ask when no PM agent is connected, and reaches setup via Settings", () => {
    const go = vi.fn();
    const commands = goToCommands({
      go,
      pmVisible: false,
      settingsGroups: settingsNavGroups,
    });
    const ids = commands.map((command) => command.id);
    expect(ids).not.toContain("go:pm");
    expect(ids).toContain("go:/pm/setup");
    commands.find((command) => command.id === "go:/pm/setup").run();
    expect(go).toHaveBeenCalledWith("/pm/setup");
  });

  it("puts every destination one hop away", () => {
    const labels = goToCommands({
      go: vi.fn(),
      pmVisible: true,
      settingsGroups: settingsNavGroups,
    }).map((command) => command.label);
    expect(labels).toEqual([
      "Decide",
      "Watch",
      "Ask",
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
});
