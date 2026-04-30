import { describe, expect, it } from "vitest";

import { HOME_FEED_PRESET } from "../../src/lib/events/eventRows";
import {
  buildEventListApiQuery,
  buildEventListSearchString,
  hasEventListFilters,
  normalizeEventListGroups,
  parseEventListSearchParams,
} from "../../src/lib/eventFilters.js";

describe("event list URL state", () => {
  it("parses event filters from search params", () => {
    const sp = new URLSearchParams();
    sp.set("q", "hello");
    sp.set("type", "message_posted");
    sp.append("event_group", "messages");
    sp.append("event_group", "topics");
    sp.set("backing_scope", "standalone");
    sp.set("topic_id", "topic-1");
    sp.set("actor_id", "actor-1");
    sp.set("since", "2026-01-01");
    sp.set("until", "2026-01-02");
    sp.set("preset", HOME_FEED_PRESET);

    expect(parseEventListSearchParams(sp)).toEqual({
      preset: HOME_FEED_PRESET,
      type: "message_posted",
      event_group: ["messages", "topics"],
      backing_scope: "standalone",
      topic_id: "topic-1",
      actor_id: "actor-1",
      q: "hello",
      since: "2026-01-01",
      until: "2026-01-02",
    });
  });

  it("drops invalid preset and backing_scope values", () => {
    expect(
      parseEventListSearchParams(
        new URLSearchParams("preset=nope&backing_scope=invalid"),
      ),
    ).toEqual({
      preset: "",
      type: "",
      event_group: [],
      backing_scope: "all",
      topic_id: "",
      actor_id: "",
      q: "",
      since: "",
      until: "",
    });
  });

  it("normalizes event groups to taxonomy order and drops unknown keys", () => {
    expect(normalizeEventListGroups(["topics", "bogus", "messages"])).toEqual([
      "messages",
      "topics",
    ]);
  });

  it("serializes filters into a stable search string", () => {
    expect(
      buildEventListSearchString({
        q: "x",
        backing_scope: "backing_only",
        event_group: ["exceptions"],
      }),
    ).toMatch(/(^|&)q=x($|&)/);
    expect(
      buildEventListSearchString({
        q: "x",
        backing_scope: "backing_only",
        event_group: ["exceptions"],
      }),
    ).toContain("backing_scope=backing_only");
    expect(
      buildEventListSearchString({
        q: "x",
        backing_scope: "backing_only",
        event_group: ["exceptions"],
      }),
    ).toContain("event_group=exceptions");
  });

  it("treats home_feed preset as an active filter", () => {
    expect(hasEventListFilters({ preset: HOME_FEED_PRESET })).toBe(true);
    expect(hasEventListFilters({ backing_scope: "all", event_group: [] })).toBe(
      false,
    );
  });

  it("builds API query compatibly with listEvents", () => {
    expect(
      buildEventListApiQuery(
        {
          q: "hello",
          backing_scope: "all",
          event_group: [],
        },
        { cursor: "cur-1", limit: 25 },
      ),
    ).toEqual({
      q: "hello",
      cursor: "cur-1",
      limit: 25,
    });
  });
});
