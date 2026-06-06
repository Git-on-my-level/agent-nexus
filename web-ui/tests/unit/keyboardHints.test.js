// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  formatShortcut,
  isMacPlatform,
  modSymbol,
} from "../../src/lib/keyboardHints.js";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("keyboardHints", () => {
  it("isMacPlatform detects Mac user agents", () => {
    vi.stubGlobal("navigator", { platform: "MacIntel" });
    expect(isMacPlatform()).toBe(true);
    vi.stubGlobal("navigator", { platform: "Win32" });
    expect(isMacPlatform()).toBe(false);
  });

  it("modSymbol returns platform-appropriate modifier label", () => {
    vi.stubGlobal("navigator", { platform: "MacIntel" });
    expect(modSymbol()).toBe("⌘");
    vi.stubGlobal("navigator", { platform: "Win32" });
    expect(modSymbol()).toBe("Ctrl");
  });

  it("formatShortcut builds Mac and Windows labels", () => {
    vi.stubGlobal("navigator", { platform: "MacIntel" });
    expect(formatShortcut("S")).toBe("⌘S");
    expect(formatShortcut("Enter")).toBe("⌘Enter");
    expect(formatShortcut("M", { alt: true })).toBe("⌥⌘M");

    vi.stubGlobal("navigator", { platform: "Win32" });
    expect(formatShortcut("S")).toBe("Ctrl+S");
    expect(formatShortcut("Enter")).toBe("Ctrl+Enter");
    expect(formatShortcut("M", { alt: true })).toBe("Alt+Ctrl+M");
  });
});
