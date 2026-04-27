import { describe, expect, it } from "vitest";

import {
  documentLifecycleLabel,
  documentLifecyclePillClass,
  documentResourceState,
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

  it("prefers state over legacy status for document lifecycle", () => {
    expect(documentResourceState({ state: "trashed" })).toBe("trashed");
    expect(documentResourceState({ state: "active", status: "draft" })).toBe(
      "active",
    );
    expect(documentResourceState({ status: "active" })).toBe("active");
  });

  it("labels canonical document lifecycle states", () => {
    expect(documentLifecycleLabel("active")).toBe("Active");
    expect(documentLifecycleLabel("trashed")).toBe("Trashed");
  });

  it("maps lifecycle states to pill classes", () => {
    expect(documentLifecyclePillClass("trashed")).toContain("danger");
    expect(documentLifecyclePillClass("active")).toContain("ok");
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
