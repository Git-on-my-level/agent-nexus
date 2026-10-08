// @vitest-environment jsdom
import { cleanup, fireEvent, render, waitFor } from "@testing-library/svelte";
import { get } from "svelte/store";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const pageStore = vi.hoisted(() => {
  let value = {
    url: new URL("http://localhost/o/local/w/local/inbox/inbox-first"),
    params: {
      organization: "local",
      workspace: "local",
      id: "inbox-first",
    },
  };
  const subscribers = new Set();
  return {
    subscribe(fn) {
      subscribers.add(fn);
      fn(value);
      return () => subscribers.delete(fn);
    },
    set(next) {
      value = next;
      for (const fn of subscribers) fn(value);
    },
    reset() {
      this.set({
        url: new URL("http://localhost/o/local/w/local/inbox/inbox-first"),
        params: {
          organization: "local",
          workspace: "local",
          id: "inbox-first",
        },
      });
    },
  };
});

const coreClientMock = vi.hoisted(() => ({
  getInboxItem: vi.fn(),
  respondInboxItem: vi.fn(),
  createArtifactAttachment: vi.fn(),
  /** Where each sender was bound, recorded when it is captured. */
  capturedScopes: [],
}));

const searchActorsMock = vi.hoisted(() => vi.fn());

vi.mock("$app/environment", () => ({
  browser: true,
}));

vi.mock("$app/navigation", () => ({
  goto: vi.fn(),
  invalidate: vi.fn(),
  invalidateAll: vi.fn(),
  beforeNavigate: vi.fn(),
  afterNavigate: vi.fn(),
}));

vi.mock("$app/stores", () => ({
  page: {
    subscribe: pageStore.subscribe,
  },
}));

vi.mock("$lib/coreClient", () => ({
  coreClient: coreClientMock,
  // The response queue captures the sender when it enqueues, so the commit
  // survives the navigation that follows. The sender is bound to one
  // workspace and reader: record which, so a test can tell where an answer
  // was actually sent.
  captureInboxResponseSender: () => {
    coreClientMock.capturedScopes.push([
      get(currentOrganizationSlug),
      get(currentWorkspaceSlug),
      get(authenticatedAgent)?.actor_id,
    ]);
    return coreClientMock.respondInboxItem.bind(coreClientMock);
  },
}));

vi.mock("$lib/searchHelpers", () => ({
  searchActors: searchActorsMock,
}));

import { PROPOSAL_FLASH_MS } from "../../src/lib/inboxProposalChoice.js";
import {
  flushInboxResponse,
  inboxResponseOverlay,
  resetInboxResponseQueue,
} from "../../src/lib/inboxResponseQueue.js";
import { authenticatedAgent } from "../../src/lib/authSession.js";
import {
  currentOrganizationSlug,
  currentWorkspaceSlug,
} from "../../src/lib/workspaceContext.js";
import InboxDetailPage from "../../src/routes/o/[organization]/w/[workspace]/inbox/[id]/+page.svelte";

function inboxItem(id, title, overrides = {}) {
  return {
    id,
    kind: "ask",
    title,
    body: `${title} body`,
    thread_id: `thread-${id}`,
    subject_ref: `thread:thread-${id}`,
    related_refs: [`thread:thread-${id}`],
    response_proposals: [],
    notification_target_status: { resolvable: true },
    requester_label: "Requester",
    ...overrides,
  };
}

function setInboxRoute(id, workspace = "local") {
  pageStore.set({
    url: new URL(`http://localhost/o/local/w/${workspace}/inbox/${id}`),
    params: {
      organization: "local",
      workspace,
      id,
    },
  });
}

afterEach(() => {
  cleanup();
  resetInboxResponseQueue();
  vi.useRealTimers();
  vi.clearAllMocks();
  coreClientMock.capturedScopes.length = 0;
  currentOrganizationSlug.set("");
  currentWorkspaceSlug.set("");
  authenticatedAgent.set(null);
  localStorage.clear();
  pageStore.reset();
});

beforeEach(() => {
  pageStore.reset();
});

