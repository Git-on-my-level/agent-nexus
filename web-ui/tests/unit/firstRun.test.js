import { describe, expect, it } from "vitest";

import {
  hasArrivedAgent,
  isColdStart,
  rosterHasAgent,
} from "../../src/lib/firstRun.js";
import { loadOverview } from "../../src/lib/overview.js";

/** The projection a workspace nothing has used produces. */
function coldModel(overrides = {}) {
  return {
    agents: { status: "ok", known: 0, working: 0, waiting: 0, offline: 0 },
    work: { status: "ok", total: 0 },
    initiatives: { status: "ok", count: 0, items: [] },
    needsYou: { status: "ok", count: 0, rows: [] },
    reports: { status: "ok", pinned_ref: null, reports: [] },
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

  it("ends at a dashboard somebody pinned, or a report to pin", () => {
    // An agent wrote the report and a reader pinned it. Either half is this
    // workspace having been used, even after every agent was revoked.
    expect(
      isColdStart(
        coldModel({
          reports: {
            status: "ok",
            pinned_ref: "document:dashboard",
            reports: [],
          },
        }),
      ),
    ).toBe(false);
    expect(
      isColdStart(
        coldModel({
          reports: { status: "ok", pinned_ref: null, reports: [{ id: "r1" }] },
        }),
      ),
    ).toBe(false);
  });

  it("refuses to conclude zero from a section core cut short", () => {
    // `truncated` means core stopped before the end: the zero beside it
    // counts the window it read, not the workspace.
    expect(
      isColdStart(
        coldModel({
          agents: { status: "ok", known: 0, truncated: true, working: 0 },
        }),
      ),
    ).toBe(false);
    expect(
      isColdStart(
        coldModel({ work: { status: "ok", total: 0, truncated: true } }),
      ),
    ).toBe(false);
  });

  it("refuses to read a failed section as emptiness", () => {
    // A workspace full of work whose roster read failed must not be greeted
    // with "connect your first agent".
    for (const section of [
      "agents",
      "work",
      "initiatives",
      "needsYou",
      "reports",
    ]) {
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

describe("the live arrival signal", () => {
  /*
   * Core publishes a new host and a newly named agent only to the ephemeral
   * roster hub (`agents_changed` on `GET /stream/agents`) and appends nothing
   * to the workspace event log, so the roster store — not this page's event
   * subscription — is what can end the waiting state on a visible tab.
   */
  it("fires on the first non-revoked agent in this workspace", () => {
    const roster = (agents) => ({ workspace: "ops", status: "ready", agents });
    expect(rosterHasAgent(roster([]), "ops")).toBe(false);
    expect(rosterHasAgent(roster([{ id: "a" }]), "ops")).toBe(true);
  });

  it("ignores a revoked identity", () => {
    // The only agent having been revoked puts the workspace back to needing
    // one, which is what `rosterSummary` counts too.
    expect(
      rosterHasAgent(
        {
          workspace: "ops",
          status: "ready",
          agents: [{ id: "a", revoked_at: "2026-10-01T00:00:00Z" }],
        },
        "ops",
      ),
    ).toBe(false);
  });

  it("ignores a roster that is another workspace's, or not loaded yet", () => {
    const agents = [{ id: "a" }];
    expect(
      rosterHasAgent({ workspace: "other", status: "ready", agents }, "ops"),
    ).toBe(false);
    expect(
      rosterHasAgent({ workspace: "ops", status: "idle", agents }, "ops"),
    ).toBe(false);
    expect(
      rosterHasAgent({ workspace: "ops", status: "error", agents }, "ops"),
    ).toBe(false);
    expect(rosterHasAgent(undefined, "ops")).toBe(false);
  });
});
