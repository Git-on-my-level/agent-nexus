import { describe, expect, it } from "vitest";

import { layoutPatch, watchView } from "../../src/lib/watchView.js";

const view = (query) => watchView(new URLSearchParams(query));

describe("watchView", () => {
  it("lands on the Initiatives list", () => {
    expect(view("")).toEqual({ tab: "initiatives", layout: "list" });
  });

  it("opens the swimlane board from ?view=lanes", () => {
    expect(view("view=lanes")).toEqual({ tab: "initiatives", layout: "lanes" });
  });

  it("sends old by-stage links to the swimlane board", () => {
    expect(view("view=board")).toEqual({ tab: "initiatives", layout: "lanes" });
    expect(view("layout=board")).toEqual({
      tab: "initiatives",
      layout: "lanes",
    });
  });

  it("keeps the Loops board on the Loops tab", () => {
    expect(view("tab=loops&view=board")).toEqual({
      tab: "loops",
      layout: "board",
    });
    expect(view("tab=loops&layout=board")).toEqual({
      tab: "loops",
      layout: "board",
    });
    expect(view("tab=loops")).toEqual({ tab: "loops", layout: "list" });
    // The swimlane board is an initiatives layout; on Loops it is the board.
    expect(view("tab=loops&view=lanes")).toEqual({
      tab: "loops",
      layout: "board",
    });
  });

  it("still lands old tab links", () => {
    expect(view("view=loops")).toEqual({ tab: "loops", layout: "list" });
    expect(view("dashboard=metrics")).toEqual({
      tab: "dashboard",
      layout: "list",
    });
  });

  it("opens Trends, which has no layout", () => {
    expect(view("tab=trends&view=lanes")).toEqual({
      tab: "trends",
      layout: "list",
    });
  });

  it("has no layout on a dashboard", () => {
    expect(view("tab=dashboard&view=lanes").layout).toBe("list");
  });

  it("ignores unknown values", () => {
    expect(view("tab=nope&view=grid")).toEqual({
      tab: "initiatives",
      layout: "list",
    });
  });
});

describe("layoutPatch", () => {
  it("writes ?view= and drops the old ?layout=", () => {
    expect(layoutPatch("lanes")).toEqual({ view: "lanes", layout: "" });
    expect(layoutPatch("board")).toEqual({ view: "board", layout: "" });
    expect(layoutPatch("list")).toEqual({ view: "", layout: "" });
  });
});
