import { describe, expect, it } from "vitest";

import {
  CHIP_REF_PREFIXES,
  classifyWorkUrl,
  collectPageRefs,
  formatMovedAgo,
  indexResolvedRefs,
  isChipRef,
  refChipModel,
  tokenizeRefText,
} from "../../src/lib/refResolve.js";
import {
  refResolveExample,
  refResolveExampleRequest,
} from "../../src/lib/fixtures/refResolveExample.js";

const context = { organizationSlug: "scaling", workspaceSlug: "anx" };
const resolved = indexResolvedRefs(refResolveExample, refResolveExampleRequest);

describe("isChipRef", () => {
  it("accepts every chip prefix", () => {
    for (const prefix of CHIP_REF_PREFIXES) {
      expect(isChipRef(`${prefix}:thing`)).toBe(true);
    }
  });

  it("accepts both doc and document spellings", () => {
    expect(isChipRef("doc:plan")).toBe(true);
    expect(isChipRef("document:plan")).toBe(true);
  });

  it("rejects a prefix with no value and an unknown prefix", () => {
    expect(isChipRef("card:")).toBe(false);
    expect(isChipRef("thread:abc")).toBe(false);
    expect(isChipRef("just text")).toBe(false);
  });
});

describe("classifyWorkUrl", () => {
  it("recognises a GitHub pull request", () => {
    expect(
      classifyWorkUrl(
        "https://github.com/Git-on-my-level/agent-nexus/pull/246",
      ),
    ).toMatchObject({
      kind: "pull_request",
      source: "github",
      label: "Git-on-my-level/agent-nexus#246",
    });
  });

  it("recognises a GitHub issue", () => {
    expect(
      classifyWorkUrl("https://github.com/anthropics/claude-code/issues/12"),
    ).toMatchObject({ kind: "issue", label: "anthropics/claude-code#12" });
  });

  it("recognises a Multica issue on any host", () => {
    expect(
      classifyWorkUrl("https://multica-01.tail76ea03.ts.net/issue/sca-604"),
    ).toMatchObject({ kind: "issue", source: "multica", label: "SCA-604" });
  });

  it("tolerates a query or fragment on the URL", () => {
    expect(
      classifyWorkUrl(
        "https://github.com/Git-on-my-level/agent-nexus/pull/246#issuecomment-1",
      ),
    ).toMatchObject({ kind: "pull_request" });
  });

  it("does not claim an unrelated URL is work", () => {
    expect(classifyWorkUrl("https://example.com/docs/readme")).toBeNull();
    expect(
      classifyWorkUrl("https://github.com/Git-on-my-level/agent-nexus"),
    ).toBeNull();
    expect(classifyWorkUrl("")).toBeNull();
  });
});

describe("tokenizeRefText", () => {
  it("splits a ref out of a sentence", () => {
    expect(tokenizeRefText("Blocked by card:release-b today")).toEqual([
      { type: "text", value: "Blocked by " },
      { type: "ref", value: "card:release-b" },
      { type: "text", value: " today" },
    ]);
  });

  it("leaves sentence punctuation out of the ref", () => {
    const tokens = tokenizeRefText("See card:release-b.");
    expect(tokens[1]).toEqual({ type: "ref", value: "card:release-b" });
    expect(tokens[2]).toEqual({ type: "text", value: "." });
  });

  it("finds several refs in one cell", () => {
    const tokens = tokenizeRefText("card:a and doc:b and topic:c");
    expect(tokens.filter((t) => t.type === "ref").map((t) => t.value)).toEqual([
      "card:a",
      "doc:b",
      "topic:c",
    ]);
  });

  it("labels a work URL and keeps a plain URL as a link", () => {
    const work = tokenizeRefText(
      "fixed in https://github.com/Git-on-my-level/agent-nexus/pull/246",
    );
    expect(work.at(-1)).toMatchObject({
      type: "url",
      kind: "pull_request",
      label: "Git-on-my-level/agent-nexus#246",
    });

    const plain = tokenizeRefText("see https://example.com/x");
    expect(plain.at(-1)).toEqual({
      type: "url",
      value: "https://example.com/x",
    });
  });

  it("strips a trailing bracket from a URL", () => {
    const tokens = tokenizeRefText("(https://example.com/x)");
    expect(tokens.find((t) => t.type === "url").value).toBe(
      "https://example.com/x",
    );
  });

  it("returns nothing for empty text", () => {
    expect(tokenizeRefText("")).toEqual([]);
    expect(tokenizeRefText(null)).toEqual([]);
  });

  it("returns a single run when there is nothing to chip", () => {
    expect(tokenizeRefText("plain prose")).toEqual([
      { type: "text", value: "plain prose" },
    ]);
  });
});

