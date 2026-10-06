// @vitest-environment jsdom
/**
 * What a report's ref chips show while requests are in flight.
 *
 * Resolution is one batched request per ref set, remembered by that set so a
 * live panel refreshing with the same refs does not re-ask. Both regressions
 * here are about what that remembering must not do: accept an answer a later
 * request has overtaken, or keep a request that never completed as if it were
 * an answer.
 */
import { cleanup, render } from "@testing-library/svelte";
import { tick } from "svelte";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import VisualReport from "../../src/lib/components/reports/VisualReport.svelte";
import { MAX_BATCH_REFS } from "../../src/lib/refResolve.js";

const coreClientMock = vi.hoisted(() => ({
  resolveRefs: vi.fn(),
  renderReport: vi.fn(),
}));
vi.mock("$lib/coreClient", () => ({ coreClient: coreClientMock }));

const CARD = "card:adapter-contract";
const OTHER = "card:release-b";

/** A report whose only ref is `ref`, written in a panel of prose. */
const report = (title, ref = CARD) => ({
  schema_version: 1,
  title,
  summary: "",
  generated_at: "2026-10-03T07:00:00Z",
  projects: [{ id: "release", title: "Release", outcome: "", summary: "" }],
  panels: [
    {
      id: "overview",
      project_id: "release",
      type: "explanation",
      title: `${title} panel`,
      author: "Report author",
      provenance: "reported",
      observed_at: "2026-10-03T07:00:00Z",
      freshness: "current",
      source_ids: [],
      data: { text: `Waiting on ${ref} to land.` },
    },
  ],
  sources: [],
});

/** A report naming both refs, in the order given. */
const pair = (title, refs) => ({
  ...report(title),
  panels: [
    {
      ...report(title).panels[0],
      data: { text: `Waiting on ${refs[0]} and ${refs[1]} to land.` },
    },
  ],
});

/** A resolve response carrying the title and status of the moment. */
const answer = (ref, title, status) => ({
  items: [{ ref, kind: "card", title, status, resolvable: true }],
});

/** A promise this test settles by hand, to control what lands when. */
const deferred = () => {
  let settle;
  const promise = new Promise((resolve, reject) => {
    settle = { resolve, reject };
  });
  return { promise, ...settle };
};

const chip = (container, ref = CARD) =>
  container.querySelector(`[data-anx-ref="${ref}"]`);

/** Let the component's effects run and any settled promise be handled. */
const settled = async () => {
  for (let pass = 0; pass < 4; pass += 1) {
    await Promise.resolve();
    await tick();
  }
};

beforeEach(() => {
  coreClientMock.resolveRefs.mockReset();
  coreClientMock.renderReport.mockReset();
});
afterEach(() => cleanup());

