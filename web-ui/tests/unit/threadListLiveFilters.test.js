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
    url: new URL("http://localhost/o/local/w/local/threads"),
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
        url: new URL("http://localhost/o/local/w/local/threads"),
        params: {
          organization: "local",
          workspace: "local",
        },
      });
    },
  };
});

const goto = vi.hoisted(() => vi.fn(() => Promise.resolve()));

const coreClientMock = vi.hoisted(() => ({
  listThreads: vi.fn(),
}));

vi.mock("$app/navigation", () => ({
  goto,
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

import ThreadListPage from "../../src/routes/o/[organization]/w/[workspace]/threads/+page.svelte";

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  pageStore.reset();
});

beforeEach(() => {
  pageStore.reset();
  coreClientMock.listThreads.mockResolvedValue({ threads: [] });
});

describe("threads list live filters", () => {
  it("keeps the typed search when a lifecycle box is clicked in the same turn", async () => {
    render(ThreadListPage);
    await waitFor(() => {
      expect(coreClientMock.listThreads).toHaveBeenCalled();
    });

    await fireEvent.click(screen.getByTestId("topics-filters-toggle"));
    const search = screen.getByLabelText("Search");
    await fireEvent.input(search, { target: { value: "needle" } });
    await fireEvent.change(search);
    await fireEvent.click(screen.getByRole("checkbox", { name: "Archived" }));

    expect(goto).toHaveBeenCalled();
    const lastHref = String(goto.mock.calls.at(-1)?.[0] ?? "");
    expect(lastHref).toContain("q=needle");
    expect(lastHref).toContain("state=archived");

    pageStore.set({
      url: new URL(lastHref, "http://localhost"),
      params: {
        organization: "local",
        workspace: "local",
      },
    });
    await waitFor(() => {
      expect(search.value).toBe("needle");
    });
  });
});
