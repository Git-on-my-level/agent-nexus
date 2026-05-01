import { describe, expect, it } from "vitest";

import { suggestAgentUsername } from "../../src/lib/agentInviteIdentity.js";

describe("agentInviteIdentity", () => {
  it("suggests a mention-safe username from an agent profile name", () => {
    expect(suggestAgentUsername("Claude Code")).toBe("claude-code");
    expect(suggestAgentUsername("  Hermes Prod  ")).toBe("hermes-prod");
    expect(suggestAgentUsername("M4.Hermes_prod")).toBe("m4-hermes-prod");
  });

  it("keeps suggestions within backend username bounds", () => {
    expect(suggestAgentUsername("AI")).toBe("ai-agent");
    expect(suggestAgentUsername("!!!")).toBe("");
    expect(suggestAgentUsername("x".repeat(80))).toHaveLength(64);
  });
});
