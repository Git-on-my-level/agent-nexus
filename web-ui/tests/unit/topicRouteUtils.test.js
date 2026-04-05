import { describe, expect, it } from "vitest";

import {
  inboxTopicRouteSegment,
  topicDetailPathFromRef,
  topicDetailPathFromSubject,
  topicRouteSegmentFromBackingThread,
  topicRouteSegmentFromBoardCardRow,
  topicRouteSegmentFromBoardWorkspace,
} from "../../src/lib/topicRouteUtils.js";

describe("topicRouteUtils", () => {
  describe("topicRouteSegmentFromBackingThread", () => {
    it("prefers topic_ref topic: id over thread id", () => {
      expect(
        topicRouteSegmentFromBackingThread({
          id: "thread-a",
          topic_ref: "topic:topic-b",
        }),
      ).toBe("topic-b");
    });

    it("falls back to thread id when topic_ref absent", () => {
      expect(
        topicRouteSegmentFromBackingThread({
          id: "thread-a",
        }),
      ).toBe("thread-a");
    });
  });

  describe("topicDetailPathFromRef", () => {
    it("routes topic refs to topic detail", () => {
      expect(topicDetailPathFromRef("topic:topic-1")).toBe("/topics/topic-1");
    });

    it("routes thread refs through the legacy thread redirect", () => {
      expect(topicDetailPathFromRef("thread:thread-1")).toBe(
        "/threads/thread-1",
      );
    });
  });

  describe("topicDetailPathFromSubject", () => {
    it("prefers explicit topic ids", () => {
      expect(
        topicDetailPathFromSubject({
          topicId: "topic-7",
          threadId: "thread-7",
        }),
      ).toBe("/topics/topic-7");
    });

    it("falls back to thread detail when only a backing thread is known", () => {
      expect(
        topicDetailPathFromSubject({
          threadId: "thread-7",
        }),
      ).toBe("/threads/thread-7");
    });
  });

  describe("topicRouteSegmentFromBoardCardRow", () => {
    it("prefers membership.topic_ref", () => {
      expect(
        topicRouteSegmentFromBoardCardRow(
          {
            topic_ref: "topic:top-1",
            thread_id: "thread-x",
          },
          { id: "thread-x", topic_ref: "topic:top-2" },
        ),
      ).toBe("top-1");
    });

    it("uses first topic: in related_refs", () => {
      expect(
        topicRouteSegmentFromBoardCardRow(
          {
            thread_id: "thread-x",
            related_refs: ["board:b1", "topic:top-from-ref"],
          },
          null,
        ),
      ).toBe("top-from-ref");
    });

    it("uses backing thread topic_ref when membership has no topic hint", () => {
      expect(
        topicRouteSegmentFromBoardCardRow(
          { thread_id: "thread-x" },
          { id: "thread-x", topic_ref: "topic:via-backing" },
        ),
      ).toBe("via-backing");
    });

    it("falls back to thread_id", () => {
      expect(
        topicRouteSegmentFromBoardCardRow({ thread_id: "thread-z" }, null),
      ).toBe("thread-z");
    });
  });

  describe("topicRouteSegmentFromBoardWorkspace", () => {
    it("prefers primary_topic.id", () => {
      expect(
        topicRouteSegmentFromBoardWorkspace({
          primary_topic: { id: "pt-1" },
          board: { thread_id: "th-1" },
        }),
      ).toBe("pt-1");
    });

    it("reads board.primary_topic_ref", () => {
      expect(
        topicRouteSegmentFromBoardWorkspace({
          board: {
            thread_id: "th-1",
            primary_topic_ref: "topic:from-ref",
          },
        }),
      ).toBe("from-ref");
    });
  });

  describe("inboxTopicRouteSegment", () => {
    it("prefers topic_id over thread subject_ref", () => {
      expect(
        inboxTopicRouteSegment({
          topic_id: "topic-alpha",
          thread_id: "thread-beta",
          subject_ref: "thread:thread-beta",
        }),
      ).toBe("topic-alpha");
    });

    it("parses explicit subject_ref topic", () => {
      expect(
        inboxTopicRouteSegment({
          subject_ref: "topic:top-99",
        }),
      ).toBe("top-99");
    });

    it("does not treat bare thread identity as a topic route segment", () => {
      expect(
        inboxTopicRouteSegment({
          thread_id: "thread-only",
        }),
      ).toBe("");
    });
  });
});
