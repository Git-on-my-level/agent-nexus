import { describe, expect, it, vi } from "vitest";
import {
  fuzzyScore,
  goToCommands,
  rankCommands,
} from "../../src/lib/commandPaletteModel.js";

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
      ["Overview", "G O"],
      ["Inbox", "G I"],
      ["Agents", "G A"],
      ["Tasks", "G T"],
      ["Docs", "G D"],
      ["Ask PM", "Ctrl J"],
      ["Access", ""],
      ["Audit", ""],
    ]);
    commands.find((c) => c.label === "Audit").run();
    expect(go).toHaveBeenCalledWith("/events");
  });

  /*
   * A PM agent runs on the reader's own computer, so a workspace can have
   * none. The palette must then offer setup, not a conversation nothing can
   * answer — and never both.
   */
  it("offers setup instead of Ask PM when no PM agent is onboarded", () => {
    const go = vi.fn();
    const commands = goToCommands({
      go,
      pmVisible: false,
      pmNeedsSetup: true,
    });
    const ids = commands.map((command) => command.id);
    expect(ids).toContain("go:pm-setup");
    expect(ids).not.toContain("go:pm");
    commands.find((command) => command.id === "go:pm-setup").run();
    expect(go).toHaveBeenCalledWith("/pm/setup");
  });

  /*
   * An older core reports no PM state. That is not evidence of no PM, so Ask
   * PM stays and no setup is offered — the palette must not invite the reader
   * to install a PM that may already be running.
   */
  it("keeps Ask PM, and offers no setup, when core does not say", () => {
    const ids = goToCommands({
      go: vi.fn(),
      pmVisible: true,
      pmNeedsSetup: false,
    }).map((command) => command.id);
    expect(ids).toContain("go:pm");
    expect(ids).not.toContain("go:pm-setup");
  });
});
