// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/svelte";
import { afterEach, describe, expect, it, vi } from "vitest";
import { tick } from "svelte";
import { authenticatedAgent } from "../../src/lib/authSession.js";
import TaskParticipation from "../../src/lib/components/participation/TaskParticipation.svelte";
import EvidenceHandoff from "../../src/lib/components/participation/EvidenceHandoff.svelte";
const tasks = [{ ref: "card:one", title: "One task" }];
const participant = (id, overrides = {}) => ({
  participant_id: id,
  agent_id: "agent-one",
  actor_id: "actor-one",
  activity: "active",
  active: true,
  last_seen_at: new Date().toISOString(),
  expires_at: new Date(Date.now() + 90_000).toISOString(),
  ...overrides,
});
const clientFor = (participants) => ({
  listWorkParticipants: vi.fn(async () => ({ participants, next_cursor: "" })),
});
afterEach(() => {
  cleanup();
  authenticatedAgent.set(null);
  vi.useRealTimers();
});

describe("participation panel", () => {
  it("renders concurrent sessions without exposing even owner-visible private fields", async () => {
    const client = clientFor([
      participant("p1", {
        session_id: "SECRET",
        native_session_id: "/private/log.json",
        provider: "SECRET_PROVIDER",
      }),
      participant("p2"),
    ]);
    const { container } = render(TaskParticipation, { tasks, client });
    await screen.findByText("2 sessions participating · 2 currently active");
    expect(screen.getByText("Session 1")).toBeTruthy();
    expect(screen.getByText("Session 2")).toBeTruthy();
    expect(screen.getAllByText("Currently active")).toHaveLength(2);
    expect(container.innerHTML).not.toMatch(/SECRET|\/private\/log/);
    expect(screen.getAllByText(/Agent-reported activity/)).toHaveLength(2);
  });
  it("shows stale, unknown and closed records without falsely showing offline or completed", async () => {
    const client = clientFor([
      participant("p1", {
        expires_at: new Date(Date.now() - 1_000).toISOString(),
      }),
      participant("p2", { activity: "new-activity" }),
      participant("p3", { activity: "closed", run: { state: "completed" } }),
    ]);
    render(TaskParticipation, { tasks, client });
    await screen.findByText("2 sessions participating · 0 currently active");
    expect(screen.getByText("Stale · activity unknown")).toBeTruthy();
    expect(screen.getByText("Activity unknown")).toBeTruthy();
    expect(screen.getByText("Session closed")).toBeTruthy();
    expect(screen.queryByText("Offline")).toBeNull();
    expect(screen.queryByText("Completed")).toBeNull();
  });
  it("does not label denied task existence or render raw error bodies", async () => {
    const client = {
      listWorkParticipants: vi
        .fn()
        .mockRejectedValue({ status: 404, message: "private session:SECRET" }),
    };
    const { container } = render(TaskParticipation, {
      tasks,
      client,
      agentId: "agent-one",
    });
    await screen.findByText(/Participation unavailable/);
    expect(screen.queryByText("One task")).toBeNull();
    expect(screen.queryByText(/0 participating/)).toBeNull();
    expect(container.innerHTML).not.toContain("SECRET");
  });
  it("labels limited agent coverage, including empty and partial results", async () => {
    const client = clientFor([]);
    render(TaskParticipation, { tasks, client, agentId: "agent-one" });
    await screen.findByText(
      "No shared participation reported for this agent on this task.",
    );
    expect(
      screen.getByText(/Other tasks and private sessions are not included/),
    ).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "One task" }).getAttribute("href"),
    ).toBe("/tasks/card%3Aone");
  });
  it("clears an old task during navigation and ignores its late response", async () => {
    let resolveOld;
    const client = {
      listWorkParticipants: vi.fn((ref) =>
        ref === "card:one"
          ? new Promise((resolve) => {
              resolveOld = resolve;
            })
          : Promise.resolve({
              participants: [participant("new")],
              next_cursor: "",
            }),
      ),
    };
    const { rerender } = render(TaskParticipation, { tasks, client });
    await waitFor(() =>
      expect(client.listWorkParticipants).toHaveBeenCalledTimes(1),
    );
    await rerender({ tasks: [{ ref: "card:two" }] });
    await screen.findByText("1 session participating · 1 currently active");
    resolveOld({
      participants: [participant("old1"), participant("old2")],
      next_cursor: "",
    });
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(
      screen.queryByText("2 sessions participating · 2 currently active"),
    ).toBeNull();
  });
  it("expires active rows without a network response", async () => {
    vi.useFakeTimers();
    const now = Date.now();
    const client = clientFor([
      participant("p1", { expires_at: new Date(now + 2_000).toISOString() }),
    ]);
    render(TaskParticipation, { tasks, client });
    await vi.advanceTimersByTimeAsync(0);
    await tick();
    expect(
      screen.getByText("1 session participating · 1 currently active"),
    ).toBeTruthy();
    await vi.advanceTimersByTimeAsync(5_000);
    await tick();
    expect(screen.getByText("Stale · activity unknown")).toBeTruthy();
    expect(
      screen.getByText("1 session participating · 0 currently active"),
    ).toBeTruthy();
    expect(client.listWorkParticipants).toHaveBeenCalledTimes(1);
  });
  it("does not reuse cached records across workspace or authenticated-actor changes", async () => {
    const client = clientFor([participant("p1")]);
    const { rerender } = render(TaskParticipation, {
      tasks,
      client,
      workspaceHref: (path) => `/first${path}`,
    });
    await screen.findByText("1 session participating · 1 currently active");
    client.listWorkParticipants.mockResolvedValue({
      participants: [],
      next_cursor: "",
    });
    await rerender({ workspaceHref: (path) => `/second${path}` });
    await screen.findByText("No shared participation reported.");
    expect(client.listWorkParticipants).toHaveBeenCalledTimes(2);
    client.listWorkParticipants.mockResolvedValue({
      participants: [participant("p2")],
      next_cursor: "",
    });
    authenticatedAgent.set({ actor_id: "different-human" });
    await screen.findByText("1 session participating · 1 currently active");
    expect(client.listWorkParticipants).toHaveBeenCalledTimes(3);
  });
  it("disposes pending pagination and queued tasks before a workspace switch", async () => {
    let scope = "first";
    const releases = [];
    const calls = [];
    const client = {
      listWorkParticipants: vi.fn((ref, query) => {
        calls.push({ scope, ref, query });
        if (scope === "first")
          return new Promise((resolve) => releases.push(resolve));
        return Promise.resolve({ participants: [], next_cursor: "" });
      }),
    };
    const manyTasks = Array.from({ length: 8 }, (_, n) => ({
      ref: `card:${n}`,
    }));
    const { rerender } = render(TaskParticipation, {
      tasks: manyTasks,
      client,
      workspaceHref: (path) => `/first${path}`,
    });
    await waitFor(() => expect(calls).toHaveLength(3));
    scope = "second";
    await rerender({ workspaceHref: (path) => `/second${path}` });
    await screen.findByText("0 sessions participating · 0 currently active");
    for (const release of releases)
      release({
        participants: [participant("old")],
        next_cursor: "private-old-scope-cursor",
      });
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(calls.filter((call) => call.scope === "second")).toHaveLength(8);
    expect(calls.every((call) => !call.query.cursor)).toBe(true);
    expect(screen.queryByText("Currently active")).toBeNull();
  });
  it("counts cross-task agent participation without claiming unique sessions", async () => {
    const client = clientFor([participant("p1")]);
    render(TaskParticipation, {
      tasks: [tasks[0], { ref: "card:two", title: "Second task" }],
      client,
      agentId: "agent-one",
    });
    await screen.findByText("2 task participations · 2 currently active");
    expect(screen.getByText(/Sessions are counted per task/)).toBeTruthy();
  });
  it("coalesces repeated refresh clicks and recovers after an unavailable read", async () => {
    const client = {
      listWorkParticipants: vi
        .fn()
        .mockRejectedValueOnce(new Error("unavailable"))
        .mockResolvedValue({
          participants: [participant("p1")],
          next_cursor: "",
        }),
    };
    render(TaskParticipation, { tasks, client });
    await screen.findByText(/Participation unavailable/);
    await fireEvent.click(
      screen.getByRole("button", { name: "Refresh activity" }),
    );
    await screen.findByText("1 session participating · 1 currently active");
    expect(client.listWorkParticipants).toHaveBeenCalledTimes(2);
  });
});

