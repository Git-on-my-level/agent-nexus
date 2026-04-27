import { describe, expect, it } from "vitest";

import {
  applyBackingThreadListClientFilters,
  applyThreadListClientFilters,
  applyTopicListClientFilters,
  buildThreadFilterQueryParamsFromThreadListState,
  buildThreadFilterQueryString,
  buildThreadFilterQueryParams,
  buildTopicListSearchString,
  buildTopicListApiQueryParams,
  parseTopicListSearchParams,
} from "../../src/lib/topicFilters.js";

describe("thread filter query builders", () => {
  it("builds stable query string for thread list (state and optional q)", () => {
    const query = buildThreadFilterQueryString({
      state: "active",
    });

    expect(query).toBe("state=active");
    expect(
      buildThreadFilterQueryString({ state: "active", q: " hello " }),
    ).toBe("state=active&q=hello");
  });

  it("builds request query object for listThreads", () => {
    expect(
      buildThreadFilterQueryParams({
        state: "archived",
      }),
    ).toEqual({
      state: "archived",
    });

    expect(buildThreadFilterQueryParams({ state: "" })).toEqual({});
    expect(buildThreadFilterQueryParams({ state: "active", q: "x" })).toEqual({
      state: "active",
      q: "x",
    });
  });

  it("parses list URL: open clears state, ignores legacy filter params", () => {
    const sp = new URLSearchParams(
      "open=1&state=active&priority=p2&stale=true&tag=ops&q=hi",
    );
    expect(parseTopicListSearchParams(sp)).toEqual({
      state: "",
      q: "hi",
      openOnly: true,
    });
  });

  it("parses list URL without open flag", () => {
    expect(
      parseTopicListSearchParams(
        new URLSearchParams("state=archived&q=report"),
      ),
    ).toEqual({
      state: "archived",
      q: "report",
      openOnly: false,
    });
  });

  it("serializes list URL with open, state, and q", () => {
    expect(
      buildTopicListSearchString({
        openOnly: true,
        state: "active",
        q: "needle",
      }),
    ).toBe("open=1&q=needle");
    expect(
      buildTopicListSearchString({
        openOnly: false,
        state: "archived",
        q: "x",
      }),
    ).toBe("state=archived&q=x");
  });

  it("omits state from thread list API query when openOnly (client-side active filter)", () => {
    expect(
      buildThreadFilterQueryParamsFromThreadListState({
        state: "active",
        q: "find",
        openOnly: true,
      }),
    ).toEqual({ q: "find" });
  });

  it("filters threads client-side for open only", () => {
    const threads = [
      { state: "archived" },
      { state: "trashed" },
      { state: "active" },
    ];
    expect(applyThreadListClientFilters(threads, { openOnly: true })).toEqual([
      { state: "active" },
    ]);
  });

  it("backing thread list filters only by lifecycle for openOnly", () => {
    const threads = [
      { id: "a", state: "archived" },
      { id: "b", state: "active" },
    ];
    expect(
      applyBackingThreadListClientFilters(threads, { openOnly: true }),
    ).toEqual([threads[1]]);
    expect(applyBackingThreadListClientFilters(threads, {})).toEqual(threads);
  });

  it("builds GET /topics query from list filter state", () => {
    expect(
      buildTopicListApiQueryParams(
        {
          openOnly: false,
          state: "archived",
          q: "",
        },
        { includeArchived: true },
      ),
    ).toEqual({ include_archived: "true", state: "archived" });
  });

  it("topic list client filters match backing thread openOnly behavior", () => {
    const items = [
      { id: "a", state: "trashed" },
      { id: "b", state: "active" },
    ];
    expect(
      applyTopicListClientFilters(items, { openOnly: true, state: "", q: "" }),
    ).toEqual([items[1]]);
  });
});
