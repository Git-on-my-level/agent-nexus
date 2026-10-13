import { describe, expect, it } from "vitest";

import {
  adoptPanelPreference,
  clearPanelPreference,
  PANEL_AUTO_COLLAPSE_BELOW,
  panelAutoCollapsed,
  panelCollapsed,
  panelStorageKey,
  readPanelPreference,
  SHELL_PANELS,
  writePanelPreference,
} from "../../src/lib/shellPanels.js";

/** A Storage stand-in: the module must never assume a browser. */
function fakeStorage(initial = {}) {
  const map = new Map(Object.entries(initial));
  return {
    getItem: (key) => (map.has(key) ? map.get(key) : null),
    setItem: (key, value) => map.set(key, String(value)),
    removeItem: (key) => map.delete(key),
    get size() {
      return map.size;
    },
  };
}

describe("panelStorageKey", () => {
  it("is scoped to the viewer, so a shared browser profile is not a shared layout", () => {
    expect(panelStorageKey(SHELL_PANELS.NAV, "agent-a")).not.toBe(
      panelStorageKey(SHELL_PANELS.NAV, "agent-b"),
    );
  });

  it("is scoped to the panel", () => {
    expect(panelStorageKey(SHELL_PANELS.NAV, "a")).not.toBe(
      panelStorageKey(SHELL_PANELS.RAIL, "a"),
    );
  });

  it("gives an unidentified viewer a key rather than no persistence", () => {
    expect(panelStorageKey(SHELL_PANELS.NAV, "")).toContain("anonymous");
  });
});

describe("adoptPanelPreference", () => {
  const NAV = SHELL_PANELS.NAV;

  it("adopts a choice made before the viewer was identified", () => {
    // The shell paints during bootstrap, so a collapse clicked then lands on
    // the anonymous key. Identifying must not undo it.
    const storage = fakeStorage();
    writePanelPreference(NAV, true, "", storage);
    expect(
      adoptPanelPreference(NAV, {
        fromViewerId: "",
        toViewerId: "agent-a",
        storage,
      }),
    ).toBe(true);
    expect(readPanelPreference(NAV, "agent-a", storage)).toBe(true);
  });

  it("clears the anonymous key it adopted, so the next viewer starts clean", () => {
    const storage = fakeStorage();
    writePanelPreference(NAV, true, "", storage);
    adoptPanelPreference(NAV, {
      fromViewerId: "",
      toViewerId: "agent-a",
      storage,
    });
    expect(readPanelPreference(NAV, "", storage)).toBeNull();
    expect(
      adoptPanelPreference(NAV, {
        fromViewerId: "",
        toViewerId: "agent-b",
        storage,
      }),
    ).toBeNull();
  });

  it("keeps the identified viewer's own choice over the anonymous one", () => {
    const storage = fakeStorage();
    writePanelPreference(NAV, true, "", storage);
    writePanelPreference(NAV, false, "agent-a", storage);
    expect(
      adoptPanelPreference(NAV, {
        fromViewerId: "",
        toViewerId: "agent-a",
        storage,
      }),
    ).toBe(false);
    // And the anonymous key is left alone: it was not adopted.
    expect(readPanelPreference(NAV, "", storage)).toBe(true);
  });

  it("never carries one real viewer's layout to another", () => {
    const storage = fakeStorage();
    writePanelPreference(NAV, true, "agent-a", storage);
    expect(
      adoptPanelPreference(NAV, {
        fromViewerId: "agent-a",
        toViewerId: "agent-b",
        storage,
      }),
    ).toBeNull();
    expect(readPanelPreference(NAV, "agent-a", storage)).toBe(true);
  });

  it("adopts nothing on the first read, when no viewer has been read yet", () => {
    // `null` is "nothing read yet", which is not the anonymous viewer.
    const storage = fakeStorage();
    writePanelPreference(NAV, true, "", storage);
    expect(
      adoptPanelPreference(NAV, {
        fromViewerId: null,
        toViewerId: "agent-a",
        storage,
      }),
    ).toBeNull();
    expect(readPanelPreference(NAV, "", storage)).toBe(true);
  });

  it("returns the stored value when the viewer has not changed", () => {
    const storage = fakeStorage();
    writePanelPreference(NAV, true, "", storage);
    expect(
      adoptPanelPreference(NAV, {
        fromViewerId: "",
        toViewerId: "",
        storage,
      }),
    ).toBe(true);
  });

  it("survives a storage that throws", () => {
    const hostile = {
      getItem() {
        throw new Error("blocked");
      },
      setItem() {
        throw new Error("blocked");
      },
      removeItem() {
        throw new Error("blocked");
      },
    };
    expect(
      adoptPanelPreference(NAV, {
        fromViewerId: "",
        toViewerId: "agent-a",
        storage: hostile,
      }),
    ).toBeNull();
    expect(() => clearPanelPreference(NAV, "a", hostile)).not.toThrow();
  });
});

