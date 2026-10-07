import { describe, expect, it, vi } from "vitest";

import { createWorkspaceResourceLifecycleController } from "../../src/lib/workspaceResourceLifecycle.svelte.js";

/**
 * Bulk archive / unarchive / trash across a list selection.
 *
 * The run is not atomic: it stops at the first failure with everything before
 * it already applied. What the reader is owed in that case is the one sentence
 * saying so — and that sentence has to survive the reload the controller
 * itself triggers, because list loaders clear the page error as they start.
 */

/** A controller over `items`, recording what the page was told. */
function controllerOver(items, actions) {
  const state = { errors: [], reloads: 0, cleared: 0 };
  const controller = createWorkspaceResourceLifecycleController({
    resourceSingular: "document",
    resourcePlural: "documents",
    selectedItems: () => items,
    idFor: (item) => item.id,
    isArchived: (item) => item.state === "archived",
    isTrashed: (item) => item.state === "trashed",
    actions,
    reload: async () => {
      state.reloads += 1;
      // What a real list loader does on the way in.
      state.errors.push("");
    },
    clearSelection: () => {
      state.cleared += 1;
    },
    setError: (message) => state.errors.push(message),
  });
  return { controller, state };
}

/** The message the page is left showing. */
const visibleError = (state) => state.errors.at(-1);

const DOCS = [
  { id: "doc-1", state: "active" },
  { id: "doc-2", state: "active" },
];

describe("bulk lifecycle writes", () => {
  it("leaves the partial-failure message on screen after the reload", async () => {
    const archive = vi
      .fn()
      .mockResolvedValueOnce({})
      .mockRejectedValueOnce(new Error("500 internal_error"));
    const { controller, state } = controllerOver(DOCS, { archive });

    await controller.runBulk("archive", ["doc-1", "doc-2"]);

    expect(archive).toHaveBeenCalledTimes(2);
    expect(state.reloads).toBe(1);
    /*
     * The blocker: the reload ran last and wiped this, so one document had
     * vanished from the list with nothing on screen saying why the other had
     * not.
     */
    expect(visibleError(state)).toBe(
      "Archive stopped after 1 of 2: 500 internal_error",
    );
  });

  it("names how far it got, so a half-applied run is not read as nothing", async () => {
    const trash = vi.fn().mockRejectedValue(new Error("500 internal_error"));
    const { controller, state } = controllerOver(DOCS, { trash });

    await controller.runBulk("trash", ["doc-1", "doc-2"]);

    // Nothing applied: no count to report, and the plain sentence is right.
    expect(visibleError(state)).toBe("Trash failed: 500 internal_error");
  });

  it("clears the error and the selection when everything applies", async () => {
    const archive = vi.fn().mockResolvedValue({});
    const { controller, state } = controllerOver(DOCS, { archive });

    await controller.runBulk("archive", ["doc-1", "doc-2"]);

    expect(archive).toHaveBeenCalledTimes(2);
    expect(state.cleared).toBe(1);
    expect(visibleError(state)).toBe("");
  });

  it("re-reads the list either way, so the screen matches the server", async () => {
    const archive = vi
      .fn()
      .mockResolvedValueOnce({})
      .mockRejectedValueOnce(new Error("500 internal_error"));
    const { controller, state } = controllerOver(DOCS, { archive });

    await controller.runBulk("archive", ["doc-1", "doc-2"]);
    expect(state.reloads).toBe(1);
    // The selection survives a failure: the reader still has the run in hand.
    expect(state.cleared).toBe(0);
  });

  it("reports a failing reload without claiming the write failed", async () => {
    const archive = vi.fn().mockResolvedValue({});
    const { state } = controllerOver(DOCS, { archive });
    const broken = createWorkspaceResourceLifecycleController({
      resourceSingular: "document",
      resourcePlural: "documents",
      selectedItems: () => DOCS,
      idFor: (item) => item.id,
      isArchived: () => false,
      isTrashed: () => false,
      actions: { archive },
      reload: async () => {
        throw new Error("list read failed");
      },
      clearSelection: () => {},
      setError: (message) => state.errors.push(message),
    });

    await broken.runBulk("archive", ["doc-1", "doc-2"]);
    // The writes landed; the reload's own failure is the list loader's to say.
    expect(visibleError(state)).toBe("");
  });
});
