import { describe, expect, it } from "vitest";

import {
  buildOwnerDisplay,
  compactOwnerName,
  splitOwnerLabel,
} from "../../src/lib/ownerDisplay.js";

const MAYA = {
  id: "actor-gds-producer",
  display_name: "Maya Chen (Studio producer)",
  tags: ["human", "producer"],
};
const PRIYA_ACTOR = { id: "actor-priya", display_name: "Priya", tags: [] };
const PRIYA_AGENT = {
  id: "agent-priya",
  actor_id: "actor-priya",
  host_id: "host-dev",
  handle: "priya.dev-host",
  display_name: "priya on dev-host",
};

const loaded = (extra = {}) => ({
  actors: [MAYA, PRIYA_ACTOR],
  principals: [],
  agents: [PRIYA_AGENT],
  actorsLoaded: true,
  agentsLoaded: true,
  ...extra,
});

describe("splitOwnerLabel", () => {
  it("lifts a trailing role out of the name", () => {
    expect(splitOwnerLabel("Maya Chen (Studio producer)")).toEqual({
      name: "Maya Chen",
      role: "Studio producer",
      host: "",
    });
  });

  it("splits a host relation only when the owner is an agent", () => {
    expect(splitOwnerLabel("priya on dev-host", { host: true })).toEqual({
      name: "priya",
      role: "",
      host: "dev-host",
    });
    expect(splitOwnerLabel("priya on dev-host").name).toBe("priya on dev-host");
  });

  it("keeps a name that is only a parenthetical", () => {
    expect(splitOwnerLabel("(unknown)").name).toBe("(unknown)");
  });

  it("is empty for nothing", () => {
    expect(splitOwnerLabel(null)).toEqual({ name: "", role: "", host: "" });
  });
});

describe("buildOwnerDisplay", () => {
  it("gives people and agents one format, detail in the title", () => {
    const ownerCell = buildOwnerDisplay(loaded());

    const human = ownerCell("actor-gds-producer");
    expect(human.pending).toBe(false);
    expect(human.name).toBe("Maya Chen");
    expect(human.isAgent).toBe(false);
    expect(human.title).toBe("Maya Chen · Studio producer");

    const agent = ownerCell("actor:actor-priya");
    expect(agent.name).toBe("priya");
    expect(agent.isAgent).toBe(true);
    expect(agent.title).toBe("priya · on dev-host · @priya.dev-host");
    // The avatar seed stays the id, so the colour never changes with the name.
    expect(agent.seed).toBe("actor-priya");
  });

  it("has no cell at all for an empty owner", () => {
    expect(buildOwnerDisplay(loaded())("")).toBeNull();
    expect(buildOwnerDisplay(loaded())(null)).toBeNull();
  });

  it("holds a placeholder until a directory read lands", () => {
    const beforeAnyRead = buildOwnerDisplay(
      loaded({ actorsLoaded: false, agentsLoaded: false }),
    )("actor-gds-producer");
    expect(beforeAnyRead.pending).toBe(true);
    expect(beforeAnyRead.name).toBe("");
    expect(beforeAnyRead.title).toContain("actor-gds-producer");

    // Actors landed and name this owner: no reason to keep waiting.
    expect(
      buildOwnerDisplay(loaded({ agentsLoaded: false }))("actor-gds-producer")
        .pending,
    ).toBe(false);
  });

  it("waits for the roster rather than flashing a handle it will replace", () => {
    const rosterPending = buildOwnerDisplay({
      actors: [],
      principals: [],
      agents: [],
      actorsLoaded: true,
      agentsLoaded: false,
    });
    expect(rosterPending("actor-priya").pending).toBe(true);

    const rosterLanded = buildOwnerDisplay({
      actors: [],
      principals: [],
      agents: [PRIYA_AGENT],
      actorsLoaded: true,
      agentsLoaded: true,
    });
    expect(rosterLanded("actor-priya")).toMatchObject({
      pending: false,
      name: "priya",
      isAgent: true,
    });
  });

  it("falls back to the raw owner once every read has answered", () => {
    const ownerCell = buildOwnerDisplay({
      actors: [],
      principals: [],
      agents: [],
      actorsLoaded: true,
      agentsLoaded: true,
    });
    expect(ownerCell("actor-nobody")).toMatchObject({
      pending: false,
      name: "actor-nobody",
      isAgent: false,
    });
  });

  it("marks an actor the workspace tags as an agent", () => {
    const ownerCell = buildOwnerDisplay({
      actors: [{ id: "actor-leo", display_name: "Leo Park", tags: ["agent"] }],
      principals: [],
      agents: [],
      actorsLoaded: true,
      agentsLoaded: true,
    });
    expect(ownerCell("actor-leo")).toMatchObject({
      name: "Leo Park",
      isAgent: true,
    });
  });
});

describe("compactOwnerName", () => {
  it("drops the role, and the host for an agent", () => {
    expect(compactOwnerName("Maya Chen (Studio producer)")).toBe("Maya Chen");
    expect(compactOwnerName("priya on dev-host", { isAgent: true })).toBe(
      "priya",
    );
    expect(compactOwnerName("priya on dev-host")).toBe("priya on dev-host");
    expect(compactOwnerName("")).toBe("");
  });
});
