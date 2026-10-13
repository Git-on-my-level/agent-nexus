import { describe, expect, it } from "vitest";

import {
  avatarSizeClasses,
  hashSeed,
  initialsOf,
  paletteForSeed,
  truncateActorDisplayName,
} from "../../src/lib/avatarModel.js";

describe("avatarModel", () => {
  it("initialsOf handles multi-part labels and empty input", () => {
    expect(initialsOf("Local Dev")).toBe("LD");
    expect(initialsOf("agent.handle")).toBe("AH");
    expect(initialsOf("")).toBe("·");
  });

  it("initialsOf ignores bracketed roles and punctuation", () => {
    expect(initialsOf("Jordan (Human operator)")).toBe("JO");
    expect(initialsOf("Zara (Ops AI)")).toBe("ZA");
    expect(initialsOf("Iris Park [docs]")).toBe("IP");
    expect(initialsOf("“Quoted” Name")).toBe("QN");
    expect(initialsOf("(bot)")).toBe("BO");
  });

  it("paletteForSeed is deterministic for the same seed", () => {
    const a = paletteForSeed("actor-123", "fallback");
    const b = paletteForSeed("actor-123", "other");
    expect(a).toEqual(b);
    expect(a.bg).toMatch(/^bg-/);
  });

  it("hashSeed is stable", () => {
    expect(hashSeed("same")).toBe(hashSeed("same"));
    expect(hashSeed("other")).not.toBe(hashSeed("same"));
  });

  it("truncateActorDisplayName shortens long handles", () => {
    expect(truncateActorDisplayName("Local Dev")).toBe("Local Dev");
    expect(
      truncateActorDisplayName("very.long.agent.handle.name.here"),
    ).toMatch(/…$/);
  });

  it("avatarSizeClasses maps sizes", () => {
    expect(avatarSizeClasses("xs")).toContain("h-5");
    expect(avatarSizeClasses("lg")).toContain("h-9");
  });
});
