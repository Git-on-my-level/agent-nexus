import { describe, expect, it } from "vitest";

import {
  filterTopLevelDocuments,
  isLegacyAgentRegistrationDocument,
} from "../../src/lib/documentVisibility.js";

describe("documentVisibility", () => {
  it("detects legacy registration documents by id prefix", () => {
    expect(
      isLegacyAgentRegistrationDocument({ id: "agentreg.hermes", labels: [] }),
    ).toBe(true);
  });

  it("detects legacy registration documents by label", () => {
    expect(
      isLegacyAgentRegistrationDocument({
        id: "doc-1",
        labels: ["agent-registration", "handle:m4-hermes"],
      }),
    ).toBe(true);
  });

  it("preserves normal documents", () => {
    expect(
      isLegacyAgentRegistrationDocument({
        id: "onboarding-runbook",
        labels: ["onboarding"],
      }),
    ).toBe(false);
  });

  it("filters system registration records from top-level docs views", () => {
    expect(
      filterTopLevelDocuments([
        { id: "agentreg.hermes", title: "Agent registration @hermes" },
        { id: "welcome", title: "Welcome", labels: ["overview"] },
        {
          id: "doc-2",
          title: "Agent metadata",
          labels: ["agent-registration"],
        },
      ]),
    ).toEqual([{ id: "welcome", title: "Welcome", labels: ["overview"] }]);
  });
});