describe("readPanelPreference / writePanelPreference", () => {
  it("round-trips both values", () => {
    const storage = fakeStorage();
    writePanelPreference(SHELL_PANELS.NAV, true, "me", storage);
    expect(readPanelPreference(SHELL_PANELS.NAV, "me", storage)).toBe(true);
    writePanelPreference(SHELL_PANELS.NAV, false, "me", storage);
    expect(readPanelPreference(SHELL_PANELS.NAV, "me", storage)).toBe(false);
  });

  it("is null when the viewer has never chosen", () => {
    expect(
      readPanelPreference(SHELL_PANELS.NAV, "me", fakeStorage()),
    ).toBeNull();
  });

  it("treats an unrecognised stored value as unset", () => {
    const storage = fakeStorage({
      [panelStorageKey(SHELL_PANELS.NAV, "me")]: "yes",
    });
    expect(readPanelPreference(SHELL_PANELS.NAV, "me", storage)).toBeNull();
  });

  it("survives a storage that throws", () => {
    const hostile = {
      getItem() {
        throw new Error("blocked");
      },
      setItem() {
        throw new Error("blocked");
      },
    };
    expect(readPanelPreference(SHELL_PANELS.NAV, "me", hostile)).toBeNull();
    expect(() =>
      writePanelPreference(SHELL_PANELS.NAV, true, "me", hostile),
    ).not.toThrow();
  });
});

describe("panelCollapsed", () => {
  const wide = PANEL_AUTO_COLLAPSE_BELOW.nav + 400;
  const narrow = PANEL_AUTO_COLLAPSE_BELOW.nav - 1;

  it("follows the viewer's choice on a wide window", () => {
    expect(
      panelCollapsed({
        panel: SHELL_PANELS.NAV,
        preference: true,
        viewportWidth: wide,
      }),
    ).toBe(true);
    expect(
      panelCollapsed({
        panel: SHELL_PANELS.NAV,
        preference: false,
        viewportWidth: wide,
      }),
    ).toBe(false);
  });

  it("collapses on a narrow window whatever the viewer chose", () => {
    expect(
      panelCollapsed({
        panel: SHELL_PANELS.NAV,
        preference: false,
        viewportWidth: narrow,
      }),
    ).toBe(true);
  });

  it("gives the viewer's choice back when the window widens again", () => {
    // The same preference, two widths: the override is not a write.
    const preference = false;
    expect(
      panelCollapsed({
        panel: SHELL_PANELS.NAV,
        preference,
        viewportWidth: narrow,
      }),
    ).toBe(true);
    expect(
      panelCollapsed({
        panel: SHELL_PANELS.NAV,
        preference,
        viewportWidth: wide,
      }),
    ).toBe(false);
  });

  it("is expanded by default", () => {
    expect(
      panelCollapsed({ panel: SHELL_PANELS.NAV, viewportWidth: wide }),
    ).toBe(false);
  });

  it("does not treat an unmeasured width as very narrow", () => {
    // Server render: width 0 must not collapse everything.
    expect(
      panelCollapsed({
        panel: SHELL_PANELS.NAV,
        preference: false,
        viewportWidth: 0,
      }),
    ).toBe(false);
  });

  it("never auto-collapses the rail: stacked is not collapsed", () => {
    // Below `xl` the rail sits under the content rather than beside it, so
    // collapsing it there would hide the Source block and the Inbox link to
    // save width the rail is no longer taking.
    expect(PANEL_AUTO_COLLAPSE_BELOW.rail).toBe(0);
    for (const viewportWidth of [390, 768, 1024, 1600]) {
      expect(
        panelCollapsed({
          panel: SHELL_PANELS.RAIL,
          preference: false,
          viewportWidth,
        }),
      ).toBe(false);
    }
    // The viewer's choice still applies at every width.
    expect(
      panelCollapsed({
        panel: SHELL_PANELS.RAIL,
        preference: true,
        viewportWidth: 390,
      }),
    ).toBe(true);
  });

  it("audits the widths David named: 390 and 768 collapse the nav", () => {
    for (const viewportWidth of [390, 768]) {
      expect(
        panelCollapsed({
          panel: SHELL_PANELS.NAV,
          preference: false,
          viewportWidth,
        }),
      ).toBe(true);
    }
  });

  it("leaves the nav expanded at 1024, where the sidebar first has room", () => {
    // `lg` is where the shell swaps the bottom tab bar for the sidebar. The
    // sidebar is 232px of it and the rest is a comfortable column, so the
    // viewer's choice rules from here up rather than being overridden.
    expect(
      panelCollapsed({
        panel: SHELL_PANELS.NAV,
        preference: false,
        viewportWidth: 1024,
      }),
    ).toBe(false);
    expect(
      panelCollapsed({
        panel: SHELL_PANELS.NAV,
        preference: false,
        viewportWidth: 1023,
      }),
    ).toBe(true);
  });
});

describe("panelAutoCollapsed", () => {
  it("says when the window, not the viewer, is what collapsed it", () => {
    expect(
      panelAutoCollapsed({ panel: SHELL_PANELS.NAV, viewportWidth: 390 }),
    ).toBe(true);
    expect(
      panelAutoCollapsed({ panel: SHELL_PANELS.NAV, viewportWidth: 1600 }),
    ).toBe(false);
  });

  it("is false before the width is measured", () => {
    expect(
      panelAutoCollapsed({ panel: SHELL_PANELS.NAV, viewportWidth: 0 }),
    ).toBe(false);
  });
});
