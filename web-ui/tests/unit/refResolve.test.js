import { describe, expect, it } from "vitest";

import {
  MAX_BATCH_REFS,
  hasUnreadableRefs,
  keepReadableRefs,
  resolveRefsInBatches,
  safeRefDestination,
  CHIP_REF_PREFIXES,
  classifyWorkUrl,
  collectPageRefs,
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
      classifyWorkUrl("https://workspace.example.test/issue/sca-604"),
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
      // `owner` is an actor ref on the wire; `owner_display` is the name.
      ownerDisplay: "Codex Sol",
      board: "Release B",
      priority: "p1",
      nextStep: "Computed progress and health",
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
    const index = indexResolvedRefs({ items: [] }, ["card:never-came-back"]);
    expect(index.get("card:never-came-back")).toEqual({
      ref: "card:never-came-back",
      resolvable: false,
    });
  });

  it("reads the contract envelope and a bare array alike", () => {
    expect(
      indexResolvedRefs({ items: [{ ref: "card:a", title: "A" }] }).get(
        "card:a",
      ).title,
    ).toBe("A");
    expect(
      indexResolvedRefs([{ ref: "card:a", title: "A" }]).get("card:a").title,
    ).toBe("A");
  });

  it("reads phase when the row spells status that way", () => {
    const index = indexResolvedRefs({
      items: [{ ref: "card:a", phase: "review" }],
    });
    expect(index.get("card:a").status).toBe("review");
  });

  it("drops a progress object that cannot be a ratio", () => {
    const index = indexResolvedRefs({
      items: [
        { ref: "card:a", progress: { done: 1, total: 0 } },
        { ref: "card:b", progress: { done: "x", total: 4 } },
      ],
    });
    expect(index.get("card:a").progress).toBeNull();
    expect(index.get("card:b").progress).toBeNull();
  });

  it("clamps progress that overshoots its total", () => {
    const index = indexResolvedRefs({
      items: [{ ref: "card:a", progress: { done: 9, total: 4 } }],
    });
    expect(index.get("card:a").progress).toEqual({ done: 4, total: 4 });
  });

  it("ignores a row with no ref", () => {
    expect(indexResolvedRefs({ items: [{ title: "orphan" }] }).size).toBe(0);
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
    // Core sends `/tasks/<handle>`; the UI rebases it onto the workspace route.
    expect(model.href).toBe("/o/scaling/w/anx/tasks/initiative-plans");
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

  it("labels an external pull request by its URL, not core's card kind", () => {
    // Core resolves external links through source-backed cards, so it sends
    // `kind: "card"`; the URL says more, and says it reliably.
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
    const index = indexResolvedRefs({
      items: [{ ref: "card:a", title: "A", url: "https://elsewhere.test/a" }],
    });
    expect(refChipModel("card:a", index, context).href).toBe(
      "https://elsewhere.test/a",
    );
  });

  it("yields no link without a workspace in context", () => {
    expect(refChipModel("card:initiative-plans", resolved, {}).href).toBe("");
  });

  it("shows the owner by name", () => {
    const model = refChipModel("card:pushed-series", resolved, context);
    expect(model).toMatchObject({
      owner: "Codex Sol",
      progress: { done: 1, total: 6 },
      statusLabel: "blocked",
    });
  });

  it("carries the preview fields the contract now returns", () => {
    const model = refChipModel("card:pushed-series", resolved, context);
    expect(model).toMatchObject({
      board: "Release B",
      priority: "p1",
      nextStep: "Waiting on the panel binding decision",
      lastMovedAt: "2026-09-27T11:02:00Z",
    });
  });

  it("reports a field the response omitted as absent, not invented", () => {
    // Board metadata needs independent board visibility, so a readable ref can
    // still arrive without one.
    const model = refChipModel("doc:release-b-plan", resolved, context);
    expect(model.board).toBe("");
    expect(model.nextStep).toBe("");
  });

  it("still reads those fields when a response does carry them", () => {
    const index = indexResolvedRefs({
      items: [
        {
          ref: "card:a",
          title: "A",
          board: "Release B",
          priority: "high",
          next_step: "Ship it",
          last_moved_at: "2026-10-04T09:12:00Z",
          resolvable: true,
        },
      ],
    });
    expect(refChipModel("card:a", index, context)).toMatchObject({
      board: "Release B",
      priority: "high",
      nextStep: "Ship it",
      lastMovedAt: "2026-10-04T09:12:00Z",
    });
  });
});

