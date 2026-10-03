import { describe, expect, it } from "vitest";

import { getGameDevStudioSeedData } from "../../scripts/game-dev-studio-seed-data.mjs";
import {
  collectVisualReports,
  DOC_SCAN_CAP,
  formatPartialCount,
  freshnessBuckets,
  humanActorIdSet,
  isHumanNextActor,
  isPreferredDashboardTitle,
  listWorkPages,
  needsYouFromSources,
  orderDocumentsForReportScan,
  selectVisualReports,
  tasksQuery,
  workMatrix,
} from "../../src/lib/overview.js";
import { parseVisualReport } from "../../src/lib/visualReports.js";

const NOW = Date.parse("2026-10-04T00:00:00.000Z");

function task(partial) {
  return {
    ref: partial.ref,
    title: partial.title || partial.ref,
    phase: "ready",
    source: { authority: "nexus" },
    updated_at: "2026-10-01T00:00:00.000Z",
    freshness: { status: "unknown" },
    ...partial,
  };
}

describe("overview work matrix", () => {
  it("counts phase by source and keeps known source order", () => {
    const matrix = workMatrix([
      task({
        ref: "card:a",
        phase: "in_progress",
        source: { authority: "github" },
      }),
      task({
        ref: "card:b",
        phase: "in_progress",
        source: { authority: "github" },
      }),
      task({
        ref: "card:c",
        phase: "blocked",
        source: { authority: "multica" },
      }),
      task({ ref: "card:d", phase: "ready", source: { authority: "nexus" } }),
      task({ ref: "card:e", phase: "review", source: { authority: "" } }),
    ]);
    expect(matrix.rows.map((row) => row.key)).toEqual([
      "nexus",
      "github",
      "multica",
      "",
    ]);
    const github = matrix.rows.find((row) => row.key === "github");
    expect(github.cells.find((cell) => cell.phase === "in_progress")).toEqual({
      phase: "in_progress",
      count: 2,
      href: "/tasks?source=github&phase=in_progress",
    });
    expect(github.cells.find((cell) => cell.phase === "blocked").count).toBe(0);
    expect(matrix.rows.find((row) => row.key === "multica").total).toBe(1);
    expect(matrix.rows.find((row) => row.key === "").label).toBe(
      "Authority unknown",
    );
    expect(tasksQuery({ phase: "blocked" })).toBe("/tasks?phase=blocked");
  });
});

describe("overview freshness", () => {
  it("buckets stale, unknown, and error without treating a failed read as fresh", () => {
    const buckets = freshnessBuckets(
      [
        task({
          ref: "card:stale",
          freshness: {
            status: "stale",
            last_observed_at: "2020-01-01T00:00:00.000Z",
            stale_after_seconds: 60,
          },
        }),
        task({ ref: "card:unknown", freshness: { status: "unknown" } }),
        task({
          ref: "card:error",
          source: { authority: "github" },
          freshness: { status: "error" },
          refresh: { last_error: { code: "permission", message: "denied" } },
        }),
        task({
          ref: "card:fresh",
          freshness: {
            status: "fresh",
            last_observed_at: "2026-10-04T00:00:00.000Z",
            stale_after_seconds: 86_400,
          },
        }),
      ],
      NOW,
    );
    expect(
      buckets.map((bucket) => [bucket.key, bucket.count, bucket.href]),
    ).toEqual([
      ["stale", 1, "/tasks?freshness=stale"],
      ["unknown", 1, "/tasks?freshness=unknown"],
      ["error", 1, "/tasks?freshness=error"],
    ]);
  });
});

describe("overview next actor", () => {
  const humans = humanActorIdSet(
    [{ id: "actor-jordan", tags: ["human"] }],
    [{ actor_id: "actor-pat", principal_kind: "human" }],
  );

  it("counts people and explicit human refs, not unknown ids", () => {
    expect(isHumanNextActor(task({ next_actor: "actor-jordan" }), humans)).toBe(
      true,
    );
    expect(
      isHumanNextActor(task({ next_actor: "actor:actor-pat" }), humans),
    ).toBe(true);
    expect(isHumanNextActor(task({ next_actor: "human:ada" }), humans)).toBe(
      true,
    );
    expect(isHumanNextActor(task({ next_actor: "actor-codex" }), humans)).toBe(
      false,
    );
    expect(isHumanNextActor(task({ next_actor: "" }), humans)).toBe(false);
    expect(isHumanNextActor(task({ next_actor: "actor-jordan" }), null)).toBe(
      false,
    );
  });
});

