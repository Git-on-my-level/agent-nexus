import { describe, expect, it } from "vitest";

import { readEnumSearchParamWithAliases } from "../../src/lib/urlState.js";

describe("readEnumSearchParamWithAliases", () => {
  it("accepts values in the allowed list", () => {
    const sp = new URLSearchParams("tab=about");
    expect(
      readEnumSearchParamWithAliases(sp, "tab", [
        "messages",
        "about",
        "timeline",
      ]),
    ).toBe("about");
  });

  it("maps legacy overview to about when allowed", () => {
    const sp = new URLSearchParams("tab=overview");
    expect(
      readEnumSearchParamWithAliases(
        sp,
        "tab",
        ["messages", "about", "timeline"],
        { overview: "about" },
      ),
    ).toBe("about");
  });

  it("rejects unknown tab values to default", () => {
    const sp = new URLSearchParams("tab=garbage");
    expect(
      readEnumSearchParamWithAliases(
        sp,
        "tab",
        ["messages", "about"],
        { overview: "about" },
        "",
      ),
    ).toBe("");
  });
});
