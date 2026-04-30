import { describe, expect, it } from "vitest";

import {
  HOME_FEED_PRESET,
  isHomeFeedEvent,
  normalizeEventRow,
} from "../../src/lib/events/eventRows.js";

describe("event row helpers", () => {
  const workspaceHref = (path) => `/w${path}`;

  it("normalizes Home-eligible message rows from payload body", () => {
    const row = normalizeEventRow(
      {
        id: "evt-1",
        type: "message_posted",
        actor_id: "actor-1",
        refs: ["topic:topic-1"],
        payload: { body: "First line\nSecond line\nThird line" },
      },
      { workspaceHref },
    );

    expect(HOME_FEED_PRESET).toBe("home_feed");
    expect(isHomeFeedEvent(row.event)).toBe(true);
    expect(row.label).toBe("Message");
    expect(row.detail).toBe("First line\nSecond line");
    expect(row.href).toBe("/w/topics/topic-1");
  });

  it("keeps unknown events inspectable for Events", () => {
    const row = normalizeEventRow(
      {
        id: "evt-x",
        type: "future_event",
        summary: "Raw summary",
        refs: ["custom:1"],
      },
      { workspaceHref },
    );

    expect(row.homeEligible).toBe(false);
    expect(row.label).toBe("future_event");
    expect(row.detail).toBe("Raw summary");
    expect(row.href).toBe("/w/events#evt-x");
  });
});
