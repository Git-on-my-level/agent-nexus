import { describe, expect, it } from "vitest";
import { get } from "svelte/store";

import { createPageTitleState } from "../../src/lib/pageTitle.svelte.js";

describe("createPageTitleState", () => {
  it("is empty until a page claims the title", () => {
    const state = createPageTitleState();
    expect(get(state.title)).toBe("");
    state.set({}, "Tasks · Agent Nexus");
    expect(get(state.title)).toBe("Tasks · Agent Nexus");
  });

  it("clears when the owning page releases it", () => {
    const state = createPageTitleState();
    const page = {};
    state.set(page, "Settings · Agent Nexus");
    state.release(page);
    expect(get(state.title)).toBe("");
  });

  it("keeps the next page's title when the previous page tears down late", () => {
    const state = createPageTitleState();
    const previous = {};
    const next = {};
    state.set(previous, "Tasks · Agent Nexus");
    state.set(next, "Release checklist · Agent Nexus");
    state.release(previous);
    expect(get(state.title)).toBe("Release checklist · Agent Nexus");
  });
});
