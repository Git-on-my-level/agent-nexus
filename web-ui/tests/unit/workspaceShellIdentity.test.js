import { describe, expect, it } from "vitest";

import { computeWorkspaceShellIdentity } from "../../src/lib/workspaceShellIdentity.js";

describe("computeWorkspaceShellIdentity", () => {
  it("prefers control plane display name with workspace username as secondary", () => {
    const out = computeWorkspaceShellIdentity({
      hostedMode: true,
      hostedAccount: {
        display_name: "Alex Example",
        email: "alex@example.com",
      },
      selectedActorName: "external.abc123",
      authenticatedAgent: { username: "external.abc123" },
    });
    expect(out.primaryLabel).toBe("Alex Example");
    expect(out.secondaryLabel).toBe("external.abc123");
    expect(out.initials).toBe("AE");
  });

  it("uses email when display name missing", () => {
    const out = computeWorkspaceShellIdentity({
      hostedMode: true,
      hostedAccount: { email: "pat@example.com" },
      selectedActorName: "external.x",
      authenticatedAgent: { username: "external.x" },
    });
    expect(out.primaryLabel).toBe("pat@example.com");
    expect(out.secondaryLabel).toBe("external.x");
  });

  it("omits secondary when CP primary matches workspace username", () => {
    const out = computeWorkspaceShellIdentity({
      hostedMode: true,
      hostedAccount: { display_name: "same", email: "x@y.com" },
      selectedActorName: "same",
      authenticatedAgent: { username: "same" },
    });
    expect(out.primaryLabel).toBe("same");
    expect(out.secondaryLabel).toBe("");
  });

  it("falls back to core display when not hosted or no CP profile", () => {
    const out = computeWorkspaceShellIdentity({
      hostedMode: false,
      hostedAccount: null,
      selectedActorName: "dev.actor",
      authenticatedAgent: { username: "dev.actor" },
    });
    expect(out.primaryLabel).toBe("dev.actor");
    expect(out.secondaryLabel).toBe("");
  });
});