describe("inbox detail route state", () => {
  it("reloads on id changes, resets stale draft state, and ignores late prior loads", async () => {
    let resolveFirstLoad;
    coreClientMock.getInboxItem.mockImplementation((id) => {
      if (id === "inbox-first") {
        return new Promise((resolve) => {
          resolveFirstLoad = resolve;
        });
      }
      if (id === "inbox-second") {
        return Promise.resolve({
          item: inboxItem("inbox-second", "Second inbox item"),
        });
      }
      return Promise.reject(new Error(`unexpected id ${id}`));
    });

    const { getByRole, getByLabelText, queryByRole } = render(InboxDetailPage);

    await waitFor(() => {
      expect(coreClientMock.getInboxItem).toHaveBeenCalledWith("inbox-first");
    });

    setInboxRoute("inbox-second");

    await waitFor(() => {
      expect(coreClientMock.getInboxItem).toHaveBeenCalledWith("inbox-second");
    });
    await waitFor(() => {
      expect(getByRole("heading", { name: "Second inbox item" })).toBeTruthy();
    });

    await fireEvent.input(getByLabelText("Your response"), {
      target: { value: "second draft" },
    });

    resolveFirstLoad({
      item: inboxItem("inbox-first", "First inbox item"),
    });

    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(
      queryByRole("heading", { name: "First inbox item" }),
    ).not.toBeTruthy();
    expect(getByLabelText("Your response").value).toBe("second draft");
  });

  it("clears drafts and attachment state when navigating to another inbox item", async () => {
    coreClientMock.getInboxItem.mockImplementation((id) =>
      Promise.resolve({
        item: inboxItem(
          id,
          id === "inbox-first" ? "First item" : "Second item",
        ),
      }),
    );
    coreClientMock.createArtifactAttachment.mockResolvedValue({
      artifact: {
        id: "artifact-first",
        original_filename: "first.txt",
        content_type: "text/plain",
        size_bytes: 5,
      },
    });

    const { getByLabelText, getByRole, queryByLabelText } =
      render(InboxDetailPage);

    await waitFor(() => {
      expect(getByRole("heading", { name: "First item" })).toBeTruthy();
    });

    await fireEvent.input(getByLabelText("Your response"), {
      target: { value: "draft from first item" },
    });
    const file = new File(["hello"], "first.txt", { type: "text/plain" });
    await fireEvent.change(getByLabelText("Attach file"), {
      target: { files: [file] },
    });

    await waitFor(() => {
      expect(queryByLabelText("Remove artifact:artifact-first")).toBeTruthy();
    });

    setInboxRoute("inbox-second");

    await waitFor(() => {
      expect(getByRole("heading", { name: "Second item" })).toBeTruthy();
    });

    expect(getByLabelText("Your response").value).toBe("");
    expect(queryByLabelText("Remove artifact:artifact-first")).not.toBeTruthy();
  });

  it("clears a pending notify target debounce timer on unmount", async () => {
    vi.useFakeTimers();
    coreClientMock.getInboxItem.mockResolvedValue({
      item: inboxItem("inbox-first", "First item"),
    });

    const { getByPlaceholderText, getByRole, unmount } =
      render(InboxDetailPage);

    await waitFor(() => {
      expect(getByRole("heading", { name: "First item" })).toBeTruthy();
    });

    await fireEvent.click(getByRole("button", { name: "Someone else" }));
    await fireEvent.input(getByPlaceholderText("Search people or agents…"), {
      target: { value: "alex" },
    });

    unmount();
    await vi.advanceTimersByTimeAsync(250);

    expect(searchActorsMock).not.toHaveBeenCalled();
  });

  it("decodes a percent-encoded route id before GET and names a 404 without a template path", async () => {
    const encoded = "inbox%3Aescalate%3Athread-gds-launch%3Aevt%3Aevt";
    const decoded = "inbox:escalate:thread-gds-launch:evt:evt";
    const notFound = new Error("anx-core request failed");
    notFound.status = 404;
    coreClientMock.getInboxItem.mockRejectedValue(notFound);
    setInboxRoute(encoded);

    const { getByRole } = render(InboxDetailPage);

    await waitFor(() => {
      expect(coreClientMock.getInboxItem).toHaveBeenCalledWith(decoded);
    });
    await waitFor(() => {
      expect(getByRole("alert").textContent).toContain(
        "This item is no longer in the open inbox.",
      );
    });
    expect(getByRole("alert").textContent).not.toContain("{inbox_id}");
  });

  /*
   * A suggested response sends after a short flash, and the reader can open
   * another item inside it. `loadItem` nulls `item` and clears the composer
   * the moment the route changes, so a send that read this page's state at
   * that point answered nothing at all: the response was dropped in silence,
   * with its toast already on screen.
   */
  it("still sends the response it was given when the reader switches items inside the flash", async () => {
    let resolveSecond;
    coreClientMock.getInboxItem.mockImplementation((id) => {
      if (id === "inbox-first") {
        return Promise.resolve({
          item: inboxItem("inbox-first", "First item", {
            response_proposals: ["Ship it", "Hold for review"],
          }),
        });
      }
      // Still loading while the flash ends: `item` is null, which is exactly
      // the moment the dropped send happened.
      return new Promise((resolve) => {
        resolveSecond = resolve;
      });
    });
    coreClientMock.respondInboxItem.mockResolvedValue({
      event: { id: "evt-1" },
    });

    const { getByRole, findByRole } = render(InboxDetailPage);
    await findByRole("heading", { name: "First item" });

    await fireEvent.click(getByRole("button", { name: /Ship it/ }));
    setInboxRoute("inbox-second");
    await waitFor(() => {
      expect(coreClientMock.getInboxItem).toHaveBeenCalledWith("inbox-second");
    });
    await new Promise((resolve) =>
      setTimeout(resolve, PROPOSAL_FLASH_MS + 100),
    );

    /*
     * The answered item itself travels with the response, not just its id:
     * the overlay files it under Handled until core catches up, which is how
     * an answered ask stays out of the open inbox. Captured, so it is the
     * item that was answered rather than the one now on screen.
     */
    expect(get(inboxResponseOverlay)["inbox-first"]).toMatchObject({
      status: "pending",
      item: { id: "inbox-first", title: "First item" },
    });

    await flushInboxResponse();
    expect(coreClientMock.respondInboxItem).toHaveBeenCalledTimes(1);
    const [itemId, request] = coreClientMock.respondInboxItem.mock.calls[0];
    expect(itemId).toBe("inbox-first");
    expect(get(inboxResponseOverlay)["inbox-first"]).toMatchObject({
      status: "committed",
      item: { id: "inbox-first" },
    });
    expect(request).toMatchObject({
      response_text: "Ship it",
      outcome: "answered",
    });
    resolveSecond({ item: inboxItem("inbox-second", "Second item") });
  });

  /*
   * The sender is bound to one workspace and one reader. Capturing it when the
   * response is finally queued aimed a suggestion chosen here at whichever
   * workspace the reader had switched to during the flash: one send, right
   * item id, wrong workspace.
   */
  it("sends to the workspace the answer was written in, not the one switched to", async () => {
    currentOrganizationSlug.set("org-a");
    currentWorkspaceSlug.set("workspace-a");
    authenticatedAgent.set({ agent_id: "human-a", actor_id: "actor-a" });
    coreClientMock.getInboxItem.mockResolvedValue({
      item: inboxItem("inbox-first", "First item", {
        response_proposals: ["Ship it"],
      }),
    });
    coreClientMock.respondInboxItem.mockResolvedValue({
      event: { id: "evt-1" },
    });

    const { getByRole, findByRole } = render(InboxDetailPage);
    await findByRole("heading", { name: "First item" });
    await fireEvent.click(getByRole("button", { name: /Ship it/ }));

    // Inside the flash: the reader switches workspace.
    currentWorkspaceSlug.set("workspace-b");
    await new Promise((resolve) =>
      setTimeout(resolve, PROPOSAL_FLASH_MS + 100),
    );
    await flushInboxResponse();

    expect(coreClientMock.respondInboxItem).toHaveBeenCalledTimes(1);
    expect(coreClientMock.respondInboxItem.mock.calls[0][0]).toBe(
      "inbox-first",
    );
    // One sender, bound where the reader answered.
    expect(coreClientMock.capturedScopes).toEqual([
      ["org-a", "workspace-a", "actor-a"],
    ]);
  });

  it("leaves the item the reader moved to alone when the earlier answer lands", async () => {
    coreClientMock.getInboxItem.mockImplementation((id) =>
      Promise.resolve({
        item: inboxItem(
          id,
          id === "inbox-first" ? "First item" : "Second item",
          { response_proposals: ["Ship it"] },
        ),
      }),
    );
    coreClientMock.respondInboxItem.mockResolvedValue({
      event: { id: "evt-1" },
    });

    const { getByRole, getByLabelText, findByRole } = render(InboxDetailPage);
    await findByRole("heading", { name: "First item" });
    await fireEvent.click(getByRole("button", { name: /Ship it/ }));
    setInboxRoute("inbox-second");
    await findByRole("heading", { name: "Second item" });
    await fireEvent.input(getByLabelText("Your response"), {
      target: { value: "typing on the second item" },
    });
    await new Promise((resolve) =>
      setTimeout(resolve, PROPOSAL_FLASH_MS + 100),
    );

    await flushInboxResponse();
    expect(coreClientMock.respondInboxItem.mock.calls[0][0]).toBe(
      "inbox-first",
    );
    // The first item's answer must not clear the composer the reader is
    // typing in, nor navigate them away from it.
    expect(getByLabelText("Your response").value).toBe(
      "typing on the second item",
    );
    expect(getByRole("heading", { name: "Second item" })).toBeTruthy();
  });
});
