import { describe, expect, it } from "vitest";
import {
  freshness,
  safeSourceHref,
  receiptSignal,
  phaseGroups,
  filterWork,
  decisionTitle,
  decisionPayload,
} from "../../src/lib/pm/presentation.js";

describe("PM evidence presentation", () => {
  const now = Date.parse("2026-09-01T12:00:00Z");
  it("does not call a recent check progress or invent freshness without a policy", () => {
    expect(freshness({ observedAt: "2026-09-01T11:59:00Z" }, now).key).toBe(
      "unknown",
    );
    expect(freshness({ staleAfter: "2026-09-01T13:00:00Z" }, now).key).toBe(
      "unknown",
    );
    expect(
      freshness(
        {
          observedAt: "2026-09-01T11:59:00Z",
          staleAfter: "2026-09-01T13:00:00Z",
        },
        now,
      ).key,
    ).toBe("fresh");
  });
  it("keeps failure and stale last-good evidence visible", () => {
    expect(
      freshness(
        {
          observedAt: "2026-09-01T10:00:00Z",
          staleAfter: "2026-09-01T11:00:00Z",
        },
        now,
      ).key,
    ).toBe("stale");
    expect(
      freshness(
        {
          observedAt: "2026-09-01T10:00:00Z",
          staleAfter: "2026-09-01T13:00:00Z",
          error: "Access denied",
        },
        now,
      ),
    ).toMatchObject({
      key: "error",
      label: "Can't reach source · last good read kept",
    });
    // The label names the source when we know which one we could not reach.
    expect(
      freshness(
        {
          observedAt: "2026-09-01T10:00:00Z",
          staleAfter: "2026-09-01T13:00:00Z",
          error: "Access denied",
          sourceName: "GitHub",
        },
        now,
      ),
    ).toMatchObject({
      key: "error",
      label: "Can't reach GitHub · last good read kept",
      kept: true,
    });
    // Once the window has passed the failure is the reader's problem.
    expect(
      freshness(
        {
          observedAt: "2026-09-01T10:00:00Z",
          staleAfter: "2026-09-01T11:00:00Z",
          error: "Access denied",
        },
        now,
      ),
    ).toMatchObject({ key: "error", kept: false });
  });
  it("shows malformed dates and future-clock observations as unknown", () => {
    expect(
      freshness({ observedAt: "invalid", staleAfter: "invalid" }, now).key,
    ).toBe("unknown");
    expect(
      freshness(
        {
          observedAt: "2026-09-01T13:00:00Z",
          staleAfter: "2026-09-01T14:00:00Z",
        },
        now,
      ).key,
    ).toBe("unknown");
  });
  it("only offers absolute http(s) source links", () => {
    for (const value of [
      "javascript:alert(1)",
      "data:text/html,test",
      "/work",
      "file:///tmp/a",
      "https://user:secret@example.test/",
    ])
      expect(safeSourceHref(value)).toBe("");
    expect(safeSourceHref("https://example.test/issues/1")).toBe(
      "https://example.test/issues/1",
    );
  });
  it("shows four primary receipt states and folds the rest", () => {
    expect(receiptSignal("awaiting_answer")).toMatchObject({
      label: "Needs you",
      primary: true,
      verified: false,
    });
    expect(receiptSignal("delivered")).toMatchObject({
      label: "Delivered",
      primary: true,
      verified: false,
    });
    expect(receiptSignal("verified")).toMatchObject({
      label: "Done",
      primary: true,
      verified: true,
    });
    expect(receiptSignal("failed")).toMatchObject({
      label: "Failed",
      primary: true,
      verified: false,
    });
    expect(receiptSignal("acknowledged").verified).toBe(false);
    expect(receiptSignal("acknowledged").verified).toBe(false);
    expect(receiptSignal("pending_delivery").primary).toBe(false);
    expect(receiptSignal("new_remote_state")).toMatchObject({
      label: "new_remote_state",
      verified: false,
      primary: false,
    });
  });
  it("board and table retain the same records and unfamiliar phases", () => {
    const records = [
      { id: "a", phase: "active", title: "One" },
      { id: "b", phase: "vendor_pause", title: "Two" },
      { id: "c", title: "Three" },
    ];
    const filtered = filterWork(records, { q: "" });
    expect(
      phaseGroups(filtered)
        .flatMap((group) => group.items)
        .map((item) => item.id)
        .sort(),
    ).toEqual(["a", "b", "c"]);
    expect(
      phaseGroups(filtered).some((group) => group.key === "vendor_pause"),
    ).toBe(true);
    expect(filterWork(records, { q: "two" }).map((item) => item.id)).toEqual([
      "b",
    ]);
  });
  it("never titles a decision with a JSON blob", () => {
    expect(
      decisionTitle({
        instruction: '{"next_action":"Ship the release","scope":"repo"}',
      }),
    ).toBe("Ship the release");
    expect(decisionTitle({ instruction: '{"title":"Cut 2.1","x":1}' })).toBe(
      "Cut 2.1",
    );
    expect(
      decisionTitle({ instruction: '{"work_ref_only":true}' }, "Release"),
    ).toBe("Release");
    expect(decisionTitle({ instruction: "[1,2]" }, "Release")).toBe("Release");
    expect(decisionTitle({ instruction: "not json { still plain" })).toBe(
      "Not json { still plain",
    );
    expect(decisionTitle({}, "Release")).toBe("Release");
    expect(decisionTitle({})).toBe("Decision");
  });
  it("titles a note with the field it sets", () => {
    expect(
      decisionTitle({
        scope: "work.annotate",
        instruction: '{"priority":"high"}',
      }),
    ).toBe("Priority: high");
    expect(
      decisionTitle({
        scope: "work.annotate",
        instruction: '{"next_action":"Ship the release"}',
      }),
    ).toBe("Next action: Ship the release");
    // Phase proposals keep the summary-key title.
    expect(
      decisionTitle({
        scope: "work.phase",
        instruction: '{"summary":"Move to review","phase":"review"}',
      }),
    ).toBe("Move to review");
  });
  it("exposes structured instructions as payloads, plain text stays a title", () => {
    expect(decisionPayload({ instruction: '{"a":1}' })).toBe('{\n  "a": 1\n}');
    expect(decisionPayload({ instruction: "plain text" })).toBe("");
    expect(decisionPayload({})).toBe("");
  });
});

