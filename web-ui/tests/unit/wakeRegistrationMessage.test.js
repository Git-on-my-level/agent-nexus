import { describe, expect, it } from "vitest";

import { buildWakeRegistrationMessage } from "../../src/lib/wakeRegistrationMessage.js";

describe("wakeRegistrationMessage", () => {
  it("builds an adapter-agnostic registration message for existing agent auth", () => {
    const message = buildWakeRegistrationMessage(
      "https://core.example.com",
      "ws-team-alpha",
      "m4-hermes",
    );

    expect(message).toContain(
      "You already have ANX CLI auth for the core API at https://core.example.com.",
    );
    expect(message).toContain("anx bridge install");
    expect(message).toContain(
      "anx bridge init-config --kind '<bridge-kind>' --output ./agent.toml --workspace-id ws-team-alpha --handle m4-hermes",
    );
    expect(message).toContain(
      "anx bridge import-auth --config ./agent.toml --from-profile '<anx-profile>'",
    );
    expect(message).toContain(
      "anx-agent-bridge registration apply --config ./agent.toml",
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

    expect(message).toContain("<ANX_CORE_BASE_URL>");
    expect(message).toContain("--workspace-id <workspace-id>");
    expect(message).toContain("--handle <handle>");
    expect(message).toContain("--from-profile '<anx-profile>'");
    expect(message).toContain("@<handle>'s wake registration");
  });
});
