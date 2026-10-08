import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { indexResolvedRefs, refChipModel } from "../../src/lib/refResolve.js";
import { workSummaryCard } from "../../src/lib/workSummaryCards.js";
import { sinceYouLastLookedStrip } from "../../src/lib/sinceYouLastLooked.js";

/**
 * The initiative UI reads three backend shapes, and these are the serialized
 * ones core checks its own production output against. Importing them directly
 * is the point: a fixture invented here could drift from the wire without
 * anything failing, which is exactly how the card model ended up expecting a
 * string `health` when core had always sent `{status, reason}`.
 *
 * `contracts/fixtures/initiative-overview/README.md` says UI tests should do
 * this rather than invent card fields.
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
const card = (overrides = {}) =>
  workSummaryCard(
    { ...tile, ...overrides },
    { now: NOW, href: (ref) => `/w/${encodeURIComponent(ref)}` },
  );
const model = (overrides = {}) => card(overrides).summary;

describe("the Overview card reads core's serialized initiative", () => {
  it("renders the computed status core sends, with core's own label", () => {
    expect(tile.work_summary.status).toEqual({
      state: "on_track",
      label: "In progress",
      reason: "Open steps are progressing.",
      since: "2026-10-04T12:00:00Z",
    });
    expect(model().status).toMatchObject({
      state: "on_track",
      label: "In progress",
      reason: "Open steps are progressing.",
      tone: "ok",
    });
  });

  it("shows the stored phase core says disagrees with the computed status", () => {
    // The fixture's card sits in `backlog` with an untouched plan that core
    // computes as on track, so the card has to say both: "In progress ·
    // marked backlog" is exactly the disagreement this change exists to show.
    expect(tile.work_summary.set_status).toEqual({
      state: "backlog",
      label: "Backlog",
    });
    expect(model().setStatus).toMatchObject({
      state: "backlog",
      label: "Backlog",
    });
  });

  it("takes progress and its unit from the computed summary", () => {
    expect(model().progress).toMatchObject({
      done: tile.work_summary.progress.done,
      total: tile.work_summary.progress.total,
      unit: "steps",
      count: "1/2",
    });
  });

  it("takes the shape from the geometry the same row carries", () => {
    expect(card()).toMatchObject({});
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

  it("reports what the bounded geometry omitted", () => {
    expect(model().overflow).toBe(tile.geometry.collapsed_nodes);
    expect(
      model({ geometry: { ...tile.geometry, collapsed_nodes: 9 } }).overflow,
    ).toBe(9);
  });

  it("reads the step lists from the digest core computes", () => {
    // The fixture's plan has one inline-done step and one unstarted step, so
    // core sends an empty completed list: a step marked done inline has no
    // completion time to date it from. The card must say nothing about it
    // rather than invent one.
    expect(tile.work_summary.steps.window_hours).toBe(168);
    expect(model().steps.groups.map((list) => list.key)).toEqual(["next"]);
    expect(model().steps.next.items[0]).toMatchObject({
      id: "build",
      title: "Build",
      status: "not_started",
    });
    expect(model().steps.completed.items).toEqual([]);
  });

  it("names the next step core computed", () => {
    expect(model().next).toMatchObject({
      id: "build",
      title: "Build",
      more: 0,
    });
  });

  it("takes the age badge's instant from the computed movement", () => {
    expect(model().lastMovementAt).toBe(tile.work_summary.last_movement_at);
  });

  it("always marks a scoped reader's asks as sampled", () => {
    // Core sends `attention_truncated` to every scoped reader whether or not
    // asks were omitted, so a private candidate count cannot be inferred from
    // its absence. The client must carry that marker rather than drop it.
    expect(tile.work_summary.attention_truncated).toBe(true);
    expect(model().attentionTruncated).toBe(true);
    expect(model().attention).toBeNull();
  });

  it("degrades for a planless initiative without inventing a plan", () => {
    // Core sends no progress, next or steps, and a no_plan status.
    const planless = model({
      plan_state: null,
      geometry: null,
      work_summary: {
        status: {
          state: "no_plan",
          label: "No plan",
          reason: "Card has no plan steps.",
        },
      },
    });
    expect(planless.segments).toEqual([]);
    expect(planless.shape).toBe("");
    expect(planless.next).toBeNull();
    expect(planless.progress).toBeNull();
    expect(planless.status.state).toBe("no_plan");
    expect(
      card({
        plan_state: null,
        geometry: null,
        work_summary: { status: { state: "no_plan", label: "No plan" } },
      }).group,
    ).toBe("no_plan");
  });

  it("still reads the legacy fields from a core that computes no summary", () => {
    // Dropping `work_summary` is what an older core looks like on the wire.
    const legacyRow = { ...tile };
    delete legacyRow.work_summary;
    const legacy = workSummaryCard(legacyRow, { now: NOW }).summary;
    expect(legacy.status.state).toBe("on_track");
    expect(legacy.progress).toMatchObject({ done: 1, total: 2 });
    expect(legacy.next).toMatchObject({ id: "build", title: "Build" });
    expect(legacy.steps.groups.map((list) => list.key)).toEqual(["next"]);
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
      initiative_stale: 1,
      initiative_blocked: 1,
      ask_answered: 1,
    });
    expect(strip.summary).toBe(
      "1 step done · 1 blocked · 1 stale · 1 ask answered",
    );
  });

  it("leads with what needs attention", () => {
    expect(
      sinceYouLastLookedStrip(digest).items.map((item) => item.kind),
    ).toEqual([
      "initiative_blocked",
      "initiative_stale",
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
      "initiative_stale",
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
