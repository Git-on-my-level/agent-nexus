// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import InboxDocPanel from "../../src/lib/components/inbox/InboxDocPanel.svelte";
import { selectedActorId } from "../../src/lib/actorSession.js";
import {
  currentOrganizationSlug,
  currentWorkspaceSlug,
} from "../../src/lib/workspaceContext.js";

/**
 * An evidence document read beside the question.
 *
 * Core authorizes every read, so a document this panel has read belongs to the
 * reader who read it. A read already in flight when the panel closes, or when
 * the acting reader changes, must not land: showing it would show one reader
 * another reader's document, remembering it as loaded would serve that content
 * again without a fresh read, and handing its title back would label the
 * document for a reader who cannot open it.
 */

const getDocument = vi.fn();

vi.mock("../../src/lib/coreClient", () => ({
  coreClient: {
    getDocument: (...args) => getDocument(...args),
  },
}));

/** A `getDocument` whose promise this test resolves by hand. */
function deferred() {
  let settle;
  const promise = new Promise((resolve) => {
    settle = resolve;
  });
  return { promise, settle };
}

function doc(title, content) {
  return {
    document: { id: "rulings", title },
    revision: { content, content_type: "text" },
  };
}

beforeEach(() => {
  getDocument.mockReset();
  currentOrganizationSlug.set("acme");
  currentWorkspaceSlug.set("ops");
  selectedActorId.set("actor-a");
});

afterEach(() => {
  cleanup();
});

describe("InboxDocPanel", () => {
  it("renders the document it read and reports its title once", async () => {
    getDocument.mockResolvedValue(doc("Rulings", "Monday to Thursday."));
    const onTitle = vi.fn();
    const view = render(InboxDocPanel, {
      props: { ref: "document:rulings", onTitle },
    });
    await vi.waitFor(() => expect(onTitle).toHaveBeenCalledTimes(1));
    expect(onTitle).toHaveBeenCalledWith("document:rulings", "Rulings");
    expect(view.container.textContent).toContain("Monday to Thursday.");
  });

  it("drops a read that resolves after the panel closed", async () => {
    const pending = deferred();
    getDocument.mockImplementationOnce(() => pending.promise);
    const onTitle = vi.fn();
    const view = render(InboxDocPanel, {
      props: { ref: "document:rulings", onTitle },
    });
    await vi.waitFor(() => expect(getDocument).toHaveBeenCalledTimes(1));

    // Closed while the read is still out.
    await view.rerender({ ref: "", onTitle });
    pending.settle(doc("Rulings", "Monday to Thursday."));
    await Promise.resolve();
    await Promise.resolve();

    expect(onTitle).not.toHaveBeenCalled();
    expect(view.container.textContent).not.toContain("Monday to Thursday.");

    // And the dropped read was not remembered: reopening reads again.
    getDocument.mockResolvedValue(doc("Rulings", "Friday only."));
    await view.rerender({ ref: "document:rulings", onTitle });
    await vi.waitFor(() => expect(getDocument).toHaveBeenCalledTimes(2));
    await vi.waitFor(() =>
      expect(view.container.textContent).toContain("Friday only."),
    );
  });

  it("drops a read that resolves after the acting reader changed", async () => {
    const pending = deferred();
    // The first reader's read is the one left hanging; the second reader's is a
    // separate call that never settles, so only the stale result can land.
    getDocument
      .mockImplementationOnce(() => pending.promise)
      .mockImplementation(() => new Promise(() => {}));
    const onTitle = vi.fn();
    const view = render(InboxDocPanel, {
      props: { ref: "document:rulings", onTitle },
    });
    await vi.waitFor(() => expect(getDocument).toHaveBeenCalledTimes(1));

    selectedActorId.set("actor-b");
    // The reader change re-reads for whoever is acting now.
    await vi.waitFor(() => expect(getDocument).toHaveBeenCalledTimes(2));

    pending.settle(doc("Reader A's title", "Reader A's content."));
    await Promise.resolve();
    await Promise.resolve();

    expect(onTitle).not.toHaveBeenCalled();
    expect(view.container.textContent).not.toContain("Reader A's content.");
  });

  it("re-reads the same document for a new reader rather than reusing it", async () => {
    getDocument.mockResolvedValue(doc("Rulings", "Monday to Thursday."));
    const onTitle = vi.fn();
    render(InboxDocPanel, { props: { ref: "document:rulings", onTitle } });
    await vi.waitFor(() => expect(getDocument).toHaveBeenCalledTimes(1));

    currentWorkspaceSlug.set("other");
    await vi.waitFor(() => expect(getDocument).toHaveBeenCalledTimes(2));
  });

  it("reads one document once while it stays open", async () => {
    getDocument.mockResolvedValue(doc("Rulings", "Monday to Thursday."));
    const view = render(InboxDocPanel, {
      props: { ref: "document:rulings" },
    });
    await vi.waitFor(() => expect(getDocument).toHaveBeenCalledTimes(1));
    await view.rerender({ ref: "document:rulings", hrefFor: () => "/docs/x" });
    await Promise.resolve();
    expect(getDocument).toHaveBeenCalledTimes(1);
  });

  it("says a document could not be read rather than showing nothing", async () => {
    getDocument.mockRejectedValue(new Error("403"));
    const onTitle = vi.fn();
    const view = render(InboxDocPanel, {
      props: { ref: "document:rulings", onTitle },
    });
    await vi.waitFor(() =>
      expect(view.container.textContent).toContain(
        "This document could not be read.",
      ),
    );
    expect(onTitle).not.toHaveBeenCalled();
  });
});