describe("workspace-relative URLs from core", () => {
  const context = { organizationSlug: "scaling", workspaceSlug: "anx" };
  const withUrl = (url) =>
    indexResolvedRefs({
      items: [{ ref: "card:a", title: "A", url, resolvable: true }],
    });

  it("rebases a core path onto the workspace route", () => {
    // Core returns `/tasks/<handle>`, which is relative to the workspace. The
    // UI's routes live under /o/<org>/w/<workspace>/, so a verbatim href would
    // navigate out of the workspace the reader is in.
    expect(
      refChipModel("card:a", withUrl("/tasks/release-b"), context).href,
    ).toBe("/o/scaling/w/anx/tasks/release-b");
    expect(refChipModel("card:a", withUrl("/docs/plan"), context).href).toBe(
      "/o/scaling/w/anx/docs/plan",
    );
  });

  it("leaves an absolute http(s) URL alone", () => {
    expect(
      refChipModel("card:a", withUrl("https://github.com/o/r/pull/1"), context)
        .href,
    ).toBe("https://github.com/o/r/pull/1");
  });

  it("yields no link for a core path without a workspace in context", () => {
    expect(refChipModel("card:a", withUrl("/tasks/release-b"), {}).href).toBe(
      "",
    );
  });
});

describe("safeRefDestination", () => {
  it("allows http, https and workspace-relative paths", () => {
    expect(safeRefDestination("https://example.test/x")).toBe(
      "https://example.test/x",
    );
    expect(safeRefDestination("http://example.test/x")).toBe(
      "http://example.test/x",
    );
    expect(safeRefDestination("/tasks/release-b")).toBe("/tasks/release-b");
  });

  it("refuses a scheme that would execute", () => {
    for (const url of [
      "javascript:alert(1)",
      "JavaScript:alert(1)",
      "data:text/html,<script>alert(1)</script>",
      "vbscript:msgbox(1)",
    ]) {
      expect(safeRefDestination(url)).toBe("");
    }
  });

  it("refuses a protocol-relative URL that would leave the origin", () => {
    expect(safeRefDestination("//evil.test/x")).toBe("");
  });

  it("refuses anything it does not recognise", () => {
    expect(safeRefDestination("mailto:a@b.test")).toBe("");
    expect(safeRefDestination("tasks/release-b")).toBe("");
    expect(safeRefDestination("")).toBe("");
  });

  it("keeps an unsafe server URL out of a chip", () => {
    const context = { organizationSlug: "scaling", workspaceSlug: "anx" };
    const unsafe = (ref) =>
      indexResolvedRefs({
        items: [
          { ref, title: "A", url: "javascript:alert(1)", resolvable: true },
        ],
      });

    // A card has a destination we can derive ourselves, so the hostile URL is
    // dropped and the chip still goes somewhere real.
    const card = refChipModel("card:a", unsafe("card:a"), context);
    expect(card.href).toBe("/o/scaling/w/anx/tasks/card%3Aa");
    expect(card.href).not.toContain("javascript");

    // A board has no derivable destination, so the chip simply does not link
    // rather than becoming an executable anchor.
    expect(refChipModel("board:a", unsafe("board:a"), context).href).toBe("");
  });
});

