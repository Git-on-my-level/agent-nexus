import { describe, expect, it } from "vitest";

import {
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

  it("parses list URL: legacy open=1 maps to state active, ignores unrelated params", () => {
    const sp = new URLSearchParams(
      "open=1&state=active&priority=p2&stale=true&tag=ops&q=hi",
    );
    expect(parseTopicListSearchParams(sp)).toEqual({
      state: "active",
      q: "hi",
    });
  });

  it("parses list URL without state as active (default)", () => {
    expect(parseTopicListSearchParams(new URLSearchParams("q=only"))).toEqual({
      state: "active",
      q: "only",
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
    });
  });

  it("serializes list URL: omits state when active; includes q", () => {
    expect(
      buildTopicListSearchString({
        state: "active",
        q: "needle",
      }),
    ).toBe("q=needle");
    expect(
      buildTopicListSearchString({
        state: "archived",
        q: "x",
      }),
    ).toBe("state=archived&q=x");
  });

  it("includes state in thread list API query when filter state is active", () => {
    expect(
      buildThreadFilterQueryParamsFromThreadListState({
        state: "active",
        q: "find",
      }),
    ).toEqual({ state: "active", q: "find" });
  });

  it("builds GET /topics query from list filter state", () => {
    expect(
      buildTopicListApiQueryParams(
        {
          state: "archived",
          q: "",
        },
        { includeArchived: true },
      ),
    ).toEqual({ include_archived: "true", state: "archived" });
  });

  it("buildTopicListApiQueryParams normalizes empty state to active", () => {
    expect(
      buildTopicListApiQueryParams(
        { state: "", q: "" },
        { includeArchived: false },
      ),
    ).toEqual({ state: "active" });
  });

  it("buildTopicListApiQueryParams omits state when include_archived and lifecycle active", () => {
    expect(
      buildTopicListApiQueryParams(
        { state: "active", q: "" },
        { includeArchived: true },
      ),
    ).toEqual({ include_archived: "true" });
  });
});
