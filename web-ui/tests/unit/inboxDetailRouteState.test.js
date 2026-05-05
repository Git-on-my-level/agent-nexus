// @vitest-environment jsdom
import { cleanup, fireEvent, render, waitFor } from "@testing-library/svelte";
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
}));

vi.mock("$lib/searchHelpers", () => ({
  searchActors: searchActorsMock,
}));

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
  vi.useRealTimers();
  vi.clearAllMocks();
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
});