describe("resolveRefsInBatches", () => {
  const row = (ref) => ({ ref, title: ref, resolvable: true });

  it("keeps a single request within the contract's maximum", async () => {
    const calls = [];
    const refs = Array.from({ length: 10 }, (_, index) => `card:${index}`);
    await resolveRefsInBatches(refs, async (batch) => {
      calls.push(batch.length);
      return { items: batch.map(row) };
    });
    expect(calls).toEqual([10]);
  });

  it("splits a report that names more refs than one request accepts", async () => {
    // /refs/resolve caps a request at 200 refs, and one oversized request is
    // rejected whole — which used to mark every chip on the page missing.
    const refs = Array.from({ length: 450 }, (_, index) => `card:${index}`);
    const calls = [];
    const resolved = await resolveRefsInBatches(refs, async (batch) => {
      calls.push(batch.length);
      return { items: batch.map(row) };
    });
    expect(calls).toEqual([MAX_BATCH_REFS, MAX_BATCH_REFS, 50]);
    expect(resolved.size).toBe(450);
    expect(resolved.get("card:449")).toMatchObject({ resolvable: true });
  });

  it("keeps the other batches when one fails", async () => {
    const refs = Array.from({ length: 300 }, (_, index) => `card:${index}`);
    const resolved = await resolveRefsInBatches(refs, async (batch) => {
      if (batch.includes("card:0")) throw new Error("rejected");
      return { items: batch.map(row) };
    });
    // The failed batch's refs survive as "not found" rather than vanishing,
    // and say they were never read so a caller does not cache the failure.
    expect(resolved.get("card:0")).toEqual({
      ref: "card:0",
      resolvable: false,
      unreadable: true,
    });
    expect(resolved.get("card:250")).toMatchObject({ resolvable: true });
    expect(resolved.get("card:250").unreadable).toBeUndefined();
    expect(hasUnreadableRefs(resolved)).toBe(true);
  });

  it("does not call a ref the resolver answered for unreadable", async () => {
    // A ref the resolver answered about is a fact, however unwelcome: it is
    // "not found", and asking again gets the same answer. Only a request that
    // never completed is worth retrying, and a caller cannot tell the two
    // apart from `resolvable` alone.
    const resolved = await resolveRefsInBatches(
      ["card:gone", "card:here"],
      async () => ({ items: [row("card:here")] }),
    );
    expect(resolved.get("card:gone")).toEqual({
      ref: "card:gone",
      resolvable: false,
    });
    expect(hasUnreadableRefs(resolved)).toBe(false);
  });

  it("reports nothing unreadable for an empty or missing answer", () => {
    expect(hasUnreadableRefs(new Map())).toBe(false);
    expect(hasUnreadableRefs(null)).toBe(false);
    expect(hasUnreadableRefs(undefined)).toBe(false);
  });

  it("keeps a readable row when the next answer could not read it", () => {
    const previous = new Map([
      ["card:a", { ref: "card:a", title: "Adapter", resolvable: true }],
    ]);
    const next = new Map([
      ["card:a", { ref: "card:a", resolvable: false, unreadable: true }],
      ["card:b", { ref: "card:b", resolvable: false, unreadable: true }],
    ]);
    const merged = keepReadableRefs(previous, next);
    // A title the reader can see beats "not found" from a request that failed.
    expect(merged.get("card:a")).toMatchObject({ title: "Adapter" });
    // Nothing was ever read for this one, so there is nothing to keep.
    expect(merged.get("card:b")).toMatchObject({ resolvable: false });
  });

  it("lets an answered row replace a readable one, missing included", () => {
    const previous = new Map([
      ["card:a", { ref: "card:a", title: "Adapter", resolvable: true }],
      ["card:b", { ref: "card:b", title: "Release", resolvable: true }],
    ]);
    const next = new Map([
      [
        "card:a",
        { ref: "card:a", title: "Adapter contract", resolvable: true },
      ],
      ["card:b", { ref: "card:b", resolvable: false }],
      ["card:c", { ref: "card:c", resolvable: false, unreadable: true }],
    ]);
    const merged = keepReadableRefs(previous, next);
    expect(merged.get("card:a")).toMatchObject({ title: "Adapter contract" });
    // Deleted, renamed out of reach, or never there: that is news about the
    // ref, not a failed request, so it stands.
    expect(merged.get("card:b")).toEqual({ ref: "card:b", resolvable: false });
  });

  it("returns a fully readable answer untouched", () => {
    const next = new Map([["card:a", { ref: "card:a", resolvable: true }]]);
    expect(keepReadableRefs(new Map(), next)).toBe(next);
    expect(keepReadableRefs(null, next)).toBe(next);
    expect(
      keepReadableRefs(
        new Map([["card:a", { ref: "card:a", resolvable: true }]]),
        next,
      ),
    ).toBe(next);
  });

  it("dedupes before counting against the cap", async () => {
    const calls = [];
    await resolveRefsInBatches(
      ["card:a", "card:a", "card:b"],
      async (batch) => {
        calls.push(batch);
        return { items: batch.map(row) };
      },
    );
    expect(calls).toEqual([["card:a", "card:b"]]);
  });

  it("makes no request for an empty page", async () => {
    const resolve = async () => ({ items: [] });
    expect((await resolveRefsInBatches([], resolve)).size).toBe(0);
  });
});