describe("collectPageRefs", () => {
  it("dedupes every ref on a page into one batch", () => {
    const refs = collectPageRefs([
      "Blocked by card:a",
      "card:a is also here, with doc:b",
      "and https://github.com/o/r/pull/9",
    ]);
    expect(refs).toEqual(["card:a", "doc:b", "https://github.com/o/r/pull/9"]);
  });

  it("includes refs the page already knows are refs", () => {
    const refs = collectPageRefs(["prose with no refs"], {
      extraRefs: ["card:plan-step", "not a ref"],
    });
    expect(refs).toEqual(["card:plan-step"]);
  });

  it("leaves out a URL that is not work", () => {
    expect(collectPageRefs(["see https://example.com/x"])).toEqual([]);
  });

  it("returns nothing for an empty page", () => {
    expect(collectPageRefs([])).toEqual([]);
  });
});

describe("indexResolvedRefs", () => {
  it("indexes the fixture by ref", () => {
    expect(resolved.get("card:initiative-plans")).toMatchObject({
      kind: "card",
      title: "Initiative plans on cards",
      status: "in_progress",
      owner: "Codex Sol",
      progress: { done: 3, total: 7 },
      resolvable: true,
    });
  });

  it("keeps a row the server marked unresolvable", () => {
    expect(resolved.get("card:deleted-thing")).toMatchObject({
      resolvable: false,
    });
  });

  it("records a requested ref the response left out", () => {
    const index = indexResolvedRefs({ refs: [] }, ["card:never-came-back"]);
    expect(index.get("card:never-came-back")).toEqual({
      ref: "card:never-came-back",
      resolvable: false,
    });
  });

  it("accepts a bare array as well as a wrapped response", () => {
    const index = indexResolvedRefs([{ ref: "card:a", title: "A" }]);
    expect(index.get("card:a").title).toBe("A");
  });

  it("reads phase when the row spells status that way", () => {
    const index = indexResolvedRefs([{ ref: "card:a", phase: "review" }]);
    expect(index.get("card:a").status).toBe("review");
  });

  it("drops a progress object that cannot be a ratio", () => {
    const index = indexResolvedRefs([
      { ref: "card:a", progress: { done: 1, total: 0 } },
      { ref: "card:b", progress: { done: "x", total: 4 } },
    ]);
    expect(index.get("card:a").progress).toBeNull();
    expect(index.get("card:b").progress).toBeNull();
  });

  it("clamps progress that overshoots its total", () => {
    const index = indexResolvedRefs([
      { ref: "card:a", progress: { done: 9, total: 4 } },
    ]);
    expect(index.get("card:a").progress).toEqual({ done: 4, total: 4 });
  });

  it("ignores a row with no ref", () => {
    expect(indexResolvedRefs([{ title: "orphan" }]).size).toBe(0);
  });
});

