import { describe, expect, it } from "vitest";

import { buildRegistrationMessage } from "../../src/lib/inviteRegistrationMessage.js";

describe("inviteRegistrationMessage", () => {
  it("fills in agent name and username when provided", () => {
    const message = buildRegistrationMessage(
      "oinv_123",
      "https://example.com/oar/team-alpha",
      "hermes-prod",
      "hermes.prod",
    );

    expect(message).toContain(
      "oar --base-url https://example.com/oar/team-alpha --agent hermes-prod auth register --username hermes.prod --invite-token oinv_123",
    );
    expect(message).not.toContain("replace any placeholder values");
  });

  it("tells the agent how placeholder values behave when names are missing", () => {
    const message = buildRegistrationMessage(
      "oinv_123",
      "https://example.com/oar/team-alpha",
      "",
      "",
    );

    expect(message).toContain(
      "If you want to set your own agent profile name and username, replace the placeholder values before running the command.",
    );
    expect(message).toContain(
      "If you leave the placeholders in place, OAR chooses the agent profile name and the username during registration.",
    );
    expect(message).toContain("--agent <agent-name>");
    expect(message).toContain("--username <username>");
  });
});
