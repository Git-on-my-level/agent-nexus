import { describe, expect, it } from "vitest";

describe("mockCoreData parity behaviors", () => {
  describe("inbox ack is non-destructive", () => {
    it("module exports ackMockInboxItem and listMockInboxItems functions", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      expect(typeof mod.ackMockInboxItem).toBe("function");
      expect(typeof mod.listMockInboxItems).toBe("function");
    });
  });

  describe("documents list matches contract behavior", () => {
    it("filters tombstoned docs by default and sorts by updated_at desc", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const docs = mod.listMockDocuments();

      expect(docs.map((doc) => doc.id)).toEqual([
        "product-constitution",
        "incident-response-playbook",
        "onboarding-guide-v1",
      ]);
    });

    it("includes tombstoned docs when requested", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const docs = mod.listMockDocuments({ include_tombstoned: true });

      expect(docs.map((doc) => doc.id)).toEqual([
        "product-constitution",
        "incident-response-playbook",
        "old-pricing-doc",
        "onboarding-guide-v1",
      ]);
    });

    it("supports thread-scoped filtering and head revision summaries", async () => {
      const mod = await import("../../src/lib/mockCoreData.js");
      const docs = mod.listMockDocuments({ thread_id: "thread-governance" });

      expect(docs.map((doc) => doc.id)).toEqual(["product-constitution"]);
      expect(docs[0]?.head_revision).toMatchObject({
        revision_id: "rev-pc-3",
        revision_number: 3,
        content_type: "text",
      });
    });
  });
});
