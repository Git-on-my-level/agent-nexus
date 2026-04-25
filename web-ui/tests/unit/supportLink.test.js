import { describe, expect, it } from "vitest";

import {
  DEFAULT_HOSTED_SUPPORT_HREF,
  resolveHostedSupportUrl,
  supportLinkOpensInNewTab,
} from "../../src/lib/hosted/supportLink.js";

describe("resolveHostedSupportUrl", () => {
  it("uses default when unset or blank", () => {
    expect(resolveHostedSupportUrl()).toBe(DEFAULT_HOSTED_SUPPORT_HREF);
    expect(resolveHostedSupportUrl("  ")).toBe(DEFAULT_HOSTED_SUPPORT_HREF);
  });

  it("trims and returns configured https URL", () => {
    expect(resolveHostedSupportUrl("  https://example.com/help  ")).toBe(
      "https://example.com/help",
    );
  });

  it("returns mailto as-is", () => {
    expect(resolveHostedSupportUrl("mailto:a@b.co")).toBe("mailto:a@b.co");
  });
});

describe("supportLinkOpensInNewTab", () => {
  it("is true for http(s)", () => {
    expect(supportLinkOpensInNewTab("https://a")).toBe(true);
    expect(supportLinkOpensInNewTab("http://a")).toBe(true);
  });

  it("is false for mailto", () => {
    expect(supportLinkOpensInNewTab("mailto:support@example.com")).toBe(
      false,
    );
  });
});
