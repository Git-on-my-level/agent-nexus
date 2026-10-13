import { describe, expect, it } from "vitest";

import {
  createInflightSlot,
  patchSearchParams,
  readEnumSearchParamWithAliases,
} from "../../src/lib/urlState.js";

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

describe("createInflightSlot", () => {
  it("returns the overlay until cleared, then the fallback", () => {
    const slot = createInflightSlot();
    expect(slot.get("fallback")).toBe("fallback");
    slot.set("pending");
    expect(slot.get("fallback")).toBe("pending");
    slot.clear();
    expect(slot.get("fallback")).toBe("fallback");
  });
});

describe("patchSearchParams", () => {
  it("layers a second patch onto the first without a live URL", () => {
    const first = patchSearchParams("", { project_ref: "topic:a" });
    const second = patchSearchParams(first, { source: "github" });
    expect(second.toString()).toBe("project_ref=topic%3Aa&source=github");
  });

  it("deletes a key when the new value is empty", () => {
    const params = patchSearchParams("source=github&q=one", { source: "" });
    expect(params.toString()).toBe("q=one");
  });
});
