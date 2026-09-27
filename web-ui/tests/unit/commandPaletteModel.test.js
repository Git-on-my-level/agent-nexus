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
      ["Inbox", "G I"],
      ["Tasks", "G T"],
      ["Docs", "G D"],
      ["Ask PM", "Ctrl J"],
      ["Access", ""],
      ["Audit", ""],
    ]);
    commands.find((c) => c.label === "Audit").run();
    expect(go).toHaveBeenCalledWith("/events");
  });
});