describe("structured decisions read as sentences", () => {
  it("lists labelled fields and leads with the next action", async () => {
    const { decisionFields, decisionSummary } =
      await import("$lib/pm/presentation.js");
    const item = {
      instruction: JSON.stringify({
        next_action: "A/B the alert ducking curve",
        next_actor: "audio lead",
        blockers: ["booth booked", "VO absent"],
      }),
      scope: "work.annotate",
    };
    expect(decisionFields(item)).toEqual([
      { label: "Next action", value: "A/B the alert ducking curve" },
      { label: "Next actor", value: "audio lead" },
      { label: "Blockers", value: "booth booked; VO absent" },
    ]);
    expect(decisionSummary(item, "Balance hub ambience")).toEqual({
      title: "Balance hub ambience",
      ask: "Sets next action to “A/B the alert ducking curve”",
    });
  });
});

describe("phase-change decisions lead with the phase", () => {
  it("names the target phase before the source so truncated rows stay distinct", async () => {
    const { decisionSummary } = await import("$lib/pm/presentation.js");
    const item = {
      scope: "work.phase",
      instruction: "request status change at Nexus to Blocked",
      payload: { phase: "blocked" },
    };
    expect(decisionSummary(item, "Tune core combat loop")).toEqual({
      title: "Tune core combat loop",
      ask: "Move to Blocked at Nexus",
    });
    expect(
      decisionSummary({ ...item, instruction: "move it" }, "Tune").ask,
    ).toBe("Move to Blocked");
  });
});

