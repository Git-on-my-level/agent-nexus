import { afterEach, describe, expect, it, vi } from "vitest";

import {
  normalizeAssigneeIdForRecency,
  orderPickerOptionsByRecent,
  readRecentAssigneeIds,
  RECENT_ASSIGNEE_IDS_KEY,
  touchRecentAssigneeIds,
} from "../../src/lib/recentAssignees.js";

function mockLocalStorage() {
  const store = {};
  return {
    getItem: vi.fn((key) => store[key] ?? null),
    setItem: vi.fn((key, val) => {
      store[key] = String(val);
    }),
    removeItem: vi.fn((key) => {
      delete store[key];
    }),
    clear: vi.fn(() => {
      for (const k of Object.keys(store)) delete store[k];
    }),
    _store: store,
  };
}

describe("recentAssignees", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("normalizeAssigneeIdForRecency strips actor: prefix", () => {
    expect(normalizeAssigneeIdForRecency("actor:foo")).toBe("foo");
    expect(normalizeAssigneeIdForRecency("  actor:bar  ")).toBe("bar");
    expect(normalizeAssigneeIdForRecency("plain")).toBe("plain");
  });

  it("orderPickerOptionsByRecent prefers recent ids in order", () => {
    const opts = [
      { id: "a", title: "A" },
      { id: "b", title: "B" },
      { id: "c", title: "C" },
    ];
    expect(
      orderPickerOptionsByRecent(opts, ["c", "a"]).map((o) => o.id),
    ).toEqual(["c", "a", "b"]);
  });

  it("touchRecentAssigneeIds prepends and dedupes in localStorage", () => {
    const ls = mockLocalStorage();
    vi.stubGlobal("localStorage", ls);
    touchRecentAssigneeIds(["c"]);
    expect(readRecentAssigneeIds()).toEqual(["c"]);
    touchRecentAssigneeIds(["a", "b", "c"]);
    expect(readRecentAssigneeIds()).toEqual(["a", "b", "c"]);
    expect(ls.setItem).toHaveBeenCalled();
    const payload = ls._store[RECENT_ASSIGNEE_IDS_KEY];
    expect(JSON.parse(payload)).toEqual(["a", "b", "c"]);
  });
});
