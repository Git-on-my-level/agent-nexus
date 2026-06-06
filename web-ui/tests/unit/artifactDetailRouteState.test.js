// @vitest-environment jsdom
import { cleanup, render, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const pageStore = vi.hoisted(() => {
  let value = {
    url: new URL("http://localhost/o/local/w/local/artifacts/artifact-first"),
    params: {
      organization: "local",
      workspace: "local",
      artifactId: "artifact-first",
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
        url: new URL(
          "http://localhost/o/local/w/local/artifacts/artifact-first",
        ),
        params: {
          organization: "local",
          workspace: "local",
          artifactId: "artifact-first",
        },
      });
    },
  };
});

const coreClientMock = vi.hoisted(() => ({
  getArtifact: vi.fn(),
  getArtifactContent: vi.fn(),
  archiveArtifact: vi.fn(),
  unarchiveArtifact: vi.fn(),
  trashArtifact: vi.fn(),
  restoreArtifact: vi.fn(),
}));

vi.mock("$app/navigation", () => ({
  goto: vi.fn(),
}));

vi.mock("$app/stores", () => ({
  page: {
    subscribe: pageStore.subscribe,
  },
}));

vi.mock("$lib/coreClient", () => ({
  coreClient: coreClientMock,
}));

import ArtifactDetailPage from "../../src/routes/o/[organization]/w/[workspace]/artifacts/[artifactId]/+page.svelte";

function artifact(id, summary) {
  return {
    id,
    kind: "agent_wake",
    summary,
    created_at: "2026-01-01T00:00:00Z",
    created_by: "agent:test",
    refs: [],
    provenance: {},
  };
}

function setArtifactRoute(id, workspace = "local") {
  pageStore.set({
    url: new URL(`http://localhost/o/local/w/${workspace}/artifacts/${id}`),
    params: {
      organization: "local",
      workspace,
      artifactId: id,
    },
  });
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  localStorage.clear();
  pageStore.reset();
});

beforeEach(() => {
  pageStore.reset();
});

describe("artifact detail route state", () => {
  it("ignores stale content from the previous artifact after navigation", async () => {
    let resolveFirstContent;
    coreClientMock.getArtifact.mockImplementation((id) =>
      Promise.resolve({
        artifact: artifact(
          id,
          id === "artifact-first" ? "First artifact" : "Second artifact",
        ),
      }),
    );
    coreClientMock.getArtifactContent.mockImplementation((id) => {
      if (id === "artifact-first") {
        return new Promise((resolve) => {
          resolveFirstContent = resolve;
        });
      }
      if (id === "artifact-second") {
        return Promise.resolve({
          content: "second artifact content",
          contentType: "text/plain",
        });
      }
      return Promise.reject(new Error(`unexpected artifact ${id}`));
    });

    const { getByText, queryByText } = render(ArtifactDetailPage);

    await waitFor(() => {
      expect(coreClientMock.getArtifactContent).toHaveBeenCalledWith(
        "artifact-first",
      );
    });

    setArtifactRoute("artifact-second");

    await waitFor(() => {
      expect(getByText("Second artifact")).toBeTruthy();
    });
    await waitFor(() => {
      expect(getByText("second artifact content")).toBeTruthy();
    });

    resolveFirstContent({
      content: "first artifact content",
      contentType: "text/plain",
    });
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(queryByText("first artifact content")).not.toBeTruthy();
    expect(getByText("second artifact content")).toBeTruthy();
  });
});