describe("refChipModel", () => {
  it("builds a task chip with its status tone and a link to the task", () => {
    const model = refChipModel("card:initiative-plans", resolved, context);
    expect(model).toMatchObject({
      kind: "card",
      kindLabel: "Task",
      title: "Initiative plans on cards",
      statusLabel: "in progress",
      statusTone: "neutral",
      resolvable: true,
      isExternal: false,
    });
    expect(model.href).toBe("/o/scaling/w/anx/tasks/card%3Ainitiative-plans");
  });

  it("tones a done ref ok and a blocked ref danger", () => {
    expect(
      refChipModel("card:shared-report-contracts", resolved, context)
        .statusTone,
    ).toBe("ok");
    expect(
      refChipModel("card:pushed-series", resolved, context).statusTone,
    ).toBe("danger");
  });

  it("opens a project ref as the Tasks list filtered to it", () => {
    expect(refChipModel("topic:release-b", resolved, context).href).toBe(
      "/o/scaling/w/anx/tasks?project_ref=topic%3Arelease-b",
    );
  });

  it("gives a board ref no link, because there is no board surface", () => {
    const model = refChipModel("board:release-b", resolved, context);
    expect(model.resolvable).toBe(true);
    expect(model.href).toBe("");
    expect(model.kindLabel).toBe("Board");
  });

  it("links a doc ref to the doc", () => {
    expect(refChipModel("doc:release-b-plan", resolved, context).href).toBe(
      "/o/scaling/w/anx/docs/release-b-plan",
    );
  });

  it("marks an external pull request external and links it out", () => {
    const model = refChipModel(
      "https://github.com/Git-on-my-level/agent-nexus/pull/246",
      resolved,
      context,
    );
    expect(model).toMatchObject({
      kind: "pull_request",
      kindLabel: "PR",
      isExternal: true,
      statusTone: "ok",
    });
    expect(model.href).toBe(
      "https://github.com/Git-on-my-level/agent-nexus/pull/246",
    );
  });

  it("renders an unresolvable ref as not found, never as nothing", () => {
    const model = refChipModel("card:deleted-thing", resolved, context);
    expect(model.resolvable).toBe(false);
    expect(model.title).toBe("card:deleted-thing");
    expect(model.href).toBe("");
  });

  it("treats a ref that was never resolved as not found", () => {
    const model = refChipModel("card:unknown", new Map(), context);
    expect(model.resolvable).toBe(false);
    expect(model.title).toBe("card:unknown");
  });

  it("prefers a url the server supplied over a derived path", () => {
    const index = indexResolvedRefs([
      { ref: "card:a", title: "A", url: "https://elsewhere.test/a" },
    ]);
    expect(refChipModel("card:a", index, context).href).toBe(
      "https://elsewhere.test/a",
    );
  });

  it("yields no link without a workspace in context", () => {
    expect(refChipModel("card:initiative-plans", resolved, {}).href).toBe("");
  });

  it("carries the preview fields through", () => {
    const model = refChipModel("card:pushed-series", resolved, context);
    expect(model).toMatchObject({
      owner: "Codex Sol",
      board: "Release B",
      nextStep: "Waiting on the panel binding decision",
      progress: { done: 1, total: 6 },
    });
  });
});

describe("formatMovedAgo", () => {
  const now = Date.parse("2026-10-04T12:00:00Z");
  const ago = (ms) => new Date(now - ms).toISOString();

  it("reads in minutes, hours and days", () => {
    expect(formatMovedAgo(ago(30_000), now)).toBe("just now");
    expect(formatMovedAgo(ago(5 * 60_000), now)).toBe("5m ago");
    expect(formatMovedAgo(ago(3 * 3_600_000), now)).toBe("3h ago");
    expect(formatMovedAgo(ago(2 * 86_400_000), now)).toBe("2d ago");
  });

  it("says nothing for a missing or future timestamp", () => {
    expect(formatMovedAgo(null, now)).toBe("");
    expect(formatMovedAgo("not a date", now)).toBe("");
    expect(formatMovedAgo(new Date(now + 60_000).toISOString(), now)).toBe("");
  });
});
