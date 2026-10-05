import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { initiativeTileModel, miniViz } from "../../src/lib/initiativeTiles.js";
import { indexResolvedRefs, refChipModel } from "../../src/lib/refResolve.js";
import { sinceYouLastLookedStrip } from "../../src/lib/sinceYouLastLooked.js";

/**
 * The initiative UI reads three backend shapes, and these are the serialized
 * ones core checks its own production output against. Importing them directly
 * is the point: a fixture invented here could drift from the wire without
 * anything failing, which is exactly how the tile model ended up expecting a
 * string `health` when core had always sent `{status, reason}`.
 *
 * `contracts/fixtures/initiative-overview/README.md` says UI tests should do
 * this rather than invent tile fields.
 */
const contractFixture = (name) =>
  JSON.parse(
    readFileSync(
      fileURLToPath(
        new URL(
          `../../../contracts/fixtures/initiative-overview/${name}`,
          import.meta.url,
        ),
      ),
      "utf8",
    ),
  );

const tile = contractFixture("tile.json");
const refs = contractFixture("refs.json");
const digest = contractFixture("digest.json");

const NOW = Date.parse("2026-10-04T14:00:00Z");
const model = (overrides = {}) =>
  initiativeTileModel(
    { ...tile, ...overrides },
    { now: NOW, href: (ref) => `/w/${encodeURIComponent(ref)}` },
  );

describe("the Overview tile reads core's serialized initiative", () => {
  it("takes health from the object core sends, not a string", () => {
    expect(tile.health).toEqual({
      status: "on_track",
      reason: "Work is progressing.",
    });
    expect(model().health).toMatchObject({
      state: "on_track",
      label: "On track",
      short: "On track",
      tone: "ok",
      reason: "Work is progressing.",
      known: true,
    });
  });

  it("prefers the computed plan_health field once core sends it", () => {
    // The parallel core change adds `plan_health {state, reason, since}`; the
    // tile has to read it in preference to the older status field.
    expect(
      model({
        plan_health: {
          state: "at_risk",
          reason: "two steps slipped",
          since: "2026-10-02T00:00:00Z",
        },
      }).health,
    ).toMatchObject({
      state: "at_risk",
      reason: "two steps slipped",
      since: "2026-10-02T00:00:00Z",
    });
  });

  it("takes progress from the plan, matching the summary core also sends", () => {
    expect(model().progress).toEqual(tile.plan_state.progress);
    expect(model().progress).toEqual(tile.progress);
  });

  it("takes the shape from the geometry", () => {
    expect(model()).toMatchObject({ shape: "chain", shapeLabel: "Timeline" });
  });

  it("draws a segment per geometry node, marking the critical path", () => {
    const { segments } = model();
    expect(segments.map((segment) => segment.id)).toEqual(
      tile.geometry.nodes.map((node) => node.id),
    );
    expect(segments.map((segment) => segment.status)).toEqual([
      "done",
      "not_started",
    ]);
    expect(segments.map((segment) => segment.onCriticalPath)).toEqual([
      false,
      true,
    ]);
  });

  it("carries the layers and edges core computed, for the tree", () => {
    const { segments } = model();
    expect(segments.map((segment) => segment.layer)).toEqual([0, 1]);
    expect(segments[1].after).toEqual(["design"]);
  });

  it("reports what the bounded geometry omitted", () => {
    expect(model().overflow).toBe(tile.geometry.collapsed_nodes);
    expect(
      model({
        geometry: { ...tile.geometry, collapsed_nodes: 9 },
      }).overflow,
    ).toBe(9);
  });

  it("names the next step from the id core sends", () => {
    expect(model().next).toMatchObject({ id: "build", title: "Build" });
  });

  it("prefers the computed next_step, with the title core gives it", () => {
    expect(
      model({
        next_step: { id: "build", title: "Build the thing", ref: "card:build" },
      }).next,
    ).toMatchObject({ title: "Build the thing", ref: "card:build" });
  });

  it("prefers the plan's last movement for the age badge", () => {
    expect(model().movedAt).toBe(tile.plan_state.last_movement_at);
  });

  it("degrades for a planless initiative without inventing a plan", () => {
    // Core sends null plan_state and geometry, and a health reason from the
    // native phase; the tile still has to render.
    const planless = model({ plan_state: null, geometry: null });
    expect(planless.hasPlan).toBe(false);
    expect(planless.segments).toEqual([]);
    expect(planless.shape).toBe("");
    expect(planless.next).toBeNull();
    expect(planless.health.state).toBe("no_plan");
    expect(planless.group).toBe("no_plan");
    // The summary progress core preserves is still shown.
    expect(planless.progress).toEqual(tile.progress);
  });
});

describe("the mini-viz follows the shape core computed", () => {
  const segments = (shape, nodes) =>
    model({ geometry: { ...tile.geometry, shape, nodes } });

  it("lays a chain out as one track", () => {
    expect(model().viz).toMatchObject({ kind: "track" });
    expect(model().viz.tracks).toHaveLength(1);
  });

  it("lays a tree out as a column per dependency layer", () => {
    const built = segments("dag", [
      { id: "a", status: "done", layer: 0, after: [] },
      { id: "b", status: "active", layer: 1, after: ["a"] },
      { id: "c", status: "not_started", layer: 1, after: ["a"] },
      { id: "d", status: "not_started", layer: 2, after: ["b", "c"] },
    ]);
    expect(built.viz.kind).toBe("tree");
    expect(built.viz.tracks.map((track) => track.length)).toEqual([1, 2, 1]);
  });

  it("lays lanes out as a track per independent run", () => {
    const built = segments("lanes", [
      { id: "a", status: "done", layer: 0, after: [] },
      { id: "b", status: "active", layer: 1, after: ["a"] },
      { id: "x", status: "not_started", layer: 0, after: [] },
      { id: "y", status: "not_started", layer: 1, after: ["x"] },
    ]);
    expect(built.viz.kind).toBe("lanes");
    expect(built.viz.tracks.map((track) => track.map((s) => s.id))).toEqual([
      ["a", "b"],
      ["x", "y"],
    ]);
  });

  it("returns nothing to draw for a plan with no nodes", () => {
    expect(miniViz({ segments: [] }, "dag")).toEqual({
      kind: "track",
      tracks: [],
    });
  });
});

