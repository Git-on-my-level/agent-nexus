import { describe, expect, it } from "vitest";

import { hasArrivedAgent, isColdStart } from "../../src/lib/firstRun.js";
import { loadOverview } from "../../src/lib/overview.js";

/** The projection a workspace nothing has used produces. */
function coldModel(overrides = {}) {
  return {
    agents: { status: "ok", known: 0, working: 0, waiting: 0, offline: 0 },
    work: { status: "ok", total: 0 },
    initiatives: { status: "ok", count: 0, items: [] },
    needsYou: { status: "ok", count: 0, rows: [] },
    ...overrides,
  };
}

describe("cold start", () => {
  it("is the state of a workspace nothing has reported into", () => {
    expect(isColdStart(coldModel())).toBe(true);
  });

  it("is not claimed before the snapshot has loaded", () => {
    expect(isColdStart(null)).toBe(false);
    expect(isColdStart(undefined)).toBe(false);
  });

  it("ends at the first agent, including one that has never run", () => {
    // `known` counts the identities the four state counts leave out, which is
    // why it is projected at all: an enrolled host nobody has run yet is
    // somebody who already got through setup.
    expect(
      isColdStart(
        coldModel({
          agents: {
            status: "ok",
            known: 1,
            working: 0,
            waiting: 0,
            offline: 0,
          },
        }),
      ),
    ).toBe(false);
  });

  it("ends at the first task, initiative or ask", () => {
    expect(isColdStart(coldModel({ work: { status: "ok", total: 1 } }))).toBe(
      false,
    );
    expect(
      isColdStart(
        coldModel({ initiatives: { status: "ok", count: 1, items: [{}] } }),
      ),
    ).toBe(false);
    expect(
      isColdStart(
        coldModel({ needsYou: { status: "ok", count: 1, rows: [] } }),
      ),
    ).toBe(false);
  });

  it("is not an arrival: that requires a readable roster row", () => {
    expect(hasArrivedAgent(coldModel())).toBe(false);
    expect(hasArrivedAgent(null)).toBe(false);
    expect(
      hasArrivedAgent(
        coldModel({
          agents: {
            status: "unavailable",
            message: "nope",
          },
        }),
      ),
    ).toBe(false);
    expect(
      hasArrivedAgent(coldModel({ work: { status: "ok", total: 1 } })),
    ).toBe(false);
    expect(
      hasArrivedAgent(
        coldModel({
          agents: {
            status: "ok",
            known: 1,
            working: 0,
            waiting: 0,
            offline: 0,
          },
        }),
      ),
    ).toBe(true);
  });

  it("refuses to read a failed section as emptiness", () => {
    // A workspace full of work whose roster read failed must not be greeted
    // with "connect your first agent".
    for (const section of ["agents", "work", "initiatives", "needsYou"]) {
      expect(
        isColdStart(
          coldModel({ [section]: { status: "unavailable", message: "nope" } }),
        ),
        section,
      ).toBe(false);
    }
  });
});

describe("the agent count cold start reads", () => {
  it("comes out of the Overview snapshot the page already loaded", async () => {
    const model = await loadOverview({
      getOverview: async () => ({
        work: { status: "ok", items: [], total: 0, human_count: 0 },
        initiatives: { status: "ok", count: 0, items: [] },
        needs_you: { status: "ok", rows: [], count: 0 },
        dashboard: { status: "ok", reports: [] },
        agents: { status: "ok", items: [] },
      }),
    });
    expect(model.agents.known).toBe(0);
    expect(isColdStart(model)).toBe(true);
  });

  it("counts an identity that has never checked in", async () => {
    const model = await loadOverview({
      getOverview: async () => ({
        work: { status: "ok", items: [], total: 0, human_count: 0 },
        initiatives: { status: "ok", count: 0, items: [] },
        needs_you: { status: "ok", rows: [], count: 0 },
        dashboard: { status: "ok", reports: [] },
        // No `last_signal_at`: enrolled, never run. Every state count is
        // still zero, so `known` is the only thing that can see it.
        agents: { status: "ok", items: [{ id: "agent-1" }] },
      }),
    });
    expect(model.agents.working).toBe(0);
    expect(model.agents.known).toBe(1);
    expect(isColdStart(model)).toBe(false);
  });
});
