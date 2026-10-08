import { describe, expect, it } from "vitest";
import { pinnedRefs, activityLabel } from "../../src/lib/pm/context.js";
import {
  collectPageRefs,
  indexResolvedRefs,
  refChipModel,
  keepReadableRefs,
} from "../../src/lib/refResolve.js";

describe("PM context and body refs", () => {
  it("persists multiple refs and prefers the conversation over navigation", () => {
    const search = new URLSearchParams("work_ref=card:wrong&ref=topic:wrong");
    expect(
      pinnedRefs(
        {
          work_ref: "card:release",
          context_refs: ["card:release", "topic:project"],
        },
        search,
      ),
    ).toEqual(["card:release", "topic:project"]);
    expect(pinnedRefs(null, search)).toEqual(["card:wrong", "topic:wrong"]);
  });
  it("resolves the inline card and topic using the same batch metadata", () => {
    const refs = collectPageRefs(["Read card:release and topic:project."]);
    const resolved = indexResolvedRefs(
      {
        items: refs.map((ref) => ({
          ref,
          title: ref.startsWith("card:") ? "Release" : "Project",
          resolvable: true,
          status: "review",
        })),
      },
      refs,
    );
    expect(refChipModel("card:release", resolved).title).toBe("Release");
    expect(refChipModel("topic:project", resolved).resolvable).toBe(true);
  });
  it("keeps valid chips through a transient resolve failure", () => {
    const known = indexResolvedRefs({
      items: [{ ref: "card:release", title: "Release", resolvable: true }],
    });
    const next = new Map([
      [
        "card:release",
        { ref: "card:release", resolvable: false, unreadable: true },
      ],
    ]);
    expect(
      refChipModel("card:release", keepReadableRefs(known, next)).title,
    ).toBe("Release");
    expect(refChipModel("card:release", next).resolutionKnown).toBe(false);
    expect(refChipModel("card:missing", new Map()).resolutionKnown).toBe(false);
  });
  it("shows only real runner progress", () => {
    expect(activityLabel({ activity: [{ label: "Reading the task" }] })).toBe(
      "Reading the task",
    );
    expect(activityLabel({})).toBe("Thinking");
  });
});