describe("the ref preview reads core's serialized resolve response", () => {
  const resolved = indexResolvedRefs(refs);
  const context = { organizationSlug: "scaling", workspaceSlug: "anx" };
  const chip = (ref) => refChipModel(ref, resolved, context);

  it("shows the owner's name, not the actor ref", () => {
    expect(refs.items[0].owner).toBe("actor:actor-1");
    expect(chip("card:initiative").owner).toBe("Actor 1");
  });

  it("reads board and next step out of their objects", () => {
    expect(refs.items[0].board).toEqual({
      ref: "board:portfolio",
      title: "Portfolio",
    });
    expect(chip("card:initiative")).toMatchObject({
      board: "Portfolio",
      nextStep: "Build",
    });
  });

  it("carries priority, progress and last movement", () => {
    expect(chip("card:initiative")).toMatchObject({
      priority: "p1",
      progress: { done: 0, total: 1 },
      lastMovedAt: "2026-10-04T12:00:00Z",
    });
  });

  it("rebases the workspace-relative url core sends", () => {
    expect(refs.items[0].url).toBe("/tasks/initiative");
    expect(chip("card:initiative").href).toBe(
      "/o/scaling/w/anx/tasks/initiative",
    );
  });

  it("keeps the unresolvable row as a not-found chip", () => {
    expect(chip("card:unknown")).toMatchObject({
      resolvable: false,
      href: "",
    });
  });
});

describe("Since you last looked reads core's digest", () => {
  it("summarises every kind the digest carries", () => {
    const strip = sinceYouLastLookedStrip(digest);
    expect(strip.since).toBe(digest.since);
    expect(strip.counts).toEqual({
      step_completed: 1,
      initiative_stalled: 1,
      initiative_blocked: 1,
      ask_answered: 1,
    });
    expect(strip.summary).toBe(
      "1 step done · 1 blocked · 1 stalled · 1 ask answered",
    );
  });

  it("leads with what needs attention", () => {
    expect(
      sinceYouLastLookedStrip(digest).items.map((item) => item.kind),
    ).toEqual([
      "initiative_blocked",
      "initiative_stalled",
      "ask_answered",
      "step_completed",
    ]);
  });

  it("renders nothing on a first visit", () => {
    // Core sends a null `since` and no items before a baseline exists.
    expect(
      sinceYouLastLookedStrip({ since: null, items: [], truncated: false }),
    ).toBeNull();
  });

  it("renders nothing on a quiet visit", () => {
    expect(sinceYouLastLookedStrip({ ...digest, items: [] })).toBeNull();
  });

  it("reports the server's truncation", () => {
    expect(
      sinceYouLastLookedStrip({ ...digest, truncated: true }).truncated,
    ).toBe(true);
  });

  it("caps its own list and counts the remainder", () => {
    const many = {
      ...digest,
      items: Array.from({ length: 10 }, (_, index) => ({
        kind: "step_completed",
        ref: `card:${index}`,
        title: `Step ${index}`,
        step_id: `s${index}`,
      })),
    };
    const strip = sinceYouLastLookedStrip(many, { limit: 4 });
    expect(strip.items).toHaveLength(4);
    expect(strip.overflow).toBe(6);
  });

  it("ignores a kind it does not know rather than rendering a blank row", () => {
    expect(
      sinceYouLastLookedStrip({
        ...digest,
        items: [{ kind: "something_new", ref: "card:a", title: "A" }],
      }),
    ).toBeNull();
  });
});

describe("the digest's decision items", () => {
  it("renders the contract's decision kind", () => {
    // The contract enum is `decision_created`. An earlier guess at
    // `decision_recorded` filtered every decision out silently, so a
    // decision-only digest rendered no strip at all.
    const strip = sinceYouLastLookedStrip({
      since: "2026-10-04T12:00:00Z",
      generated_at: "2026-10-04T14:00:00Z",
      items: [
        {
          kind: "decision_created",
          ref: "decision:launch",
          title: "New decision for Launch",
          ts: "2026-10-04T13:00:00Z",
        },
      ],
      truncated: false,
    });
    expect(strip).not.toBeNull();
    expect(strip.items[0].kind).toBe("decision_created");
    expect(strip.summary).toBe("1 new decision");
  });

  it("matches the kinds the OpenAPI enum declares", () => {
    // Guards the mismatch class rather than the one instance of it.
    const enumerated = [
      "step_completed",
      "initiative_stalled",
      "initiative_blocked",
      "ask_answered",
      "decision_created",
    ];
    for (const kind of enumerated) {
      const strip = sinceYouLastLookedStrip({
        since: "2026-10-04T12:00:00Z",
        generated_at: "2026-10-04T14:00:00Z",
        items: [{ kind, ref: "card:a", title: "A" }],
        truncated: false,
      });
      expect(strip, `${kind} should render`).not.toBeNull();
    }
  });
});
