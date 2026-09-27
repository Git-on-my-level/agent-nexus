import { describe, expect, it } from "vitest";

import { agentShortcutAction } from "../../src/lib/agentShortcuts.js";

const key = (value, extra = {}) => ({
  key: value,
  target: { tagName: "DIV", getAttribute: () => null },
  ...extra,
});

describe("agents roster shortcuts", () => {
  it("maps roster keys", () => {
    expect(agentShortcutAction(key("j"))).toEqual({ type: "next" });
    expect(agentShortcutAction(key("k"))).toEqual({ type: "previous" });
    expect(agentShortcutAction(key("Enter"))).toEqual({ type: "open" });
    expect(agentShortcutAction(key("i"))).toEqual({ type: "inbox" });
    expect(agentShortcutAction(key("t"))).toEqual({ type: "task" });
    expect(agentShortcutAction(key("?"))).toEqual({ type: "help" });
  });

  it("stays out of the way while typing, with modifiers, or under a dialog", () => {
    const input = { tagName: "INPUT", getAttribute: () => "text" };
    expect(agentShortcutAction(key("j", { target: input }))).toBeNull();
    expect(agentShortcutAction(key("j", { metaKey: true }))).toBeNull();
    expect(agentShortcutAction(key("j"), { modalOpen: true })).toBeNull();
    expect(agentShortcutAction(key("Escape"), { helpOpen: true })).toEqual({
      type: "close-help",
    });
  });
});
