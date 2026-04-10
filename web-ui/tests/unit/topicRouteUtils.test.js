import { describe, expect, it } from "vitest";

import {
  boardCardInspectNav,
  boardRowInspectNav,
  boardWorkspaceInspectNav,
  inboxTopicRouteSegment,
  resolveBoardCardThreadIdField,
  topicDetailPathFromRef,
  topicDetailPathFromSubject,
  topicRouteSegmentFromBackingThread,
  topicRouteSegmentFromBoardCardRow,
  topicRouteSegmentFromBoardWorkspace,
  warningInspectNav,
} from "../../src/lib/topicRouteUtils.js";

describe("topicRouteUtils", () => {
  describe("resolveBoardCardThreadIdField", () => {
    it("reads thread_id", () => {
      expect(resolveBoardCardThreadIdField({ thread_id: "a" })).toBe("a");
    });

    it("returns empty when only legacy parent_thread is present", () => {
      expect(resolveBoardCardThreadIdField({ parent_thread: "thread-z" })).toBe(
        "",
      );
    });
  });

  describe("topicRouteSegmentFromBackingThread", () => {
    it("prefers topic_ref topic: id over thread id", () => {
      expect(
        topicRouteSegmentFromBackingThread({
          id: "thread-a",
          topic_ref: "topic:topic-b",
        }),
      ).toBe("topic-b");
    });

    it("returns empty when topic_ref absent (backing-only thread)", () => {
      expect(
        topicRouteSegmentFromBackingThread({
          id: "thread-a",
        }),
      ).toBe("");
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

  describe("boardCardInspectNav", () => {
    it("uses membership.topic_ref for topic kind", () => {
      expect(
        boardCardInspectNav(
          {
            topic_ref: "topic:top-1",
            thread_id: "thread-x",
          },
          { id: "thread-x", topic_ref: "topic:top-2" },
        ),
      ).toEqual({ kind: "topic", segment: "top-1" });
    });

    it("uses first topic: in related_refs", () => {
      expect(
        boardCardInspectNav(
          {
            thread_id: "thread-x",
            related_refs: ["board:b1", "topic:top-from-ref"],
          },
          null,
        ),
      ).toEqual({ kind: "topic", segment: "top-from-ref" });
    });

    it("uses backing thread topic_ref when membership has no topic hint", () => {
      expect(
        boardCardInspectNav(
          { thread_id: "thread-x" },
          {
            id: "thread-x",
            topic_ref: "topic:via-backing",
          },
        ),
      ).toEqual({ kind: "topic", segment: "via-backing" });
    });

    it("uses thread kind when only membership.thread_id", () => {
      expect(boardCardInspectNav({ thread_id: "thread-z" }, null)).toEqual({
        kind: "thread",
        segment: "thread-z",
      });
    });

    it("returns null when only legacy parent_thread is set", () => {
      expect(
        boardCardInspectNav({ parent_thread: "thread-legacy" }, null),
      ).toBe(null);
    });
  });

  describe("topicRouteSegmentFromBoardCardRow", () => {
    it("delegates segment from boardCardInspectNav", () => {
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

    it("returns thread id segment when no topic ref", () => {
      expect(
        topicRouteSegmentFromBoardCardRow({ thread_id: "thread-z" }, null),
      ).toBe("thread-z");
    });
  });

  describe("boardWorkspaceInspectNav", () => {
    it("prefers primary_topic.id as topic kind", () => {
      expect(
        boardWorkspaceInspectNav({
          primary_topic: { id: "pt-1" },
          board: { thread_id: "th-1" },
        }),
      ).toEqual({ kind: "topic", segment: "pt-1" });
    });

    it("prefers topic: in board.refs over primary_topic_ref", () => {
      expect(
        boardWorkspaceInspectNav({
          board: {
            thread_id: "th-1",
            primary_topic_ref: "topic:legacy",
            refs: ["board:b1", "topic:from-refs"],
          },
        }),
      ).toEqual({ kind: "topic", segment: "from-refs" });
    });

    it("reads board.primary_topic_ref when refs omit topic", () => {
      expect(
        boardWorkspaceInspectNav({
          board: {
            thread_id: "th-1",
            primary_topic_ref: "topic:from-ref",
            refs: ["document:doc-1"],
          },
        }),
      ).toEqual({ kind: "topic", segment: "from-ref" });
    });

    it("uses thread kind when only board.thread_id", () => {
      expect(
        boardWorkspaceInspectNav({
          board: { thread_id: "th-only", refs: [] },
        }),
      ).toEqual({ kind: "thread", segment: "th-only" });
    });
  });

  describe("topicRouteSegmentFromBoardWorkspace", () => {
    it("returns segment string for topic workspace", () => {
      expect(
        topicRouteSegmentFromBoardWorkspace({
          primary_topic: { id: "pt-1" },
          board: { thread_id: "th-1" },
        }),
      ).toBe("pt-1");
    });
  });

  describe("boardRowInspectNav", () => {
    it("returns topic kind when primary_topic_ref is topic:", () => {
      expect(
        boardRowInspectNav({
          thread_id: "t1",
          primary_topic_ref: "topic:abc",
          refs: [],
        }),
      ).toEqual({
        kind: "topic",
        segment: "abc",
        display: "topic:abc",
      });
    });

    it("returns thread kind when no topic ref", () => {
      expect(
        boardRowInspectNav({
          thread_id: "th-row",
          refs: [],
        }),
      ).toEqual({
        kind: "thread",
        segment: "th-row",
        display: "th-row",
      });
    });
  });

  describe("warningInspectNav", () => {
    it("prefers topic_id", () => {
      expect(warningInspectNav({ topic_id: "t1", thread_id: "th1" })).toEqual({
        kind: "topic",
        segment: "t1",
      });
    });

    it("uses thread_id when no topic_id", () => {
      expect(warningInspectNav({ thread_id: "th1" })).toEqual({
        kind: "thread",
        segment: "th1",
      });
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
