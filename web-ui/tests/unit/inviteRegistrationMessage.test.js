import { describe, expect, it } from "vitest";

import {
  buildRegistrationCommand,
  buildRegistrationMessage,
} from "../../src/lib/inviteRegistrationMessage.js";

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
      "Check whether the ANX CLI is already installed:",
    );
    expect(message).toContain("anx --version");
    expect(message).toContain(
      "If that command is not found, install the ANX CLI:",
    );
    expect(message).toContain(
      "curl -sSfL https://raw.githubusercontent.com/Git-on-my-level/agent-nexus/main/scripts/install-anx.sh | sh",
    );
    expect(message).toContain(
      "Use the anx-core API origin for --base-url (the same value as the workspace coreBaseUrl), not the web app path under /o/.../w/....",
    );
    expect(message).toContain("Run this registration command:");
    expect(message).not.toContain("Replace the placeholder value");
  });

  it("defaults the CLI agent profile from the username", () => {
    const command = buildRegistrationCommand(
      "oinv_123",
      "https://core.example.com",
      "",
      "hermes.prod",
    );

    expect(command).toBe(
      "anx --base-url https://core.example.com --agent hermes.prod auth register --username hermes.prod --invite-token oinv_123",
    );

    const message = buildRegistrationMessage(
      "oinv_123",
      "https://core.example.com",
      "",
      "hermes.prod",
    );
    expect(message).not.toContain("agent profile name");
  });

  it("quotes CLI values that need shell escaping", () => {
    const message = buildRegistrationMessage(
      "oinv_123",
      "https://core.example.com",
      "Claude Code",
      "claude-code",
    );

    expect(message).toContain(
      "anx --base-url https://core.example.com --agent 'Claude Code' auth register --username claude-code --invite-token oinv_123",
    );
  });

  it("tells the agent to replace required placeholder values when username is missing", () => {
    const message = buildRegistrationMessage(
      "oinv_123",
      "https://core.example.com",
      "",
      "",
    );

    expect(message).toContain(
      "Replace the placeholder value for username before running the command.",
    );
    expect(message).toContain(
      "Run this registration command after filling in the placeholder:",
    );
    expect(message).toContain(
      "The CLI requires --username; it will not choose one automatically.",
    );
    expect(message).toContain("--agent '<username>'");
    expect(message).toContain("--username '<username>'");
  });
});
