import { describe, expect, it } from "vitest";

import {
  filterMentionCandidates,
  parseActiveMention,
  taggableAgentHandlesFromPrincipals,
} from "../../src/lib/threadMentionUtils.js";

describe("parseActiveMention", () => {
  it("returns null when there is no @", () => {
    expect(parseActiveMention("hello", 5)).toBeNull();
  });

  it("detects mention at start", () => {
    expect(parseActiveMention("@su", 3)).toEqual({ atIndex: 0, query: "su" });
  });

  it("requires whitespace before @ when not at start", () => {
    expect(parseActiveMention("foo@su", 6)).toBeNull();
    expect(parseActiveMention("foo @su", 7)).toEqual({
      atIndex: 4,
      query: "su",
    });
  });

  it("returns empty query right after @", () => {
    expect(parseActiveMention("hi @", 4)).toEqual({ atIndex: 3, query: "" });
  });

  it("stops at cursor inside the handle", () => {
    expect(parseActiveMention("@supply partial", 4)).toEqual({
      atIndex: 0,
      query: "sup",
    });
  });
});

describe("filterMentionCandidates", () => {
  it("filters by handle prefix case-insensitively", () => {
    const c = [
      { handle: "alpha.bot", displayLabel: "A" },
      { handle: "beta.bot", displayLabel: "B" },
    ];
    expect(filterMentionCandidates(c, "be")).toEqual([c[1]]);
    expect(filterMentionCandidates(c, "ALP")).toEqual([c[0]]);
  });
});

describe("taggableAgentHandlesFromPrincipals", () => {
  it("keeps only taggable non-revoked agents with usernames and sorts", () => {
    const out = taggableAgentHandlesFromPrincipals(
      [
        {
          principal_kind: "agent",
          username: "z.last",
          actor_id: "a1",
          revoked: false,
          wakeRouting: {
            taggable: true,
            badgeLabel: "Offline",
            badgeClass: "bg-amber-500/10 text-amber-400",
            summary: "Offline but registered.",
          },
        },
        {
          principal_kind: "human",
          username: "human",
          actor_id: "h1",
          revoked: false,
        },
        {
          principal_kind: "agent",
          username: "a.first",
          actor_id: "a2",
          revoked: false,
          wakeRouting: {
            taggable: true,
            badgeLabel: "Online",
            badgeClass: "bg-emerald-500/10 text-emerald-400",
            summary: "Online now.",
          },
        },
        {
          principal_kind: "agent",
          username: "gone",
          actor_id: "a3",
          revoked: true,
        },
        {
          principal_kind: "agent",
          username: "not.taggable",
          actor_id: "a4",
          revoked: false,
          wakeRouting: {
            taggable: false,
          },
        },
      ],
      (id) => (id === "a2" ? "Display A" : ""),
    );
    expect(out.map((r) => r.handle)).toEqual(["a.first", "z.last"]);
    expect(out[0].displayLabel).toBe("Display A");
    expect(out[1].displayLabel).toBe("z.last");
    expect(out).toEqual([
      {
        handle: "a.first",
        actorId: "a2",
        displayLabel: "Display A",
        presenceLabel: "Online",
        presenceClass: "bg-emerald-500/10 text-emerald-400",
        presenceSummary: "Online now.",
      },
      {
        handle: "z.last",
        actorId: "a1",
        displayLabel: "z.last",
        presenceLabel: "Offline",
        presenceClass: "bg-amber-500/10 text-amber-400",
        presenceSummary: "Offline but registered.",
      },
    ]);
  });
});