describe("VisualReport ref resolution", () => {
  it("ignores an answer a later request has overtaken", async () => {
    // Dashboard A → B → A with the first A request slow. Its answer was
    // resolved before anything moved and arrives last, and it asks for exactly
    // the refs on screen — so matching on the ref set alone accepted it and
    // put the stale title and status back.
    const firstA = deferred();
    const secondA = deferred();
    coreClientMock.resolveRefs
      .mockReturnValueOnce(firstA.promise)
      .mockResolvedValueOnce(answer(OTHER, "Release B", "blocked"))
      .mockReturnValueOnce(secondA.promise);

    const { container, rerender } = render(VisualReport, {
      report: report("A"),
    });
    await settled();
    await rerender({ report: report("B", OTHER) });
    await settled();
    await rerender({ report: report("A") });
    await settled();
    expect(coreClientMock.resolveRefs).toHaveBeenCalledTimes(3);

    secondA.resolve(answer(CARD, "Adapter contract", "in_progress"));
    await settled();
    expect(chip(container).textContent).toContain("Adapter contract");

    firstA.resolve(answer(CARD, "Adapter contract (old)", "blocked"));
    await settled();
    expect(chip(container).textContent).not.toContain("old");
    expect(chip(container).dataset.status).toBe("in_progress");
  });

  it("asks again after a request that could not be read", async () => {
    // A 503 is not an answer about this ref. Its chip reads "not found"
    // because that beats blank, but the ref set must not be remembered as
    // asked: the next report naming the same ref used to inherit the failure
    // and stay "not found" without ever re-asking.
    coreClientMock.resolveRefs
      .mockRejectedValueOnce(new Error("503"))
      .mockResolvedValueOnce(answer(CARD, "Adapter contract", "in_progress"));

    const { container, rerender } = render(VisualReport, {
      report: report("A"),
    });
    await settled();
    expect(coreClientMock.resolveRefs).toHaveBeenCalledTimes(1);
    expect(chip(container).className).toContain("anx-ref-chip--missing");

    await rerender({ report: report("B") });
    await settled();
    expect(coreClientMock.resolveRefs).toHaveBeenCalledTimes(2);
    expect(chip(container).className).not.toContain("anx-ref-chip--missing");
    expect(chip(container).textContent).toContain("Adapter contract");
  });

  it("keeps a ref the resolver answered for, rather than asking again", async () => {
    // "No such ref" is news about the ref, and asking again gets the same
    // answer. Only a request that never completed is worth retrying: treating
    // every "not found" as a retry would re-ask on every live refresh, for as
    // long as a report names one ref that genuinely does not exist.
    coreClientMock.resolveRefs.mockResolvedValue({ items: [] });

    const { container, rerender } = render(VisualReport, {
      report: report("A"),
    });
    await settled();
    expect(chip(container).className).toContain("anx-ref-chip--missing");

    await rerender({ report: report("B") });
    await settled();
    expect(coreClientMock.resolveRefs).toHaveBeenCalledTimes(1);
    expect(chip(container).className).toContain("anx-ref-chip--missing");
  });

  it("treats the same refs in a different order as the same question", async () => {
    // A live initiatives panel lists by attention, so the same set of refs
    // arrives reordered as soon as anything moves. Keying on the written order
    // made that a new question: it re-asked, and superseded the answer already
    // on its way.
    const first = deferred();
    coreClientMock.resolveRefs.mockReturnValueOnce(first.promise);

    const { container, rerender } = render(VisualReport, {
      report: pair("A", [CARD, OTHER]),
    });
    await settled();
    await rerender({ report: pair("A", [OTHER, CARD]) });
    await settled();
    expect(coreClientMock.resolveRefs).toHaveBeenCalledTimes(1);

    first.resolve(answer(CARD, "Adapter contract", "in_progress"));
    await settled();
    expect(chip(container).textContent).toContain("Adapter contract");
  });

  it("does not lose a readable title to a retry that failed instead", async () => {
    // Over the per-request cap, so the answer arrives in two batches and can
    // be half an answer. The first attempt reads the head and loses the tail,
    // which is what asks for the retry — and the retry fails the other way
    // round. Replacing the whole answer would take titles the reader is
    // looking at and turn them into "not found".
    const refs = Array.from(
      { length: MAX_BATCH_REFS + 1 },
      (_, index) => `card:bulk-${index}`,
    );
    const tail = refs.at(-1);
    let calls = 0;
    coreClientMock.resolveRefs.mockImplementation(async (batch) => {
      // Batches go out together and in order, so calls 1-2 are the first
      // attempt and 3-4 the retry.
      const retry = ++calls > 2;
      const head = batch.length > 1;
      if (head === retry) throw new Error("503");
      return {
        items: batch.map((ref) => ({
          ref,
          kind: "card",
          title: `Bulk ${ref}`,
          status: "in_progress",
          resolvable: true,
        })),
      };
    });

    const bulk = { ...report("A"), panels: [report("A").panels[0]] };
    bulk.panels[0] = {
      ...bulk.panels[0],
      data: { text: `Waiting on ${refs.join(", ")} to land.` },
    };
    const { container, rerender } = render(VisualReport, { report: bulk });
    await settled();
    expect(chip(container, "card:bulk-0").textContent).toContain(
      "Bulk card:bulk-0",
    );
    expect(chip(container, tail).className).toContain("anx-ref-chip--missing");

    await rerender({ report: { ...bulk, title: "A refreshed" } });
    await settled();
    expect(calls).toBe(4);
    expect(chip(container, "card:bulk-0").textContent).toContain(
      "Bulk card:bulk-0",
    );
    expect(chip(container, "card:bulk-0").className).not.toContain(
      "anx-ref-chip--missing",
    );
    expect(chip(container, tail).textContent).toContain(`Bulk ${tail}`);
  });

  it("keeps the in-flight answer when a refresh names the same refs", async () => {
    // Why staleness is settled on arrival rather than by cancelling on re-run:
    // a refresh that names the same refs must neither re-ask nor throw away
    // the request already out for them.
    const first = deferred();
    coreClientMock.resolveRefs.mockReturnValueOnce(first.promise);

    const { container, rerender } = render(VisualReport, {
      report: report("A"),
    });
    await settled();
    await rerender({ report: { ...report("A"), title: "A refreshed" } });
    await settled();
    expect(coreClientMock.resolveRefs).toHaveBeenCalledTimes(1);

    first.resolve(answer(CARD, "Adapter contract", "in_progress"));
    await settled();
    expect(chip(container).textContent).toContain("Adapter contract");
  });
});
