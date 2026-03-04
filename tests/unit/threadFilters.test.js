import { describe, expect, it } from "vitest";

import {
  buildThreadFilterQuery,
  buildThreadFilterRequestQuery,
  parseTagFilterInput,
} from "../../src/lib/threadFilters.js";

describe("thread filter query builders", () => {
  it("builds stable query string for selected filters", () => {
    const query = buildThreadFilterQuery({
      status: "active",
      priority: "p1",
      cadence: "weekly",
      tags: ["ops", "customer"],
      staleness: "stale",
    });

    expect(query).toBe(
      "status=active&priority=p1&cadence=weekly&tag=ops&tag=customer&stale=true",
    );
  });

  it("builds request query object and parses tag input", () => {
    expect(parseTagFilterInput("ops, customer,,infra")).toEqual([
      "ops",
      "customer",
      "infra",
    ]);

    expect(
      buildThreadFilterRequestQuery({
        status: "",
        priority: "p0",
        cadence: "",
        tags: ["ops"],
        staleness: "fresh",
      }),
    ).toEqual({
      priority: "p0",
      tag: ["ops"],
      stale: false,
    });
  });
});
