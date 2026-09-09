// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const pageStore = vi.hoisted(() => {
  let value = {
    url: new URL("http://localhost/o/local/w/local/docs"),
    params: {
      organization: "local",
      workspace: "local",
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
        url: new URL("http://localhost/o/local/w/local/docs"),
        params: {
          organization: "local",
          workspace: "local",
        },
      });
    },
  };
});

const coreClientMock = vi.hoisted(() => ({
  archiveDocument: vi.fn(),
  createDocument: vi.fn(),
  listDocuments: vi.fn(),
  searchDocuments: vi.fn(),
  trashDocument: vi.fn(),
  unarchiveDocument: vi.fn(),
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

import DocsListPage from "../../src/routes/o/[organization]/w/[workspace]/docs/+page.svelte";

function doc(id, title, extra = {}) {
  return {
    id,
    title,
    summary: "",
    state: "active",
    updated_at: "2026-05-05T00:00:00Z",
    head_revision_number: 2,
    ...extra,
  };
}

async function submitSearch(query) {
  const input = screen.getByLabelText("Search documents");
  await fireEvent.input(input, { target: { value: query } });
  await fireEvent.submit(input.closest("form"));
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

beforeEach(() => {
  pageStore.reset();
});

describe("docs list search", () => {
  it("lists via docs.list with the state filter when the query is empty", async () => {
    coreClientMock.listDocuments.mockResolvedValue({ documents: [] });

    render(DocsListPage);

    await waitFor(() => {
      expect(coreClientMock.listDocuments).toHaveBeenCalledWith({
        state: ["active"],
      });
    });
    expect(coreClientMock.searchDocuments).not.toHaveBeenCalled();
  });

  it("routes a non-empty query through searchDocuments with only q", async () => {
    coreClientMock.listDocuments.mockResolvedValue({ documents: [] });
    coreClientMock.searchDocuments.mockResolvedValue({ documents: [] });

    render(DocsListPage);
    await waitFor(() => {
      expect(coreClientMock.listDocuments).toHaveBeenCalled();
    });

    await submitSearch("runbook");

    await waitFor(() => {
      expect(coreClientMock.searchDocuments).toHaveBeenCalledWith({
        q: "runbook",
      });
    });
    expect(coreClientMock.listDocuments).toHaveBeenCalledTimes(1);
  });

  it("shows a search-specific empty state, not an error", async () => {
    coreClientMock.listDocuments.mockResolvedValue({ documents: [] });
    coreClientMock.searchDocuments.mockResolvedValue({ documents: [] });

    render(DocsListPage);
    await submitSearch("nope");

    await waitFor(() => {
      expect(screen.getByText("No matching docs")).toBeTruthy();
    });
    expect(screen.queryByText("No docs yet")).not.toBeTruthy();
  });

  it("renders source, knowledge tag, quiet tags, and last comment on rows", async () => {
    coreClientMock.listDocuments.mockResolvedValue({
      documents: [
        doc("doc-1", "Launch checklist", {
          source: "https://example.com/spec",
          tags: ["knowledge", "ops"],
          timeline_message_count: 3,
          last_comment: {
            body: "Confirmed the rollout plan with the platform team.",
            created_at: "2026-05-04T00:00:00Z",
          },
        }),
      ],
    });

    render(DocsListPage);

    await waitFor(() => {
      expect(screen.getByText("Launch checklist")).toBeTruthy();
    });

    const sourceLink = screen.getByText("https://example.com/spec");
    expect(sourceLink.closest("a")?.getAttribute("href")).toBe(
      "https://example.com/spec",
    );
    expect(sourceLink.closest("a")?.getAttribute("target")).toBe("_blank");
    expect(screen.getByText("Knowledge")).toBeTruthy();
    expect(screen.getByText("ops")).toBeTruthy();
    expect(screen.getByText("Last comment")).toBeTruthy();
    expect(
      screen.getByText("Confirmed the rollout plan with the platform team."),
    ).toBeTruthy();
  });

  it("renders a non-URL source as plain text without a link", async () => {
    coreClientMock.listDocuments.mockResolvedValue({
      documents: [doc("doc-2", "Typed ref doc", { source: "document:launch" })],
    });

    render(DocsListPage);

    await waitFor(() => {
      expect(screen.getByText("document:launch").closest("a")).toBeNull();
    });
  });
});
