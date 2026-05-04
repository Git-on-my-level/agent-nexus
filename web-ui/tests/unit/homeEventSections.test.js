import { describe, expect, it } from "vitest";

import {
  BOARD_HOME_EVENT_TYPES,
  DOCUMENT_HOME_EVENT_TYPES,
  TOPIC_HOME_EVENT_TYPES,
  filterEventsForHomeSection,
} from "../../src/lib/homeEventSections.js";
import { HOME_FEED_EVENT_TYPES } from "../../src/lib/events/eventRows.js";

describe("Home event section filters", () => {
  it("keeps Home feed event rows aligned with section display filters", () => {
    const displayed = new Set([
      ...TOPIC_HOME_EVENT_TYPES,
      ...BOARD_HOME_EVENT_TYPES,
      ...DOCUMENT_HOME_EVENT_TYPES,
    ]);

    expect([...displayed].sort()).toEqual([...HOME_FEED_EVENT_TYPES].sort());
  });

  it("renders board-group messages so unread counts match visible rows", () => {
    expect(BOARD_HOME_EVENT_TYPES.has("message_posted")).toBe(true);

    const visible = filterEventsForHomeSection(
      [
        { id: "evt-message", type: "message_posted" },
        { id: "evt-moved", type: "card_moved" },
        { id: "evt-exception", type: "exception_raised" },
      ],
      BOARD_HOME_EVENT_TYPES,
    );

    expect(visible.map((event) => event.id)).toEqual([
      "evt-message",
      "evt-moved",
    ]);
  });

  it("keeps document sections scoped to document lifecycle rows", () => {
    const visible = filterEventsForHomeSection(
      [
        { id: "evt-message", type: "message_posted" },
        { id: "evt-doc", type: "document_revised" },
      ],
      DOCUMENT_HOME_EVENT_TYPES,
    );

    expect(visible.map((event) => event.id)).toEqual(["evt-doc"]);
  });
});
