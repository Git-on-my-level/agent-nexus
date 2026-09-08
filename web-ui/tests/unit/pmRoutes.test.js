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
    url: new URL("http://localhost/o/local/w/local/work"),
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
    ].map((key) => [key, vi.fn()]),
  ),
);
const navigation = vi.hoisted(() => ({ goto: vi.fn(), guards: [] }));
vi.mock("$app/stores", () => ({ page: { subscribe: state.subscribe } }));
vi.mock("$lib/coreClient", () => ({ coreClient: client }));
vi.mock("$lib/authSession", () => ({
  initializeAuthSession: vi.fn().mockResolvedValue({ actor_id: "human" }),
}));
vi.mock("$app/navigation", () => ({
  goto: navigation.goto,
  beforeNavigate: (fn) => navigation.guards.push(fn),
  afterNavigate: vi.fn(),
  invalidate: vi.fn(),
  invalidateAll: vi.fn(),
}));
import WorkPage from "../../src/routes/o/[organization]/w/[workspace]/work/+page.svelte";
import WorkDetail from "../../src/routes/o/[organization]/w/[workspace]/work/[workId]/+page.svelte";
import PMPage from "../../src/routes/o/[organization]/w/[workspace]/pm/+page.svelte";
import Decisions from "../../src/routes/o/[organization]/w/[workspace]/decisions/+page.svelte";
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
  state.route("/work");
  client.listWork.mockResolvedValue({ work: [], next_cursor: "" });
  client.listPmConversations.mockResolvedValue({ items: [] });
  client.listPmActions.mockResolvedValue({ items: [] });
});
afterEach(() => cleanup());

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
    state.route("/work?q=new");
    await waitFor(() => expect(screen.getByText("Current work")).toBeTruthy());
    old.resolve({ work: [work("card:old", "Old work")], next_cursor: "" });
    await Promise.resolve();
    expect(screen.queryByText("Old work")).toBeNull();
    expect(screen.getByText("Current work")).toBeTruthy();
  });
  it("retains a failed reload with an explicit outdated-data warning", async () => {
    client.listWork
      .mockResolvedValueOnce({
        work: [work("card:one", "Loaded work")],
        next_cursor: "",
      })
      .mockRejectedValueOnce(new Error("Temporary outage"));
    render(WorkPage);
    await screen.findByText("Loaded work");
    await fireEvent.click(
      screen.getByRole("button", { name: "Reload", exact: true }),
    );
    await screen.findByText("Temporary outage");
    expect(screen.getByText("Loaded work")).toBeTruthy();
    expect(
      screen.getByText(/Showing the previously loaded records/),
    ).toBeTruthy();
  });
  it("does not promote the claimed verification field, and refresh only queues", async () => {
    state.route("/work/card%3Aone", { workId: "card:one" });
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
    await screen.findByText("Reported claim");
    expect(screen.queryByText("Verified evidence")).toBeNull();
    await fireEvent.click(
      screen.getByRole("button", { name: "Request source refresh" }),
    );
    await screen.findByText(
      "Refresh queued. Evidence changes only after a reader reports back.",
    );
    expect(client.requestWorkRefresh).toHaveBeenCalledWith("card:one");
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
    state.route("/decisions?decision=decision-one");
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
    render(Decisions);
    await screen.findByRole("heading", { name: "Update sample note" });
    await fireEvent.click(screen.getByLabelText("Authorize this scope"));
    const input = screen.getByLabelText("Exact response");
    await fireEvent.input(input, {
      target: { value: "Within the stated scope" },
    });
    await fireEvent.submit(input.closest("form"));
    await screen.findByText("Pending delivery");
    expect(client.answerPmDecision).toHaveBeenCalledWith("decision-one", {
      revision: 2,
      approve: true,
      text: "Within the stated scope",
    });
    expect(client.dispatchPmDecision).not.toHaveBeenCalled();
    await fireEvent.click(
      screen.getByRole("button", { name: "Deliver approved instruction" }),
    );
    await screen.findByText("Failed", { exact: true });
    expect(screen.getByText("Within the stated scope")).toBeTruthy();
    expect(screen.queryByText("Outcome verified")).toBeNull();
  });
  it("loads a directly linked decision and receipt beyond the partial list", async () => {
    state.route("/decisions?decision=older-decision");
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
    render(Decisions);
    await screen.findByRole("heading", { name: "Older sample instruction" });
    expect(client.getPmDecision).toHaveBeenCalledWith("older-decision");
    expect(
      (await screen.findAllByText("Delivery uncertain", { exact: true }))
        .length,
    ).toBeGreaterThan(0);
    expect(client.getPmAction).toHaveBeenCalledWith("older-action");
  });
  it("can discover a later decision through canonical pagination", async () => {
    state.route("/decisions");
    client.listPmDecisions
      .mockResolvedValueOnce({
        items: [],
        has_more: true,
        next_cursor: "next-decisions",
      })
      .mockResolvedValueOnce({
        items: [
          {
            id: "later",
            instruction: "Later sample instruction",
            status: "awaiting_answer",
            work_ref: "card:one",
          },
        ],
        next_cursor: "",
        has_more: false,
      });
    render(Decisions);
    await fireEvent.click(
      await screen.findByRole("button", {
        name: "Load more decisions and receipts",
      }),
    );
    await screen.findByRole("heading", { name: "Later sample instruction" });
    expect(client.listPmDecisions).toHaveBeenLastCalledWith({
      limit: 50,
      cursor: "next-decisions",
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
    state.route("/work/card%3Aone", { workId: "card:one" });
    client.getWork
      .mockResolvedValueOnce({ work: work("card:one", "First commitment") })
      .mockResolvedValueOnce({ work: work("card:two", "Second commitment") });
    client.listWorkObservations.mockResolvedValue({ observations: [] });
    const pending = deferred();
    client.requestWorkRefresh.mockReturnValue(pending.promise);
    render(WorkDetail);
    await screen.findByRole("heading", { name: "First commitment" });
    await fireEvent.click(
      screen.getByRole("button", { name: "Request source refresh" }),
    );
    state.route("/work/card%3Atwo", { workId: "card:two" });
    await screen.findByRole("heading", { name: "Second commitment" });
    expect(
      screen.getByRole("button", { name: "Request source refresh" }).disabled,
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
});