describe("evidence for handoff", () => {
  it("keeps a claimed verified status reported unless verification establishes it", () => {
    render(EvidenceHandoff, {
      observations: [
        {
          actor_id: "actor-one",
          status: "verified",
          verification: "reported",
          observed_at: new Date().toISOString(),
          evidence: [
            { url: "https://example.com/evidence" },
            { url: "file:///private/session" },
          ],
        },
      ],
    });
    expect(screen.getByText("Reported claim")).toBeTruthy();
    expect(screen.getByText(/1 linked evidence item/)).toBeTruthy();
    expect(screen.queryByText("Verified evidence")).toBeNull();
  });
  it("clearly distinguishes an earlier report after the newest read fails", () => {
    render(EvidenceHandoff, {
      observations: [
        { status: "error" },
        { status: "uncertain", uncertainty: ["Not reviewed"], evidence: [] },
      ],
    });
    expect(screen.getByText("Uncertain report")).toBeTruthy();
    expect(
      screen.getByText("The latest read failed. This is an earlier report."),
    ).toBeTruthy();
    expect(screen.getByText("Attribution unavailable")).toBeTruthy();
  });
  it("does not treat an empty evidence history as a completed handoff", () => {
    render(EvidenceHandoff);
    expect(
      screen.getByText(
        /Participation and completed runs do not establish task completion/,
      ),
    ).toBeTruthy();
  });
});
