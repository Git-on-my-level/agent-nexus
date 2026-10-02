import { describe, expect, it, vi } from "vitest";
import {
  createParticipationReader,
  participationState,
  participationSummary,
  participationTasks,
  publicParticipant,
} from "../../src/lib/taskParticipation.js";
const NOW = Date.parse("2026-10-02T12:00:00Z");
const iso = (offset) => new Date(NOW + offset).toISOString();
const row = (id, overrides = {}) => ({
  participant_id: id,
  agent_id: "agent-1",
  actor_id: "actor-1",
  activity: "active",
  active: true,
  last_seen_at: iso(-30_000),
  expires_at: iso(90_000),
  ...overrides,
});

describe("task-scoped participation", () => {
  it("keeps concurrent sessions independent and drops private/provider fields", () => {
    const first = publicParticipant(
      row("p1", {
        session_id: "private-session",
        native_session_id: "/private/transcript",
        host_scope: "private-host",
        provider: "private-provider",
      }),
    );
    const second = publicParticipant(row("p2"));
    expect(JSON.stringify(first)).not.toMatch(
      /private|provider|session_id|host_scope/,
    );
    expect(
      participationSummary([{ participants: [first, second] }], NOW),
    ).toEqual({ participating: 2, active: 2, partial: false });
  });
  it("expires activity locally while retaining historical participation", () => {
    const participant = publicParticipant(row("p1"));
    expect(participationState(participant, NOW).active).toBe(true);
    expect(participationState(participant, NOW + 90_000)).toEqual({
      label: "Stale · activity unknown",
      active: false,
    });
    expect(
      participationSummary([{ participants: [participant] }], NOW + 90_000)
        .participating,
    ).toBe(1);
  });
  it.each([
    [{ activity: "idle", active: false }, "Idle"],
    [{ activity: "closed" }, "Session closed"],
    [{ activity: "left" }, "Left"],
    [{ activity: "stale" }, "Stale · activity unknown"],
    [{ activity: "future-private-activity" }, "Activity unknown"],
    [{ expires_at: "invalid" }, "Activity unknown"],
    [{ last_seen_at: null }, "Activity unknown"],
    [{ active: false }, "Not currently active"],
  ])("renders safe activity for %j", (overrides, label) => {
    expect(
      participationState(publicParticipant(row("p1", overrides)), NOW),
    ).toEqual({ label, active: false });
  });
  it("does not derive completion, ownership, or activity from a completed run", () => {
    const participant = publicParticipant(
      row("p1", { active: false, run: { state: "completed" }, owner: true }),
    );
    expect(participationState(participant, NOW).active).toBe(false);
    expect(participant).not.toHaveProperty("run");
    expect(participant).not.toHaveProperty("owner");
  });
  it("deduplicates task refs and bounds task fanout", () => {
    const tasks = Array.from({ length: 12 }, (_, n) => ({ ref: `card:${n}` }));
    expect(participationTasks([tasks[0], ...tasks, {}])).toHaveLength(8);
  });
  it("follows opaque cursors, coalesces requests and uses a short instance cache", async () => {
    const listWorkParticipants = vi.fn(async (_ref, query) => ({
      participants: [row(query.cursor ? "p2" : "p1")],
      next_cursor: query.cursor ? "" : "opaque+/=",
    }));
    let now = NOW;
    const read = createParticipationReader(
      { listWorkParticipants },
      { clock: () => now },
    );
    const tasks = [{ ref: "card:a" }];
    const [a, b] = await Promise.all([read(tasks), read(tasks)]);
    expect(a).toEqual(b);
    expect(a[0].participants).toHaveLength(2);
    expect(listWorkParticipants).toHaveBeenCalledTimes(2);
    expect(listWorkParticipants.mock.calls[1][1].cursor).toBe("opaque+/=");
    await read(tasks);
    expect(listWorkParticipants).toHaveBeenCalledTimes(2);
    now += 10_001;
    await read(tasks);
    expect(listWorkParticipants).toHaveBeenCalledTimes(4);
  });
  it("bounds concurrency and reports partial pagination instead of a complete count", async () => {
    let active = 0,
      peak = 0;
    const listWorkParticipants = vi.fn(async () => {
      active++;
      peak = Math.max(peak, active);
      await new Promise((resolve) => setTimeout(resolve, 1));
      active--;
      return { participants: [row("p1")], next_cursor: "repeated" };
    });
    const read = createParticipationReader({ listWorkParticipants });
    const result = await read(
      Array.from({ length: 20 }, (_, n) => ({ ref: `card:${n}` })),
    );
    expect(peak).toBeLessThanOrEqual(3);
    expect(result).toHaveLength(8);
    expect(listWorkParticipants).toHaveBeenCalledTimes(16);
    expect(result.every((group) => group.hasMore)).toBe(true);
  });
  it.each([401, 403, 404, 503])(
    "makes denied/unavailable reads indistinguishable without retaining prior active results (%s)",
    async (status) => {
      const listWorkParticipants = vi
        .fn()
        .mockResolvedValueOnce({ participants: [row("p1")], next_cursor: "" })
        .mockRejectedValueOnce(
          Object.assign(new Error("private /home/person/session"), { status }),
        );
      const read = createParticipationReader({ listWorkParticipants });
      const tasks = [{ ref: "card:a" }];
      await read(tasks);
      const result = await read(tasks, { force: true });
      expect(result[0]).toEqual({
        ref: "card:a",
        title: "",
        participants: [],
        hasMore: false,
        unavailable: true,
      });
      expect(JSON.stringify(result)).not.toMatch(/private|home|session/);
      expect(participationSummary(result, NOW)).toEqual({
        participating: 0,
        active: 0,
        partial: true,
      });
    },
  );
  it("stops queued tasks and cursor reads when the workspace reader is disposed", async () => {
    const releases = [];
    const listWorkParticipants = vi.fn(
      () => new Promise((resolve) => releases.push(resolve)),
    );
    const read = createParticipationReader({ listWorkParticipants });
    const pending = read(
      Array.from({ length: 8 }, (_, n) => ({ ref: `card:${n}` })),
    );
    expect(listWorkParticipants).toHaveBeenCalledTimes(3);
    read.dispose();
    for (const release of releases)
      release({
        participants: [row("p1")],
        next_cursor: "old-workspace-cursor",
      });
    const result = await pending;
    expect(listWorkParticipants).toHaveBeenCalledTimes(3);
    expect(
      result.every((group) => group.unavailable && !group.participants.length),
    ).toBe(true);
    expect(await read([{ ref: "card:another" }])).toEqual([]);
  });
  it("filters agent views after reading only supplied task refs", async () => {
    const listWorkParticipants = vi.fn(async () => ({
      participants: [row("p1"), row("p2", { agent_id: "other" })],
      next_cursor: "",
    }));
    const read = createParticipationReader({ listWorkParticipants });
    const groups = await read([{ ref: "card:a" }], { agentId: "agent-1" });
    expect(groups[0].participants.map((p) => p.id)).toEqual(["p1"]);
    expect(listWorkParticipants).toHaveBeenCalledTimes(1);
  });
});
