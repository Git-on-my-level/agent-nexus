import { describe, expect, it } from "vitest";

import { resolveRefLink } from "../../src/lib/refLinkModel.js";

describe("artifact ref rendering", () => {
  it("renders known artifact metadata refs as deterministic links", () => {
    const refs = [
      "artifact:artifact-receipt-seed",
      "thread:thread-onboarding",
      "url:https://example.com/logs/incident-42",
    ];

    const resolved = refs.map((refValue) =>
      resolveRefLink(refValue, { threadId: "thread-onboarding" }),
    );

    expect(resolved).toEqual([
      {
        raw: "artifact:artifact-receipt-seed",
        prefix: "artifact",
        value: "artifact-receipt-seed",
        kind: "artifact",
        label: "artifact:artifact-receipt-seed",
        href: "/artifacts/artifact-receipt-seed",
        isExternal: false,
        isLink: true,
      },
      {
        raw: "thread:thread-onboarding",
        prefix: "thread",
        value: "thread-onboarding",
        kind: "thread",
        label: "thread:thread-onboarding",
        href: "/threads/thread-onboarding",
        isExternal: false,
        isLink: true,
      },
      {
        raw: "url:https://example.com/logs/incident-42",
        prefix: "url",
        value: "https://example.com/logs/incident-42",
        kind: "url",
        label: "url:https://example.com/logs/incident-42",
        href: "https://example.com/logs/incident-42",
        isExternal: true,
        isLink: true,
      },
    ]);
  });

  it("preserves unknown artifact refs as raw text", () => {
    const resolved = resolveRefLink("vendor_blob:abc123", {
      threadId: "thread-onboarding",
    });

    expect(resolved).toMatchObject({
      kind: "unknown",
      label: "vendor_blob:abc123",
      isLink: false,
      href: "",
    });
  });
});
