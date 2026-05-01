import { describe, expect, it } from "vitest";

import { enrichPrincipalsWithWakeRouting } from "../../src/lib/principalWakeRouting.js";

describe("principalWakeRouting", () => {
  it("maps backend online wake routing into the UI badge model", async () => {
    await expect(
      enrichPrincipalsWithWakeRouting([
        {
          principal_kind: "agent",
          username: "m4-hermes",
          wake_routing: {
            applicable: true,
            handle: "m4-hermes",
            taggable: true,
            online: true,
            state: "online",
            summary: "Online as @m4-hermes.",
          },
        },
      ]),
    ).resolves.toEqual([
      {
        principal_kind: "agent",
        username: "m4-hermes",
        wake_routing: {
          applicable: true,
          handle: "m4-hermes",
          taggable: true,
          online: true,
          state: "online",
          summary: "Online as @m4-hermes.",
        },
        wakeRouting: {
          applicable: true,
          handle: "m4-hermes",
          taggable: true,
          online: true,
          offline: false,
          state: "online",
          badgeLabel: "Online",
          badgeClass: "bg-ok-soft text-ok-text",
          summary: "Online as @m4-hermes.",
        },
      },
    ]);
  });

  it("marks missing backend wake routing as unavailable instead of deriving liveness locally", async () => {
    await expect(
      enrichPrincipalsWithWakeRouting([
        {
          principal_kind: "agent",
          username: "m4-hermes",
        },
      ]),
    ).resolves.toEqual([
      {
        principal_kind: "agent",
        username: "m4-hermes",
        wakeRouting: {
          applicable: true,
          handle: "m4-hermes",
          taggable: false,
          online: false,
          offline: false,
          state: "unknown",
          badgeLabel: "Unknown",
          badgeClass: "bg-bg-soft text-fg-muted",
          summary: "Wake routing status is unavailable right now.",
        },
      },
    ]);
  });
});
