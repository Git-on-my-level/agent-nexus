import { describe, expect, it } from "vitest";

import {
  buildArtifactKindSummary,
  buildInboxCategorySummary,
  buildTopicHealthSummary,
  selectRecentArtifacts,
  selectRecentlyUpdatedTopics,
} from "../../src/lib/dashboardSummary.js";

describe("dashboard summaries", () => {
  it("summarizes inbox categories in expected order", () => {
    const summary = buildInboxCategorySummary([
      { kind: "escalate" },
      { kind: "ask" },
      { kind: "ask" },
      { kind: "unknown" },
    ]);

    expect(summary).toEqual([
      { category: "escalate", label: "Escalation", count: 1 },
      { category: "ask", label: "Ask", count: 2 },
      { category: "review", label: "Review", count: 0 },
      { category: "unknown", label: "unknown", count: 1 },
    ]);
  });

  it("computes topic health over open topics", () => {
    const summary = buildTopicHealthSummary([
      {
        id: "thread-a",
        state: "active",
      },
      {
        id: "thread-b",
        state: "active",
      },
      {
        id: "thread-c",
        state: "archived",
      },
    ]);

    expect(summary).toEqual({
      totalCount: 3,
      openCount: 2,
    });
  });

  it("does not treat topics without state as open", () => {
    expect(
      buildTopicHealthSummary([
        {
          id: "thread-x",
          archived_at: null,
          trashed_at: null,
        },
      ]),
    ).toEqual({
      totalCount: 1,
      openCount: 0,
    });
  });

  it("sorts recent topics and artifacts by descending timestamp", () => {
    const recentTopics = selectRecentlyUpdatedTopics([
      { id: "thread-1", updated_at: "2025-01-01T00:00:00.000Z" },
      { id: "thread-2", updated_at: "2026-01-01T00:00:00.000Z" },
      { id: "thread-3", updated_at: "2024-01-01T00:00:00.000Z" },
    ]);

    const recentArtifacts = selectRecentArtifacts([
      {
        id: "artifact-1",
        kind: "doc",
        summary: "Alpha",
        created_at: "2025-01-01T00:00:00.000Z",
      },
      {
        id: "artifact-2",
        kind: "doc",
        summary: "Beta",
        created_at: "2026-01-01T00:00:00.000Z",
      },
      {
        id: "artifact-3",
        kind: "doc",
        summary: "Gamma",
        created_at: "2024-01-01T00:00:00.000Z",
      },
    ]);

    expect(recentTopics.map((topic) => topic.id)).toEqual([
      "thread-2",
      "thread-1",
      "thread-3",
    ]);
    expect(recentArtifacts.map((artifact) => artifact.id)).toEqual([
      "artifact-2",
      "artifact-1",
      "artifact-3",
    ]);
  });

  it("deduplicates artifacts superseded by ref, annotating the survivor", () => {
    const artifacts = [
      {
        id: "doc-v1",
        kind: "doc",
        thread_id: "thread-1",
        summary: "My doc",
        refs: ["thread:thread-1"],
        created_at: "2025-01-01T00:00:00.000Z",
      },
      {
        id: "doc-v2",
        kind: "doc",
        thread_id: "thread-1",
        summary: "My doc",
        refs: ["thread:thread-1", "artifact:doc-v1"],
        created_at: "2026-01-01T00:00:00.000Z",
      },
    ];

    const result = selectRecentArtifacts(artifacts);
    expect(result.map((a) => a.id)).toEqual(["doc-v2"]);
    expect(result[0].isUpdate).toBe(true);
    expect(result[0].versionCount).toBe(2);
  });

  it("deduplicates artifacts with the same summary by summary heuristic", () => {
    const artifacts = [
      {
        id: "doc-old",
        kind: "doc",
        thread_id: "thread-1",
        summary: "My doc",
        refs: ["thread:thread-1"],
        created_at: "2025-01-01T00:00:00.000Z",
      },
      {
        id: "doc-new",
        kind: "doc",
        thread_id: "thread-1",
        summary: "My doc",
        refs: ["thread:thread-1"],
        created_at: "2026-01-01T00:00:00.000Z",
      },
    ];

    const result = selectRecentArtifacts(artifacts);
    expect(result.map((a) => a.id)).toEqual(["doc-new"]);
    expect(result[0].isUpdate).toBe(true);
    expect(result[0].versionCount).toBe(2);
  });

  it("excludes trashed artifacts", () => {
    const artifacts = [
      {
        id: "doc-live",
        kind: "doc",
        created_at: "2026-01-01T00:00:00.000Z",
        trashed_at: null,
      },
      {
        id: "doc-dead",
        kind: "doc",
        created_at: "2026-02-01T00:00:00.000Z",
        trashed_at: "2026-02-02T00:00:00.000Z",
      },
    ];

    const result = selectRecentArtifacts(artifacts);
    expect(result.map((a) => a.id)).toEqual(["doc-live"]);
  });

  it("does not suppress a ref to a different kind", () => {
    const artifacts = [
      {
        id: "evidence-1",
        kind: "evidence",
        thread_id: "thread-1",
        summary: "Fix the thing",
        refs: ["thread:thread-1"],
        created_at: "2025-01-01T00:00:00.000Z",
      },
      {
        id: "receipt-1",
        kind: "receipt",
        thread_id: "thread-1",
        summary: "Receipt for fixing the thing",
        refs: ["thread:thread-1", "artifact:evidence-1"],
        created_at: "2026-01-01T00:00:00.000Z",
      },
    ];

    const result = selectRecentArtifacts(artifacts);
    expect(result.map((a) => a.id)).toEqual(["receipt-1", "evidence-1"]);
    expect(result[0].isUpdate).toBe(false);
    expect(result[1].isUpdate).toBe(false);
  });

  it("summarizes artifact kinds", () => {
    const summary = buildArtifactKindSummary([
      { kind: "review" },
      { kind: "review" },
      { kind: "receipt" },
      { kind: "receipt" },
      { kind: "doc" },
    ]);

    expect(summary).toEqual({
      review: 2,
      receipt: 2,
      other: 1,
    });
  });
});
