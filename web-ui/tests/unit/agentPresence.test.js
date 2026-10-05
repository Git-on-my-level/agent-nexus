import { describe, expect, it } from "vitest";

import {
  agentActivity,
  agentPath,
  agentRowModel,
  agentRuntimeLabel,
  groupAgentsByState,
  inboxAskPath,
  rosterSummary,
  runDuration,
  runLabel,
  titleFromCardRef,
  workingAgentCount,
} from "../../src/lib/agentPresence.js";

const NOW = Date.parse("2026-09-27T12:00:00Z");
const ago = (minutes) => new Date(NOW - minutes * 60_000).toISOString();

function agent(name, state, overrides = {}) {
  return {
    id: `agent-${name}`,
    actor_id: `actor-${name}`,
    host_id: "host-1",
    host_slug: "workstation-a",
    name,
    handle: `${name}.workstation-a`,
    display_name: `${name} on workstation-a`,
    identity_kind: "derived",
    state,
    bridge_online: false,
    current_card_ref: null,
    current_card_title: null,
    last_progress_note: null,
    last_progress_at: null,
    active_run: null,
    open_asks_count: 0,
    last_signal_at: ago(5),
    revoked_at: null,
    ...overrides,
  };
}

describe("agent roster model", () => {
  it("groups by state in core precedence and drops revoked agents", () => {
    const groups = groupAgentsByState([
      agent("a", "stale"),
      agent("b", "working"),
      agent("c", "waiting_on_human"),
      agent("d", "idle", { revoked_at: ago(1) }),
    ]);
    expect(groups.map((group) => group.key)).toEqual([
      "waiting_on_human",
      "working",
      "stale",
    ]);
    expect(groups[0].label).toBe("Waiting on you");
  });

  it("orders working agents by run time, then waiting agents by ask age", () => {
    const [working] = groupAgentsByState([
      agent("short", "working", {
        active_run: { run_id: "1", adapter: "codex", duration_seconds: 60 },
      }),
      agent("long", "working", {
        active_run: { run_id: "2", adapter: "codex", duration_seconds: 900 },
      }),
    ]);
    expect(working.agents.map((a) => a.name)).toEqual(["long", "short"]);

    const ask = (minutes) => ({
      id: `evt-${minutes}`,
      inbox_item_id: `inbox:ask:t:evt-${minutes}`,
      title: "Ask",
      severity: null,
      created_at: ago(minutes),
    });
    const [waiting] = groupAgentsByState([
      agent("new", "waiting_on_human", { waiting_ask: ask(5) }),
      agent("old", "waiting_on_human", { waiting_ask: ask(90) }),
    ]);
    expect(waiting.agents.map((a) => a.name)).toEqual(["old", "new"]);
  });

  it("describes a waiting agent by its oldest open ask and links to the Inbox", () => {
    const model = agentRowModel(
      agent("omar", "waiting_on_human", {
        open_asks_count: 2,
        waiting_ask: {
          id: "e1",
          inbox_item_id: "inbox:ask:t:e1",
          title: "Confirm 20-minute quest path",
          kind: "ask",
          severity: "high",
          subject_title: "Lock hub quest path",
          created_at: ago(192),
        },
      }),
      { now: NOW },
    );
    expect(model.ask.title).toBe("Confirm 20-minute quest path");
    expect(model.ask.severity).toBe("high");
    expect(model.ask.subjectTitle).toBe("Lock hub quest path");
    expect(model.ask.href).toBe(
      "/inbox?mailbox=needs-you&item=inbox%3Aask%3At%3Ae1",
    );
    expect(model.duration).toBe("3h 12m");
    expect(model.moreAsks).toBe(1);
  });

  it("links to Needs you while the inbox projection catches up", () => {
    expect(inboxAskPath({ id: "e1", inbox_item_id: null })).toBe(
      "/inbox?mailbox=needs-you",
    );
  });

  it("describes a working agent by task, quoted note and run time", () => {
    const model = agentRowModel(
      agent("leo", "working", {
        current_card_ref: "card:tune-combat",
        current_card_title: "Tune core combat loop",
        last_progress_note: "Parry window at 120 ms",
        last_progress_at: ago(6),
        active_run: {
          run_id: "r1",
          adapter: "codex",
          model: "sol",
          duration_seconds: 840,
        },
      }),
      // Read two minutes ago: run time counts forward from core's value.
      { now: NOW, loadedAt: NOW - 120_000 },
    );
    expect(model.task).toEqual({
      title: "Tune core combat loop",
      ref: "card:tune-combat",
    });
    expect(model.note).toMatchObject({
      text: "Parry window at 120 ms",
      age: "6m",
    });
    expect(model.duration).toBe("16m");
    expect(model.durationTitle).toBe("Run time");
  });

  it("says when a stale agent never checked in", () => {
    expect(
      agentRowModel(agent("pm", "stale", { last_signal_at: null }), {
        now: NOW,
      }).headline,
    ).toBe("Never checked in");
    expect(
      agentRowModel(agent("pm", "stale", { last_signal_at: ago(60 * 50) }), {
        now: NOW,
      }).headline,
    ).toBe("No signal for 2d 2h");
  });

  it("names the runtime from the active run or an adapter name only", () => {
    expect(
      agentRuntimeLabel(
        agent("leo", "working", {
          active_run: { adapter: "codex", model: "sol", duration_seconds: 1 },
        }),
      ),
    ).toBe("codex sol");
    expect(agentRuntimeLabel(agent("claude", "idle"))).toBe("claude");
    expect(agentRuntimeLabel(agent("reviewer", "idle"))).toBe("");
  });

  it("counts working agents for the nav badge", () => {
    const agents = [
      agent("a", "working"),
      agent("b", "working", { revoked_at: ago(1) }),
      agent("c", "waiting_on_human"),
    ];
    expect(workingAgentCount(agents)).toBe(1);
    expect(rosterSummary(agents)).toMatchObject({
      total: 2,
      working: 1,
      waiting_on_human: 1,
    });
  });

  it("links agents by handle and runs by launcher id", () => {
    expect(agentPath(agent("codex", "idle"))).toBe(
      "/agents/codex.workstation-a",
    );
    expect(runLabel({ id: "uuid", external_id: "exec-42" })).toBe("exec-42");
    expect(runDuration({ started_at: ago(71), ended_at: null }, NOW)).toBe(
      "1h 11m",
    );
  });

  it("merges notes and messages newest first with card titles", () => {
    const entries = agentActivity({
      notes: [{ text: "note", at: ago(10), card_ref: "card:a" }],
      messages: [
        {
          id: "e1",
          ts: ago(2),
          payload: { text: "message", subject_ref: "card:fix-the-thing" },
          run_attribution: { run_id: "r1" },
        },
        { id: "e2", ts: ago(1), payload: { text: "  " } },
      ],
    });
    expect(entries.map((entry) => entry.kind)).toEqual(["message", "note"]);
    expect(entries[0].runAttribution).toEqual({ run_id: "r1" });
    expect(titleFromCardRef("card:fix-the-thing")).toBe("Fix the thing");
    expect(
      titleFromCardRef("card:a", new Map([["card:a", "Known title"]])),
    ).toBe("Known title");
  });
});
