// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { clearPrincipalLocalWork } from "../../src/lib/localWorkStore.js";
import { readerScope } from "../../src/lib/readerScope.js";
const state = vi.hoisted(() => {
  let value = {
    url: new URL("http://localhost/o/local/w/local/initiatives"),
    params: { organization: "local", workspace: "local" },
  };
  const listeners = new Set();
  return {
    subscribe(fn) {
      listeners.add(fn);
      fn(value);
      return () => listeners.delete(fn);
    },
    route(path, params = {}) {
      value = {
        url: new URL(`http://localhost/o/local/w/local${path}`),
        params: { organization: "local", workspace: "local", ...params },
      };
      for (const fn of listeners) fn(value);
    },
  };
});
const client = vi.hoisted(() =>
  Object.fromEntries(
    [
      "listWork",
      "getWork",
      "getCardPlan",
      "listWorkObservations",
      "listWorkParticipants",
      "requestWorkRefresh",
      "getWorkCapabilities",
      "listBoards",
      "createWork",
      "listPmConversations",
      "createPmConversation",
      "getPmConversation",
      "sendPmMessage",
      "listPmDecisions",
      "getPmDecision",
      "listPmActions",
      "getPmAction",
      "answerPmDecision",
      "dispatchPmDecision",
      "reconcilePmAction",
      "createPmDecision",
      "moveBoardCard",
      "archiveCard",
      "listInboxItems",
      "getHomeUnread",
      "respondInboxItem",
      "markHomeRead",
      "streamEvents",
      "listEvents",
    ].map((key) => [key, vi.fn()]),
  ),
);
const navigation = vi.hoisted(() => ({ goto: vi.fn(), guards: [] }));
/** Where each captured sender was bound, newest last. */
const capturedScopes = vi.hoisted(() => []);
vi.mock("$app/stores", () => ({ page: { subscribe: state.subscribe } }));
vi.mock("$lib/coreClient", () => ({
  coreClient: client,
  createLocalMutationClient: () => client,
  createInboxSourceClient: () => client,
  // The sender is bound to one workspace; record which, so a test can tell
  // where an answer was actually sent.
  captureInboxResponseSender: () => {
    capturedScopes.push([
      getStore(currentOrganizationSlug),
      getStore(currentWorkspaceSlug),
    ]);
    return client.respondInboxItem.bind(client);
  },
}));
vi.mock("$lib/authSession", () => ({
  initializeAuthSession: vi.fn().mockResolvedValue({ actor_id: "human" }),
  // The Inbox pane asks whether the reader may decide an access request.
  isHumanWorkspacePrincipal: () => true,
  // The task page reads these to skip re-hydrating a session it already has.
  isAuthenticated: () => true,
  authSessionReady: {
    subscribe: (fn) => {
      fn(true);
      return () => {};
    },
  },
  authenticatedAgent: {
    subscribe: (fn) => {
      fn({ actor_id: "human" });
      return () => {};
    },
  },
}));
vi.mock("$app/navigation", () => ({
  goto: navigation.goto,
  beforeNavigate: (fn) => navigation.guards.push(fn),
  afterNavigate: vi.fn(),
  invalidate: vi.fn(),
  invalidateAll: vi.fn(),
}));
import WorkPage from "../../src/routes/o/[organization]/w/[workspace]/initiatives/+page.svelte";
import WorkDetail from "../../src/routes/o/[organization]/w/[workspace]/initiatives/[workId]/+page.svelte";
import PMPage from "../../src/routes/o/[organization]/w/[workspace]/pm/+page.svelte";
import { get as getStore } from "svelte/store";
import {
  currentOrganizationSlug,
  currentWorkspaceSlug,
} from "../../src/lib/workspaceContext.js";
import { resetInboxResponseQueue } from "../../src/lib/inboxResponseQueue.js";
import { clearWorkspaceViews } from "../../src/lib/workspaceViewCache.js";
import WorkViews from "../../src/lib/components/pm/WorkViews.svelte";