describe("a full PM queue reads as a queue, not a runner limit", () => {
  it("names the queue and keeps the draft", async () => {
    const { errorMessage } = await import("$lib/pm/presentation.js");
    const err = new Error("Workspace PM queue is full (20 waiting; limit 20)");
    err.status = 429;
    err.body = {
      error: {
        code: "busy",
        details: { reason: "queue", queued: 20, limit: 20 },
      },
    };
    expect(errorMessage(err)).toBe(
      "The PM queue for this workspace is full (limit 20). Your message is kept; send it again in a moment, once a waiting question is answered or expires.",
    );
  });
  it("does not print a queue past its limit as a count", async () => {
    const { errorMessage } = await import("$lib/pm/presentation.js");
    const err = new Error("Workspace PM queue is full (21 waiting; limit 20)");
    err.status = 429;
    err.body = {
      error: {
        code: "busy",
        details: { reason: "queue", queued: 21, limit: 20 },
      },
    };
    expect(errorMessage(err)).not.toContain("21 of 20");
    expect(errorMessage(err)).toContain("(limit 20)");
  });
});

describe("instants in core's messages read as times, not ISO strings", () => {
  it("replaces RFC 3339 instants and leaves other text alone", async () => {
    const { humanizeInstants } = await import("$lib/pm/presentation.js");
    const soon = new Date(Date.now() + 4 * 60_000).toISOString();
    expect(humanizeInstants(`rate limited; next attempt at ${soon}`)).toBe(
      "rate limited; next attempt in 4m",
    );
    const ago = new Date(Date.now() - 3 * 60_000).toISOString();
    expect(humanizeInstants(`last read ${ago}`)).toBe("last read 3m ago");
    expect(humanizeInstants("no dates here")).toBe("no dates here");
    expect(humanizeInstants(undefined)).toBe("");
  });
});

describe("read errors are explained in the reader's words", () => {
  it("maps known codes to a sentence and falls back to the message", async () => {
    const { readErrorExplanation } = await import("$lib/pm/presentation.js");
    expect(
      readErrorExplanation({
        code: "policy_denied",
        message: "Generated reader has no active version",
      }),
    ).toMatch(/no approved version yet/);
    expect(readErrorExplanation({ code: "weird_code" })).toBe("weird code");
    expect(
      readErrorExplanation({ code: "other", message: "GitHub said 502" }),
    ).toBe("GitHub said 502");
    expect(readErrorExplanation("")).toBe("");
  });
});

describe("freshness names the cause of a failed read", () => {
  it("blames the reader, not the source, for local causes", async () => {
    const { freshness } = await import("$lib/pm/presentation.js");
    expect(
      freshness({ error: { code: "policy_denied" }, sourceName: "GitHub" }),
    ).toMatchObject({ key: "error", label: "Reader not ready" });
    expect(
      freshness({ error: { code: "rate_limited" }, sourceName: "GitHub" }),
    ).toMatchObject({ key: "error", label: "Rate limited" });
    expect(
      freshness({ error: { code: "unavailable" }, sourceName: "GitHub" }),
    ).toMatchObject({ key: "error", label: "Can't reach GitHub" });
  });
});

describe("target revision follows core's decision_revision rule", () => {
  it("prefers the published field, then the external source revision, then version", async () => {
    const { workTargetRevision } = await import("$lib/pm/presentation.js");
    expect(workTargetRevision({ decision_revision: "abc", version: 9 })).toBe(
      "abc",
    );
    expect(
      workTargetRevision({
        source: { authority: "github", revision: "sha1" },
        version: 4,
      }),
    ).toBe("sha1");
    expect(
      workTargetRevision({
        source: { authority: "github" },
        freshness: { source_revision: "x" },
        version: 6,
      }),
    ).toBe("6");
  });
});