describe("overview report selection", () => {
  const older = {
    id: "older",
    title: "Notes",
    updated_at: "2026-10-01T00:00:00Z",
    report: { title: "Notes" },
  };
  const newer = {
    id: "newer",
    title: "Weekly notes",
    updated_at: "2026-10-04T00:00:00Z",
    report: { title: "Weekly" },
  };
  const preferred = {
    id: "fleet",
    title: "Fleet Dashboard",
    updated_at: "2026-10-02T00:00:00Z",
    report: { title: "Fleet" },
  };

  it("prefers a Dashboard title over a newer report", () => {
    expect(isPreferredDashboardTitle("Dashboard notes")).toBe(true);
    expect(isPreferredDashboardTitle("fleet dashboard weekly")).toBe(true);
    expect(isPreferredDashboardTitle("Dashboarding")).toBe(false);
    expect(isPreferredDashboardTitle("My Dashboard")).toBe(false);
    expect(
      selectVisualReports([newer, { ...older, report: null }, preferred]).map(
        (entry) => entry.id,
      ),
    ).toEqual(["fleet", "newer"]);
  });

  it("orders the rest newest first", () => {
    expect(
      selectVisualReports([older, newer]).map((entry) => entry.id),
    ).toEqual(["newer", "older"]);
  });

  it("reads a preferred dashboard first and stops once one report is valid", async () => {
    expect(DOC_SCAN_CAP).toBe(20);
    const documents = [
      {
        id: "notes",
        title: "Weekly notes",
        updated_at: "2026-10-04T00:00:00Z",
      },
      {
        id: "fleet",
        title: "Fleet Dashboard",
        updated_at: "2026-10-01T00:00:00Z",
      },
      {
        id: "other",
        title: "Launch notes",
        updated_at: "2026-10-03T00:00:00Z",
      },
    ];
    expect(orderDocumentsForReportScan(documents).map((doc) => doc.id)).toEqual(
      ["fleet", "notes", "other"],
    );
    const reads = [];
    const scanned = await collectVisualReports(documents, async (doc) => {
      reads.push(doc.id);
      return doc.id === "fleet"
        ? { id: doc.id, title: doc.title, report: { title: "Fleet" } }
        : { id: doc.id, title: doc.title, report: null };
    });
    expect(reads).toEqual(["fleet"]);
    expect(scanned.reports.map((entry) => entry.id)).toEqual(["fleet"]);
    expect(scanned.pending.map((doc) => doc.id)).toEqual(["notes", "other"]);
    expect(scanned.scanned).toBe(1);
  });

  it("keeps reading when the preferred document is not a report", async () => {
    const documents = [
      {
        id: "fleet",
        title: "Dashboard notes",
        updated_at: "2026-10-01T00:00:00Z",
      },
      {
        id: "notes",
        title: "Weekly notes",
        updated_at: "2026-10-04T00:00:00Z",
      },
    ];
    const reads = [];
    const scanned = await collectVisualReports(documents, async (doc) => {
      reads.push(doc.id);
      return doc.id === "notes"
        ? { id: doc.id, title: doc.title, report: { title: "Weekly" } }
        : { id: doc.id, title: doc.title, report: null };
    });
    expect(reads).toEqual(["fleet", "notes"]);
    expect(scanned.reports.map((entry) => entry.id)).toEqual(["notes"]);
    expect(scanned.pending).toEqual([]);
  });
});

describe("overview work paging", () => {
  it("follows next_cursor and marks a cap as incomplete", async () => {
    const pages = [
      { work: [{ ref: "a" }, { ref: "b" }], next_cursor: "more" },
      { work: [{ ref: "c" }], next_cursor: "tail" },
    ];
    const seen = [];
    const listed = await listWorkPages(
      (query) => {
        seen.push(query);
        return pages.shift();
      },
      { limit: 2, cap: 3 },
    );
    expect(seen.map((query) => query.limit)).toEqual([2, 1]);
    expect(listed.work.map((row) => row.ref)).toEqual(["a", "b", "c"]);
    expect(listed.truncated).toBe(true);
  });

  it("does not treat a finished cursor as a partial read", async () => {
    const listed = await listWorkPages(async () => ({
      work: [{ ref: "a" }],
      next_cursor: "",
    }));
    expect(listed.truncated).toBe(false);
    expect(listed.work).toHaveLength(1);
    expect(formatPartialCount(4, true)).toBe("4+");
    expect(formatPartialCount(0, true)).toBe("0+");
    expect(formatPartialCount(4, false)).toBe("4");
  });

  it("refuses a response that is not a work list", async () => {
    await expect(listWorkPages(async () => ({ work: null }))).rejects.toThrow(
      /not returned/,
    );
  });
});

describe("overview needs you", () => {
  it("uses the Inbox Needs you rows and does not invent items", () => {
    const preview = needsYouFromSources(
      {
        inboxItems: [
          {
            id: "ask-1",
            status: "open",
            kind: "ask",
            title: "Approve the parry window",
            requester_label: "Gameplay",
            created_at: "2026-10-01T00:00:00.000Z",
          },
        ],
        work: [
          task({
            ref: "card:done",
            title: "Already done",
            phase: "done",
          }),
        ],
      },
      NOW,
    );
    expect(preview.count).toBe(1);
    expect(preview.rows.map((row) => row.title)).toEqual([
      "Approve the parry window",
    ]);
    expect(preview.rows[0].href).toContain("mailbox=needs-you");
    expect(preview.rows[0].href).toContain("item=inbox%3Aask-1");
    expect(preview.truncated).toBe(false);
  });
});

describe("dev seed fleet dashboard", () => {
  it("seeds a preferred visual report document", () => {
    const seed = getGameDevStudioSeedData();
    const document = seed.documents.find(
      (entry) => entry.id === "gds-fleet-dashboard",
    );
    expect(document?.title).toBe("Fleet Dashboard");
    const revisions = seed.documentRevisions["gds-fleet-dashboard"];
    expect(revisions.length).toBeGreaterThanOrEqual(2);
    for (const revision of revisions) {
      const parsed = parseVisualReport(revision.content);
      expect(parsed.report, parsed.errors.join("\n")).toBeTruthy();
    }
  });
});
