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
  it("builds stable query string for thread list (states and optional q)", () => {
    expect(
      buildThreadFilterQueryString({
        states: ["active"],
      }),
    ).toBe("state=active");
    expect(
      buildThreadFilterQueryString({
        states: ["active", "archived"],
        q: " hello ",
      }),
    ).toBe("state=active&state=archived&q=hello");
  });

  it("builds request query object for listThreads", () => {
    expect(
      buildThreadFilterQueryParams({
        states: ["archived"],
      }),
    ).toEqual({
      state: ["archived"],
    });

    expect(buildThreadFilterQueryParams({})).toEqual({ state: ["active"] });
    expect(
      buildThreadFilterQueryParams({ states: ["active"], q: "x" }),
    ).toEqual({
      state: ["active"],
      q: "x",
    });
  });

  it("parses list URL: legacy open=1 maps to states active", () => {
    const sp = new URLSearchParams(
      "open=1&state=active&priority=p2&stale=true&tag=ops&q=hi",
    );
    expect(parseTopicListSearchParams(sp)).toEqual({
      states: ["active"],
      q: "hi",
    });
  });

  it("parses list URL without state as active (default)", () => {
    expect(parseTopicListSearchParams(new URLSearchParams("q=only"))).toEqual({
      states: ["active"],
      q: "only",
    });
  });

  it("parses repeated state values", () => {
    expect(
      parseTopicListSearchParams(
        new URLSearchParams("state=active&state=archived&q=report"),
      ),
    ).toEqual({
      states: ["active", "archived"],
      q: "report",
    });
  });

  it("serializes list URL: omits state when only active; includes q", () => {
    expect(
      buildTopicListSearchString({
        states: ["active"],
        q: "needle",
      }),
    ).toBe("q=needle");
    expect(
      buildTopicListSearchString({
        states: ["archived"],
        q: "x",
      }),
    ).toBe("state=archived&q=x");
  });

  it("includes repeated state in thread list API query", () => {
    expect(
      buildThreadFilterQueryParamsFromThreadListState({
        states: ["active", "archived"],
        q: "find",
      }),
    ).toEqual({ state: ["active", "archived"], q: "find" });
  });

  it("buildTopicListApiQueryParams mirrors listTopics contract", () => {
    expect(
      buildTopicListApiQueryParams({
        states: ["archived"],
        q: "",
      }),
    ).toEqual({ state: ["archived"] });
  });

  it("buildTopicListApiQueryParams defaults to active", () => {
    expect(buildTopicListApiQueryParams({ q: "" })).toEqual({
      state: ["active"],
    });
  });
});
