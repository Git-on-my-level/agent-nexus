import { describe, expect, it } from "vitest";

import { buildRegistrationMessage } from "../../src/lib/inviteRegistrationMessage.js";

describe("inviteRegistrationMessage", () => {
  it("fills in agent name and username when provided", () => {
    const message = buildRegistrationMessage(
      "oinv_123",
      "https://core.example.com",
      "hermes-prod",
      "hermes.prod",
    );

    expect(message).toContain(
      "anx --base-url https://core.example.com --agent hermes-prod auth register --username hermes.prod --invite-token oinv_123",
    );
    expect(message).toContain(
      "Use the anx-core API origin for --base-url (the same value as the workspace coreBaseUrl), not the web app path under /o/.../w/....",
    );
    expect(message).not.toContain("replace any placeholder values");
  });

  it("tells the agent to replace required placeholder values when names are missing", () => {
    const message = buildRegistrationMessage(
      "oinv_123",
      "https://core.example.com",
      "",
      "",
    );

    expect(message).toContain(
      "Replace the placeholder values for agent profile name and username before running the command.",
    );
    expect(message).toContain(
      "The CLI requires --username; it will not choose one automatically.",
    );
    expect(message).toContain("--agent '<agent-name>'");
    expect(message).toContain("--username '<username>'");
  });
});