const work = (ref, title) => ({
  ref,
  title,
  source: { authority: "github", native_status: "Custom phase" },
  phase: "vendor_waiting",
  freshness: { status: "unknown" },
});
function deferred() {
  let resolve;
  const promise = new Promise((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
beforeEach(async () => {
  await clearPrincipalLocalWork(getStore(readerScope));
  localStorage.clear();
  Object.defineProperty(navigator, "locks", {
    configurable: true,
    value: { request: async (_name, callback) => callback() },
  });
  clearWorkspaceViews();
  vi.clearAllMocks();
  for (const mock of Object.values(client)) mock.mockReset();
  navigation.guards.length = 0;
  state.route("/initiatives");
  client.listWork.mockResolvedValue({ work: [], next_cursor: "" });
  client.listWorkParticipants.mockResolvedValue({
    participants: [],
    next_cursor: "",
  });
  client.listPmConversations.mockResolvedValue({ items: [] });
  client.listPmActions.mockResolvedValue({ items: [] });
  client.listPmDecisions.mockResolvedValue({ items: [] });
  client.listInboxItems.mockResolvedValue({ items: [] });
  client.getHomeUnread.mockResolvedValue({ groups: [] });
  client.listEvents.mockResolvedValue({ events: [] });
  // An idle stream: open until the page unsubscribes.
  client.streamEvents.mockImplementation(
    ({ signal }) =>
      new Promise((resolve) => signal?.addEventListener("abort", resolve)),
  );
});
afterEach(async () => {
  await clearPrincipalLocalWork(getStore(readerScope));
  cleanup();
  resetInboxResponseQueue();
  capturedScopes.length = 0;
  currentOrganizationSlug.set("");
  currentWorkspaceSlug.set("");
});

describe("PM operator interactions", () => {
  it("keeps board and table records identical including an unfamiliar phase", async () => {
    const rows = [
      work("card:one", "Sample one"),
      { ...work("card:two", "Sample two"), phase: "blocked" },
    ];
    const result = render(WorkViews, {
      records: rows,
      workspaceHref: (path) => path,
    });
    const refs = () =>
      [...result.container.querySelectorAll("[data-work-ref]")]
        .map((node) => node.dataset.workRef)
        .sort();
    expect(refs()).toEqual(["card:one", "card:two"]);
    await result.rerender({
      records: rows,
      view: "board",
      workspaceHref: (path) => path,
    });
    expect(refs()).toEqual(["card:one", "card:two"]);
    expect(
      screen.getByRole("heading", { name: "vendor_waiting" }),
    ).toBeTruthy();
  });
  it("discards an older work-filter response instead of regressing the list", async () => {
    const old = deferred();
    client.listWork.mockReturnValueOnce(old.promise).mockResolvedValueOnce({
      work: [work("card:new", "Current work")],
      next_cursor: "",
    });
    render(WorkPage);
    await waitFor(() => expect(client.listWork).toHaveBeenCalledTimes(1));
    state.route("/initiatives?q=new");
    await waitFor(() => expect(screen.getByText("Current work")).toBeTruthy());
    old.resolve({ work: [work("card:old", "Old work")], next_cursor: "" });
    await Promise.resolve();
    expect(screen.queryByText("Old work")).toBeNull();
    expect(screen.getByText("Current work")).toBeTruthy();
  });
  it("re-reads on a live task event and keeps rows when that read fails", async () => {
    let emit;
    client.streamEvents.mockImplementation(
      ({ onEvent, signal }) =>
        new Promise((resolve) => {
          emit = onEvent;
          signal?.addEventListener("abort", resolve);
        }),
    );
    client.listWork
      .mockResolvedValueOnce({
        work: [work("card:one", "Loaded work")],
        next_cursor: "",
      })
      .mockRejectedValueOnce(new Error("Temporary outage"));
    render(WorkPage);
    await screen.findByText("Loaded work");
    // No Reload button: the list follows the event stream.
    expect(screen.queryByRole("button", { name: "Reload" })).toBeNull();
    await waitFor(() => expect(emit).toBeTypeOf("function"));
    // The workspace stream is shared; the page filters card events itself.
    expect(client.streamEvents).toHaveBeenCalledTimes(1);
    emit({
      id: "evt-1",
      event: "event",
      data: {
        event: {
          id: "evt-1",
          type: "card_moved",
          ts: new Date().toISOString(),
          refs: ["card:one"],
        },
      },
    });
    await screen.findByText("Temporary outage", {}, { timeout: 3000 });
    expect(screen.getByText("Loaded work")).toBeTruthy();
    expect(
      screen.getByText(/Showing the previously loaded records/),
    ).toBeTruthy();
  });
  it("queues the final Tasks event arriving during an active read", async () => {
    let emit;
    client.streamEvents.mockImplementation(
      ({ onEvent, signal }) =>
        new Promise((resolve) => {
          emit = onEvent;
          signal?.addEventListener("abort", resolve);
        }),
    );
    const slow = deferred();
    client.listWork.mockReturnValueOnce(slow.promise).mockResolvedValue({
      work: [work("card:latest", "Latest work")],
      next_cursor: "",
    });
    render(WorkPage);
    await waitFor(() => expect(client.listWork).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(emit).toBeTypeOf("function"));
    emit({
      id: "during-read",
      event: "event",
      data: {
        event: {
          id: "during-read",
          type: "card_moved",
          ts: new Date().toISOString(),
          refs: ["card:latest"],
        },
      },
    });
    // Stream coalescing delivers while the first read remains outstanding.
    await new Promise((resolve) => setTimeout(resolve, 800));
    slow.resolve({ work: [work("card:old", "Old snapshot")], next_cursor: "" });
    await screen.findByText("Latest work", {}, { timeout: 3000 });
    expect(client.listWork).toHaveBeenCalledTimes(2);
  });

  it("does not replay a queued live read for an obsolete Tasks filter", async () => {
    let emit;
    client.streamEvents.mockImplementation(
      ({ onEvent, signal }) =>
        new Promise((resolve) => {
          emit = onEvent;
          signal?.addEventListener("abort", resolve);
        }),
    );
    const slow = deferred();
    client.listWork.mockImplementation(({ q }) =>
      q === "new"
        ? Promise.resolve({
            work: [work("card:new", "New filter result")],
            next_cursor: "",
          })
        : slow.promise,
    );
    render(WorkPage);
    await waitFor(() => expect(client.listWork).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(emit).toBeTypeOf("function"));
    emit({
      id: "queued",
      event: "event",
      data: {
        event: {
          id: "queued",
          type: "card_moved",
          ts: new Date().toISOString(),
          refs: ["card:old"],
        },
      },
    });
    await new Promise((resolve) => setTimeout(resolve, 800));
    state.route("/initiatives?q=new");
    await screen.findByText("New filter result");
    slow.resolve({
      work: [work("card:old", "Old filter result")],
      next_cursor: "",
    });
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(screen.queryByText("Old filter result")).toBeNull();
    expect(screen.getByText("New filter result")).toBeTruthy();
    expect(client.listWork).toHaveBeenCalledTimes(2);
  });

  it("clears a live plan when the next read has no steps", async () => {
    let emit;
    client.streamEvents.mockImplementation(
      ({ onEvent, signal }) =>
        new Promise((resolve) => {
          emit = onEvent;
          signal?.addEventListener("abort", resolve);
        }),
    );
    state.route("/initiatives/card%3Aone", { workId: "card:one" });
    client.getWork.mockResolvedValue({
      work: work("card:one", "Planned work"),
    });
    client.listWorkObservations.mockResolvedValue({ observations: [] });
    client.getCardPlan
      .mockResolvedValueOnce({
        plan: { steps: [{ id: "ship", title: "Ship", after: [] }] },
        plan_state: {
          steps: [{ id: "ship", status: "active" }],
          progress: { done: 0, total: 1 },
          next_steps: ["ship"],
        },
      })
      .mockResolvedValueOnce({ plan: null, plan_state: null });
    render(WorkDetail);
    await screen.findByRole("heading", { name: "Plan", exact: true });
    await waitFor(() => expect(emit).toBeTypeOf("function"));
    emit({
      id: "evt-plan-cleared",
      event: "event",
      data: {
        event: {
          id: "evt-plan-cleared",
          type: "card_updated",
          ts: new Date().toISOString(),
          refs: ["card:one"],
        },
      },
    });
    await waitFor(
      () =>
        expect(
          screen.queryByRole("heading", { name: "Plan", exact: true }),
        ).toBeNull(),
      { timeout: 3000 },
    );
  });
  it("keeps the last plan when a live reread fails", async () => {
    let emit;
    client.streamEvents.mockImplementation(
      ({ onEvent, signal }) =>
        new Promise((resolve) => {
          emit = onEvent;
          signal?.addEventListener("abort", resolve);
        }),
    );
    state.route("/initiatives/card%3Aone", { workId: "card:one" });
    client.getWork.mockResolvedValue({
      work: work("card:one", "Planned work"),
    });
    client.listWorkObservations.mockResolvedValue({ observations: [] });
    client.getCardPlan
      .mockResolvedValueOnce({
        plan: { steps: [{ id: "ship", title: "Ship", after: [] }] },
        plan_state: {
          steps: [{ id: "ship", status: "active" }],
          progress: { done: 0, total: 1 },
          next_steps: ["ship"],
        },
      })
      .mockRejectedValueOnce(new Error("temporary"));
    render(WorkDetail);
    await screen.findByRole("heading", { name: "Plan", exact: true });
    await waitFor(() => expect(emit).toBeTypeOf("function"));
    emit({
      id: "evt-plan-failed",
      event: "event",
      data: {
        event: {
          id: "evt-plan-failed",
          type: "card_updated",
          ts: new Date().toISOString(),
          refs: ["card:one"],
        },
      },
    });
    await waitFor(() => expect(client.getCardPlan).toHaveBeenCalledTimes(2), {
      timeout: 3000,
    });
    expect(
      screen.getByRole("heading", { name: "Plan", exact: true }),
    ).toBeTruthy();
    expect(document.querySelector('[data-plan-node="ship"]')).toBeTruthy();
  });
  it("does not promote the claimed verification field, and refresh only queues", async () => {
    state.route("/initiatives/card%3Aone", { workId: "card:one" });
    client.getWork.mockResolvedValue({
      work: { ...work("card:one", "Claimed work"), refresh: { state: "idle" } },
    });
    client.listWorkObservations.mockResolvedValue({
      observations: [
        {
          id: "one",
          status: "verified",
          verification: "reported",
          reader_id: "reader",
          evidence: [],
        },
      ],
    });
    client.requestWorkRefresh.mockResolvedValue({
      refresh: { state: "queued" },
    });
    render(WorkDetail);
    await screen.findAllByText("Reported claim");
    expect(screen.queryByText("Verified evidence")).toBeNull();
    await fireEvent.click(
      screen.getByRole("button", { name: "Check GitHub now" }),
    );
    await screen.findByText(
      "Refresh queued. Evidence changes only after a reader reports back.",
    );
    expect(client.requestWorkRefresh).toHaveBeenCalledWith("card:one");
  });
  it("keeps the Source filter labeled after switching to the board view", async () => {
    client.listWork.mockResolvedValue({
      work: [work("card:one", "Sample one")],
      next_cursor: "",
    });
    render(WorkPage);
    await screen.findByText("Sample one");
    expect(screen.getByLabelText("Source", { exact: true }).tagName).toBe(
      "SELECT",
    );
    state.route("/initiatives?view=board");
    await screen.findByRole("region", {
      name: "Initiatives grouped by phase",
    });
    expect(screen.getByLabelText("Source", { exact: true }).tagName).toBe(
      "SELECT",
    );
  });
  it("preserves a typed PM draft until the conversation list is ready", async () => {
    const pending = deferred();
    client.listPmConversations.mockReturnValue(pending.promise);
    state.route("/pm?work_ref=card%3Aone");
    render(PMPage);
    const input = await screen.findByLabelText("Message PM");
    await fireEvent.input(input, {
      target: { value: "What is still unverified?" },
    });
    expect(screen.getByRole("button", { name: "Send message" }).disabled).toBe(
      true,
    );
    pending.resolve({ items: [] });
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Send message" }).disabled,
      ).toBe(false),
    );
    expect(input.value).toBe("What is still unverified?");
  });
  it("keeps Send message disabled when the PM conversation list is unavailable", async () => {
    client.listPmConversations.mockRejectedValue(
      new Error("PM bridge unavailable"),
    );
    state.route("/pm?work_ref=card%3Aone");
    render(PMPage);
    await screen.findByText("PM bridge unavailable");
    const input = screen.getByLabelText("Message PM");
    await fireEvent.input(input, {
      target: { value: "What is still unverified?" },
    });
    expect(screen.getByRole("button", { name: "Send message" }).disabled).toBe(
      true,
    );
    await fireEvent.submit(input.closest("form"));
    expect(client.createPmConversation).not.toHaveBeenCalled();
    expect(client.sendPmMessage).not.toHaveBeenCalled();
  });
  it("retains a failed PM message and replays the same request key", async () => {
    state.route("/pm?conversation=conversation-one");
    client.getPmConversation.mockResolvedValue({
      conversation: { id: "conversation-one", title: "Sample discussion" },
      turns: [],
    });
    client.sendPmMessage
      .mockRejectedValueOnce(new Error("Bridge unavailable"))
      .mockResolvedValueOnce({ id: "turn-one", status: "sending" });
    render(PMPage);
    await screen.findByText("Sample discussion");
    const input = screen.getByLabelText("Message PM");
    await fireEvent.input(input, {
      target: { value: "What remains uncertain?" },
    });
    await fireEvent.submit(input.closest("form"));
    await screen.findByText("Retrying");
    expect(screen.getByLabelText("Saved message").value).toBe(
      "What remains uncertain?",
    );
    await waitFor(() => expect(client.sendPmMessage).toHaveBeenCalledTimes(2), {
      timeout: 4000,
    });
    expect(client.sendPmMessage.mock.calls[0][1]).toEqual(
      client.sendPmMessage.mock.calls[1][1],
    );
  });
  it("drains a PM message through mutationOutbox when navigator.locks is unavailable", async () => {
    Object.defineProperty(navigator, "locks", {
      configurable: true,
      value: undefined,
    });
    state.route("/pm?conversation=conversation-one");
    client.getPmConversation.mockResolvedValue({
      conversation: { id: "conversation-one", title: "Sample discussion" },
      turns: [],
    });
    client.sendPmMessage.mockResolvedValueOnce({
      id: "turn-fallback",
      status: "sending",
    });
    render(PMPage);
    await screen.findByText("Sample discussion");
    const input = screen.getByLabelText("Message PM");
    await fireEvent.input(input, {
      target: { value: "Sent over plain HTTP LAN" },
    });
    await fireEvent.submit(input.closest("form"));
    await waitFor(() => expect(client.sendPmMessage).toHaveBeenCalledTimes(1));
    expect(client.sendPmMessage.mock.calls[0][1]).toMatchObject({
      text: "Sent over plain HTTP LAN",
    });
  });
  it("loads older PM turns without losing the latest reply", async () => {
    state.route("/pm?conversation=conversation-one");
    client.getPmConversation
      .mockResolvedValueOnce({
        conversation: { id: "conversation-one", title: "Sample discussion" },
        turns: [
          {
            id: "latest",
            text: "Latest question",
            response: "Latest reply",
            status: "delivered",
          },
        ],
        next_cursor: "older-turns",
        has_more: true,
      })
      .mockResolvedValueOnce({
        conversation: { id: "conversation-one" },
        turns: [
          {
            id: "older",
            text: "Earlier question",
            response: "Earlier reply",
            status: "delivered",
          },
        ],
        next_cursor: "",
      });
    render(PMPage);
    await screen.findByText("Latest reply");
    await fireEvent.click(
      screen.getByRole("button", { name: "Older messages" }),
    );
    await screen.findByText("Earlier reply");
    expect(screen.getByText("Latest reply")).toBeTruthy();
    expect(client.getPmConversation).toHaveBeenLastCalledWith(
      "conversation-one",
      { limit: 100, cursor: "older-turns" },
    );
  });
  it("clears an old refresh request state when navigating to another commitment", async () => {
    state.route("/initiatives/card%3Aone", { workId: "card:one" });
    client.getWork
      .mockResolvedValueOnce({ work: work("card:one", "First commitment") })
      .mockResolvedValueOnce({ work: work("card:two", "Second commitment") });
    client.listWorkObservations.mockResolvedValue({ observations: [] });
    const pending = deferred();
    client.requestWorkRefresh.mockReturnValue(pending.promise);
    render(WorkDetail);
    await screen.findByRole("heading", { name: "First commitment" });
    await fireEvent.click(
      screen.getByRole("button", { name: "Check GitHub now" }),
    );
    state.route("/initiatives/card%3Atwo", { workId: "card:two" });
    await screen.findByRole("heading", { name: "Second commitment" });
    expect(
      screen.getByRole("button", { name: "Check GitHub now" }).disabled,
    ).toBe(false);
    pending.resolve({ refresh: { state: "queued" } });
  });
  it("allows navigation after a PM message has been saved locally", async () => {
    state.route("/pm?conversation=conversation-one");
    client.getPmConversation.mockResolvedValue({
      conversation: { id: "conversation-one", title: "Sample discussion" },
      turns: [],
    });
    const pending = deferred();
    client.sendPmMessage.mockReturnValue(pending.promise);
    render(PMPage);
    await screen.findByText("Sample discussion");
    const input = screen.getByLabelText("Message PM");
    await fireEvent.input(input, { target: { value: "Sample request" } });
    await fireEvent.submit(input.closest("form"));
    await waitFor(() => expect(client.sendPmMessage).toHaveBeenCalledTimes(1));
    const cancel = vi.fn();
    expect(navigation.guards).toEqual([]);
    expect(cancel).not.toHaveBeenCalled();
    pending.resolve({ id: "turn-one" });
  });

  it("orders the table by attention and folds closed work behind a toggle", async () => {
    const native = (ref, title, phase, board = "board:one") => ({
      ref,
      title,
      phase,
      board_ref: board,
      source: { authority: "nexus" },
      freshness: {},
    });
    const rows = [
      native("card:done", "Finished thing", "done"),
      native("card:backlog", "Someday thing", "backlog"),
      native("card:blocked", "Stuck thing", "blocked"),
      native("card:progress", "Moving thing", "in_progress"),
    ];
    const result = render(WorkViews, {
      records: rows,
      workspaceHref: (path) => path,
      closedHref: "/tasks?closed=1",
    });
    const titles = () =>
      [...result.container.querySelectorAll("tbody tr a")].map(
        (node) => node.textContent,
      );
    expect(titles()).toEqual(["Stuck thing", "Moving thing", "Someday thing"]);
    // One board and no source-backed task: neither column earns its place.
    const headings = [...result.container.querySelectorAll("thead th")].map(
      (node) => node.textContent.trim(),
    );
    expect(headings).toEqual(["Initiative", "Status", "Owner"]);
    expect(screen.queryByText("created here")).toBeNull();
    const toggle = result.container.querySelector("[data-work-closed-toggle]");
    expect(toggle.textContent).toContain("1 done");
    expect(toggle.getAttribute("href")).toBe("/tasks?closed=1");
    await result.rerender({
      records: [
        ...rows,
        {
          ...native("card:gh", "Mirrored issue", "ready", "board:two"),
          source: { authority: "github", native_id: "o/r#1" },
          freshness: { last_observed_at: new Date().toISOString() },
        },
      ],
      workspaceHref: (path) => path,
      showClosed: true,
      closedHref: "/tasks",
    });
    expect(titles()).toEqual([
      "Stuck thing",
      "Moving thing",
      "Mirrored issue",
      "Someday thing",
      "Finished thing",
    ]);
    const wide = [...result.container.querySelectorAll("thead th")].map(
      (node) => node.textContent.trim(),
    );
    expect(wide).toEqual([
      "Initiative",
      "Group",
      "Status",
      "Owner",
      "Last checked",
    ]);
  });
  it("keeps the latest handoff report when older observation pagination fails", async () => {
    state.route("/initiatives/card%3Aone", { workId: "card:one" });
    client.getWork.mockResolvedValue({
      work: work("card:one", "Evidence task"),
    });
    client.listWorkObservations
      .mockResolvedValueOnce({
        observations: [
          {
            id: "current",
            status: "reported",
            actor_id: "reviewer",
            observed_at: new Date().toISOString(),
            evidence: [],
          },
        ],
        next_cursor: "older",
      })
      .mockRejectedValueOnce(new Error("Older history unavailable"));
    render(WorkDetail);
    await screen.findByText("Latest shared report");
    await fireEvent.click(
      screen.getByRole("button", { name: "Older observations", hidden: true }),
    );
    await screen.findByText("Older history unavailable");
    expect(screen.getByText("Latest shared report")).toBeTruthy();
    expect(screen.queryByText(/Handoff evidence is unavailable/)).toBeNull();
    expect(client.listWorkObservations).toHaveBeenLastCalledWith("card:one", {
      limit: 30,
      cursor: "older",
    });
  });
  it("marks handoff unavailable when the initial evidence read fails", async () => {
    state.route("/initiatives/card%3Aone", { workId: "card:one" });
    client.getWork.mockResolvedValue({
      work: work("card:one", "Evidence task"),
    });
    client.listWorkObservations.mockRejectedValue(
      new Error("Evidence unavailable"),
    );
    render(WorkDetail);
    await screen.findByText(/Handoff evidence is unavailable/);
    expect(screen.queryByText("Latest shared report")).toBeNull();
  });
  it("shows task evidence as one source line with each link once", async () => {
    state.route("/initiatives/card%3Aone", { workId: "card:one" });
    client.getWork.mockResolvedValue({
      work: {
        ...work("card:one", "Issue work"),
        source: { authority: "github", native_id: "org/repo#208" },
      },
    });
    const issue = "https://github.com/org/repo/issues/208";
    client.listWorkObservations.mockResolvedValue({
      observations: [1, 2, 3, 4].map((n) => ({
        id: `o${n}`,
        status: "reported",
        observed_at: `2026-09-01T12:0${n}:00Z`,
        evidence: [
          { url: issue, kind: "issue" },
          ...[1, 2, 3, 4].map((c) => ({
            url: `${issue}#issuecomment-${c}`,
            kind: "comment",
          })),
        ],
      })),
    });
    client.listWork.mockResolvedValue({ work: [] });
    render(WorkDetail);
    const line = await waitFor(() => {
      const node = document.querySelector("[data-evidence-source]");
      if (!node) throw new Error("no source line yet");
      return node;
    });
    expect(line.textContent.replace(/\s+/g, " ")).toContain(
      "GitHub #208 · 4 observations",
    );
    const links = screen.getByRole("list", { name: "Evidence links" });
    expect(
      [...links.querySelectorAll("a")].map((a) => a.textContent.trim()),
    ).toEqual([
      "Issue #208 ↗",
      "Comment 1 ↗",
      "Comment 2 ↗",
      "Comment 3 ↗",
      "Comment 4 ↗",
    ]);
  });
});

describe("tasks list live filters", () => {
  it("keeps a free-text filter when another control changes in the same turn", async () => {
    render(WorkPage);
    await waitFor(() => expect(client.listWork).toHaveBeenCalled());

    await fireEvent.click(screen.getByTestId("tasks-filters-toggle"));
    const project = screen.getByLabelText("Project reference");
    await fireEvent.change(project, { target: { value: "topic:release" } });
    await fireEvent.change(screen.getByLabelText("Source"), {
      target: { value: "github" },
    });

    expect(navigation.goto).toHaveBeenCalled();
    const lastHref = String(navigation.goto.mock.calls.at(-1)?.[0] ?? "");
    expect(lastHref).toContain("project_ref=topic%3Arelease");
    expect(lastHref).toContain("source=github");
  });
});
