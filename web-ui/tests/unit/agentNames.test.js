import { describe, expect, it } from "vitest";

import {
  buildActorNameMap,
  findAgentSummary,
  lookupActorDisplayName,
} from "../../src/lib/actorSession.js";
import { buildInboxRows } from "../../src/lib/inboxMailbox.js";

const CODEX = {
  id: "agent-codex",
  actor_id: "actor-codex",
  host_id: "host-1",
  handle: "codex.workstation-a",
  display_name: "codex on workstation-a",
};

describe("derived agent names", () => {
  it("name a host-derived agent by its host relation everywhere", () => {
    const actors = [{ id: "actor-codex", display_name: "Leo Park" }];
    const map = buildActorNameMap(actors, [], [CODEX]);
    expect(map.get("actor-codex")).toBe("codex on workstation-a");
    expect(map.get("agent-codex")).toBe("codex on workstation-a");
    expect(lookupActorDisplayName("actor-codex", actors, [], [CODEX])).toBe(
      "codex on workstation-a",
    );
    // A standalone agent (no host) keeps its actor name.
    expect(
      buildActorNameMap(actors, [], [{ ...CODEX, host_id: null }]).get(
        "actor-codex",
      ),
    ).toBe("Leo Park");
  });

  it("finds a roster entry by actor ref, agent id or handle", () => {
    expect(findAgentSummary("actor:actor-codex", [CODEX])).toBe(CODEX);
    expect(findAgentSummary("codex.workstation-a", [CODEX])).toBe(CODEX);
    expect(findAgentSummary("someone", [CODEX])).toBeNull();
  });

  it("prefers the agent name over the requester label on Inbox rows", () => {
    const [row] = buildInboxRows({
      inboxItems: [
        {
          id: "inbox:ask:t:e1",
          kind: "ask",
          title: "Ship?",
          requester_actor_id: "actor-codex",
          requester_label: "Leo Park",
          related_refs: [],
          response_proposals: ["Yes"],
        },
      ],
      agentName: (id) => (id === "actor-codex" ? "codex on workstation-a" : ""),
    });
    expect(row.requester.name).toBe("codex on workstation-a");
  });
});
