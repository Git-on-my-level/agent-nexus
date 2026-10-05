// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => {
  let value = {
    url: new URL("http://localhost/o/local/w/local/tasks"),
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
vi.mock("$app/stores", () => ({ page: { subscribe: state.subscribe } }));
vi.mock("$lib/coreClient", () => ({ coreClient: client }));
vi.mock("$lib/authSession", () => ({
  initializeAuthSession: vi.fn().mockResolvedValue({ actor_id: "human" }),
  // The Inbox pane asks whether the reader may decide an access request.
  isHumanWorkspacePrincipal: () => true,
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
import WorkPage from "../../src/routes/o/[organization]/w/[workspace]/tasks/+page.svelte";
import WorkDetail from "../../src/routes/o/[organization]/w/[workspace]/tasks/[workId]/+page.svelte";
import PMPage from "../../src/routes/o/[organization]/w/[workspace]/pm/+page.svelte";
import InboxPage from "../../src/routes/o/[organization]/w/[workspace]/inbox/+page.svelte";
import {
  flushInboxResponse,
  resetInboxResponseQueue,
} from "../../src/lib/inboxResponseQueue.js";
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
beforeEach(() => {
  vi.clearAllMocks();
  for (const mock of Object.values(client)) mock.mockReset();
  navigation.guards.length = 0;
  state.route("/tasks");
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
afterEach(() => {
  cleanup();
  resetInboxResponseQueue();
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
    state.route("/tasks?q=new");
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
  it("does not promote the claimed verification field, and refresh only queues", async () => {
    state.route("/tasks/card%3Aone", { workId: "card:one" });
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
  it("scopes task detail decisions to this work, newest first, with an inbox answer link", async () => {
    state.route("/tasks/card%3Aone", { workId: "card:one" });
    client.getWork.mockResolvedValue({
      work: work("card:one", "Decided work"),
    });
    client.listWorkObservations.mockResolvedValue({ observations: [] });
    client.listPmDecisions.mockResolvedValue({
      items: [
        {
          id: "d-old",
          work_ref: "card:one",
          instruction: "Older question",
          status: "answered",
          created_at: "2026-09-01T10:00:00Z",
        },
        {
          id: "d-new",
          work_ref: "card:one",
          instruction: "Newer question",
          status: "awaiting_answer",
          created_at: "2026-09-02T10:00:00Z",
        },
        {
          id: "d-other",
          work_ref: "card:two",
          instruction: "Other work question",
          status: "awaiting_answer",
          created_at: "2026-09-03T10:00:00Z",
        },
      ],
    });
    render(WorkDetail);
    await screen.findByText("Newer question");
    expect(screen.getByText("Older question")).toBeTruthy();
    expect(screen.queryByText("Other work question")).toBeNull();
    expect(client.listPmDecisions).toHaveBeenCalledWith({ limit: 200 });
    expect(
      screen.getByRole("link", { name: "Answer", exact: true }),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "Answer", exact: true })
        .getAttribute("href"),
    ).toBe("/o/local/w/local/inbox?item=decision:d-new");
    const rows = [
      ...screen
        .getByText("Newer question")
        .closest("ul")
        .querySelectorAll("li"),
    ];
    expect(rows.map((row) => row.querySelector("p").textContent)).toEqual([
      "Newer question",
      "Older question",
    ]);
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
    state.route("/tasks?view=board");
    await screen.findByRole("region", {
      name: "Task board grouped by phase",
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
    await screen.findByText("Bridge unavailable");
    expect(input.value).toBe("What remains uncertain?");
    await fireEvent.submit(input.closest("form"));
    await waitFor(() => expect(client.sendPmMessage).toHaveBeenCalledTimes(2));
    expect(client.sendPmMessage.mock.calls[0][1]).toEqual(
      client.sendPmMessage.mock.calls[1][1],
    );
  });
  it("records a revision-bound answer without dispatching and retains failed follow-through", async () => {
    state.route("/inbox?item=decision:decision-one");
    const decision = {
      id: "decision-one",
      work_ref: "card:one",
      instruction: "Update sample note",
      scope: "work.annotate",
      target_revision: "7",
      status: "awaiting_answer",
      revision: 2,
    };
    client.listPmDecisions.mockResolvedValue({ items: [decision] });
    client.answerPmDecision.mockResolvedValue({
      ...decision,
      status: "answered",
      answer: "Within the stated scope",
      action_id: "action-one",
      revision: 3,
    });
    client.getPmAction
      .mockResolvedValueOnce({
        id: "action-one",
        decision_id: decision.id,
        status: "pending_delivery",
      })
      .mockResolvedValueOnce({
        id: "action-one",
        decision_id: decision.id,
        status: "failed",
        receipt: { detail: "Source unavailable" },
      });
    client.dispatchPmDecision.mockRejectedValue(
      new Error("Source unavailable"),
    );
    render(InboxPage);
    await screen.findByRole("heading", { name: "Update sample note" });
    const input = screen.getByLabelText(
      "Your note (recorded with the decision)",
    );
    await fireEvent.input(input, {
      target: { value: "Within the stated scope" },
    });
    await fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    await screen.findByText("Pending delivery", { exact: true });
    expect(client.answerPmDecision).toHaveBeenCalledWith("decision-one", {
      revision: 2,
      approve: true,
      text: "Within the stated scope",
    });
    expect(client.dispatchPmDecision).not.toHaveBeenCalled();
    await fireEvent.click(
      screen.getByRole("button", { name: "Deliver approved instruction" }),
    );
    // The receipt shows in the panel and, once joined, on the row badge too.
    expect(
      (await screen.findAllByText("Failed", { exact: true })).length,
    ).toBeGreaterThan(0);
    expect(screen.getByText("Within the stated scope")).toBeTruthy();
    expect(screen.queryByText("Outcome verified")).toBeNull();
  });
  it("loads a directly linked decision and receipt beyond the partial list", async () => {
    state.route("/inbox?item=decision:older-decision");
    client.listPmDecisions.mockResolvedValue({ items: [], has_more: true });
    client.getPmDecision.mockResolvedValue({
      id: "older-decision",
      instruction: "Older sample instruction",
      work_ref: "card:one",
      action_id: "older-action",
      status: "answered",
      answer: "Previously authorized",
    });
    client.getPmAction.mockResolvedValue({
      id: "older-action",
      decision_id: "older-decision",
      status: "unknown",
      receipt: { detail: "Delivery uncertain" },
    });
    render(InboxPage);
    await screen.findByRole("heading", { name: "Older sample instruction" });
    expect(client.getPmDecision).toHaveBeenCalledWith("older-decision");
    expect(
      (await screen.findAllByText("Delivery uncertain", { exact: true }))
        .length,
    ).toBeGreaterThan(0);
    expect(client.getPmAction).toHaveBeenCalledWith("older-action");
  });
  it("places awaiting decisions in Needs you and selects the first row", async () => {
    state.route("/inbox");
    client.listPmDecisions.mockResolvedValue({
      items: [
        {
          id: "later",
          instruction: "Later sample instruction",
          status: "awaiting_answer",
          work_ref: "card:one",
        },
      ],
    });
    const { container } = render(InboxPage);
    await waitFor(() =>
      expect(
        container.querySelector('[data-inbox-row="decision:later"]'),
      ).toBeTruthy(),
    );
    // The first row of the mailbox is selected without a click; the pane is
    // never an empty "choose an item" placeholder while rows exist.
    expect(
      await screen.findByRole("heading", { name: "Later sample instruction" }),
    ).toBeTruthy();
    expect(screen.queryByText("Choose an item.")).toBeNull();
    expect(
      screen.getAllByRole("link", { name: /Needs you/ }).length,
    ).toBeGreaterThan(0);
  });
  it("sends a suggested response behind an undo toast, never at once", async () => {
    state.route("/inbox");
    const askedAt = new Date(
      Date.now() - (3 * 60 + 12) * 60_000 - 20_000,
    ).toISOString();
    client.listInboxItems.mockImplementation(async ({ status }) => ({
      items:
        status === "open"
          ? [
              {
                id: "inbox:ask-one",
                kind: "ask",
                title: "Pick the default path",
                requester_label: "Omar Reed",
                source_event_time: askedAt,
                response_proposals: ["Combat first", "Hub first"],
              },
            ]
          : [],
    }));
    client.respondInboxItem.mockResolvedValue({ event: { id: "e1" } });
    const { container } = render(InboxPage);
    // Auto-selected, with the wait spelled out and the recommendation marked.
    await screen.findByRole("heading", { name: "Pick the default path" });
    expect(screen.getByText(/has been blocked for/)).toBeTruthy();
    expect(
      container.querySelector("[data-inbox-blocked-for]")?.textContent,
    ).toBe("3h 12m");
    expect(screen.getByText("Recommended")).toBeTruthy();

    await fireEvent.keyDown(window, { key: "2" });
    expect(client.respondInboxItem).not.toHaveBeenCalled();
    expect(await screen.findByText("Sent to Omar Reed")).toBeTruthy();

    await fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    await flushInboxResponse();
    expect(client.respondInboxItem).not.toHaveBeenCalled();
    expect(screen.queryByText("Sent to Omar Reed")).toBeNull();
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
    state.route("/tasks/card%3Aone", { workId: "card:one" });
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
    state.route("/tasks/card%3Atwo", { workId: "card:two" });
    await screen.findByRole("heading", { name: "Second commitment" });
    expect(
      screen.getByRole("button", { name: "Check GitHub now" }).disabled,
    ).toBe(false);
    pending.resolve({ refresh: { state: "queued" } });
  });
  it("blocks navigation while a PM message is being submitted", async () => {
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
    navigation.guards[0]({ cancel });
    expect(cancel).toHaveBeenCalledTimes(1);
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
    expect(headings).toEqual(["Task", "Status", "Owner"]);
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
    expect(wide).toEqual(["Task", "Board", "Status", "Owner", "Last checked"]);
  });
  it("keeps the latest handoff report when older observation pagination fails", async () => {
    state.route("/tasks/card%3Aone", { workId: "card:one" });
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
    await fireEvent.click(screen.getByText("Observation history"));
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
    state.route("/tasks/card%3Aone", { workId: "card:one" });
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
    state.route("/tasks/card%3Aone", { workId: "card:one" });
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