describe("external refs", () => {
  const context = { organizationSlug: "scaling", workspaceSlug: "anx" };

  /** The shape the parallel core change answers an external ref with. */
  const externalRow = (overrides = {}) => ({
    ref: "https://github.com/o/r/pull/12",
    kind: "external",
    authority: "github",
    native_id: "o/r#12",
    title: "Ship the renderer",
    url: "https://github.com/o/r/pull/12",
    status: "open",
    resolvable: true,
    ...overrides,
  });

  it("reads the external row core sends", () => {
    const resolved = indexResolvedRefs({ items: [externalRow()] });
    const model = refChipModel(
      "https://github.com/o/r/pull/12",
      resolved,
      context,
    );
    expect(model).toMatchObject({
      title: "Ship the renderer",
      status: "open",
      statusLabel: "open",
      isExternal: true,
      authority: "github",
      authorityLabel: "GitHub",
      nativeId: "o/r#12",
      resolvable: true,
      href: "https://github.com/o/r/pull/12",
    });
  });

  it("labels an external row with no recognisable kind by its authority", () => {
    const resolved = indexResolvedRefs({
      items: [
        externalRow({ ref: "ext:o/r/42", url: "https://github.com/o/r/42" }),
      ],
    });
    expect(refChipModel("ext:o/r/42", resolved, context)).toMatchObject({
      kindLabel: "GitHub",
      resolvable: true,
      href: "https://github.com/o/r/42",
    });
  });

  it("shows a merged pull request as a done-toned status", () => {
    const resolved = indexResolvedRefs({
      items: [externalRow({ status: "merged" })],
    });
    expect(
      refChipModel("https://github.com/o/r/pull/12", resolved, context)
        .statusTone,
    ).toBe("ok");
  });

  it("never reads not-found for a valid GitHub ref the resolve missed", () => {
    // This is the plan-graph bug: a batch that failed, or a core that does not
    // resolve externals yet, recorded the ref as unresolvable. A URL the
    // browser can classify is still a real destination.
    const resolved = indexResolvedRefs({}, ["https://github.com/o/r/pull/12"]);
    expect(resolved.get("https://github.com/o/r/pull/12").resolvable).toBe(
      false,
    );
    const model = refChipModel(
      "https://github.com/o/r/pull/12",
      resolved,
      context,
    );
    expect(model.resolvable).toBe(true);
    expect(model.title).toBe("o/r#12");
    expect(model.href).toBe("https://github.com/o/r/pull/12");
  });

  it("still reads not-found for an ANX ref that resolves to nothing", () => {
    const resolved = indexResolvedRefs({}, ["card:gone"]);
    expect(refChipModel("card:gone", resolved, context)).toMatchObject({
      resolvable: false,
      href: "",
    });
  });

  it("refuses an external url that would execute", () => {
    const resolved = indexResolvedRefs({
      items: [externalRow({ ref: "ext:bad", url: "javascript:alert(1)" })],
    });
    expect(refChipModel("ext:bad", resolved, context).href).toBe("");
  });
});
