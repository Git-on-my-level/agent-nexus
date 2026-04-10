import { describe, expect, it } from "vitest";

import { buildWakeRegistrationMessage } from "../../src/lib/wakeRegistrationMessage.js";

describe("wakeRegistrationMessage", () => {
  it("builds an adapter-agnostic registration message for existing agent auth", () => {
    const message = buildWakeRegistrationMessage(
      "https://example.com/oar/team-alpha",
      "ws-team-alpha",
      "m4-hermes",
    );

    expect(message).toContain(
      "You already have OAR CLI auth for https://example.com/oar/team-alpha.",
    );
    expect(message).toContain("oar bridge install");
    expect(message).toContain(
      "oar bridge init-config --kind <bridge-kind> --output ./agent.toml --workspace-id ws-team-alpha --handle m4-hermes",
    );
    expect(message).toContain(
      "oar bridge import-auth --config ./agent.toml --from-profile <oar-profile>",
    );
    expect(message).toContain(
      "oar-agent-bridge registration apply --config ./agent.toml",
    );
    expect(message).toContain(
      "Use the bridge kind supported by this agent runtime.",
    );
    expect(message).toContain(
      "This updates @m4-hermes's wake registration on its principal",
    );
  });

  it("falls back to placeholders when context is missing", () => {
    const message = buildWakeRegistrationMessage("", "", "");

    expect(message).toContain("<OAR_WORKSPACE_URL>");
    expect(message).toContain("--workspace-id <workspace-id>");
    expect(message).toContain("--handle <handle>");
    expect(message).toContain("--from-profile <oar-profile>");
    expect(message).toContain("@<handle>'s wake registration");
  });
});
