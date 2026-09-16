// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render } from "@testing-library/svelte";

import WorkViews from "../../src/lib/components/pm/WorkViews.svelte";
import { sortWorkBoardItems, workKey } from "../../src/lib/pm/presentation.js";
import {
  DRAG_PLACEHOLDER_KEY,
  beforeCardRefForInsert,
  boardPhasePeers,
  columnAtPoint,
  columnSlots,
  insertIndexAtY,
  placeWorkInPhase,
} from "../../src/lib/workBoardDrag.js";

function rect(left, top, width, height) {
  return {
    left,
    top,
    right: left + width,
    bottom: top + height,
    width,
    height,
    x: left,
    y: top,
  };
}

describe("workBoardDrag", () => {
  it("opens a hole at the pointer so remaining cards shift", () => {
    const items = [
      { ref: "card:a", title: "A" },
      { ref: "card:b", title: "B" },
      { ref: "card:c", title: "C" },
    ];
    const slots = columnSlots(items, workKey(items[0]), "review", {
      phase: "review",
      index: 1,
    });
    expect(slots.map((slot) => slot.key)).toEqual([
      "card:b",
      DRAG_PLACEHOLDER_KEY,
      "card:c",
    ]);
  });

  it("leaves other columns without a hole", () => {
    const items = [{ ref: "card:a", title: "A" }];
    expect(
      columnSlots(items, "card:a", "backlog", {
        phase: "review",
        index: 0,
      }),
    ).toEqual([]);
  });

  it("picks the column whose box contains the pointer", () => {
    const root = document.createElement("div");
    const backlog = document.createElement("section");
    backlog.setAttribute("data-work-phase-column", "");
    backlog.getBoundingClientRect = () => rect(0, 0, 100, 200);
    const review = document.createElement("section");
    review.setAttribute("data-work-phase-column", "");
    review.getBoundingClientRect = () => rect(120, 0, 100, 200);
    root.append(backlog, review);
    expect(columnAtPoint(150, 40, root)).toBe(review);
  });

  it("inserts above the card whose midline the pointer has not passed", () => {
    const column = document.createElement("div");
    const first = document.createElement("div");
    first.setAttribute("data-work-slot", "");
    first.setAttribute("data-work-ref", "card:b");
    first.getBoundingClientRect = () => rect(0, 0, 80, 40);
    const second = document.createElement("div");
    second.setAttribute("data-work-slot", "");
    second.setAttribute("data-work-ref", "card:c");
    second.getBoundingClientRect = () => rect(0, 48, 80, 40);
    column.append(first, second);
    expect(insertIndexAtY(column, 10, "card:a")).toBe(0);
    expect(insertIndexAtY(column, 50, "card:a")).toBe(1);
    expect(insertIndexAtY(column, 90, "card:a")).toBe(2);
  });

  it("places a dropped card in the hole, not at the end of the column", () => {
    const records = [
      { ref: "card:a", phase: "backlog" },
      { ref: "card:x", phase: "ready" },
      { ref: "card:y", phase: "ready" },
    ];
    const next = placeWorkInPhase(records, records[0], "ready", 1);
    expect(next.map((item) => item.ref)).toEqual([
      "card:x",
      "card:a",
      "card:y",
    ]);
    expect(next[1].phase).toBe("ready");
  });

  it("reorders within a column using the same remaining-card index as the hole", () => {
    const records = [
      { ref: "card:a", phase: "backlog" },
      { ref: "card:b", phase: "backlog" },
      { ref: "card:c", phase: "backlog" },
    ];
    expect(
      placeWorkInPhase(records, records[1], "backlog", 0).map(
        (item) => item.ref,
      ),
    ).toEqual(["card:b", "card:a", "card:c"]);
    expect(
      placeWorkInPhase(records, records[1], "backlog", 2).map(
        (item) => item.ref,
      ),
    ).toEqual(["card:a", "card:c", "card:b"]);
  });

  it("persists a drop index from rank order, not list recency", () => {
    const records = [
      { ref: "card:c", phase: "backlog", board_ref: "board:a", rank: "3" },
      { ref: "card:b", phase: "backlog", board_ref: "board:a", rank: "2" },
      { ref: "card:a", phase: "backlog", board_ref: "board:a", rank: "1" },
    ];
    expect(
      beforeCardRefForInsert(
        records[0],
        boardPhasePeers(records, "backlog", "card:c"),
        0,
      ),
    ).toBe("card:a");
    expect(
      sortWorkBoardItems(
        placeWorkInPhase(records, records[0], "backlog", 0),
      ).map((item) => item.ref),
    ).toEqual(["card:c", "card:a", "card:b"]);
  });

  it("picks the next same-board neighbor as before_card_id", () => {
    const work = { ref: "card:a", board_ref: "board:studio" };
    const peers = [
      { ref: "card:other", board_ref: "board:elsewhere" },
      { ref: "card:b", board_ref: "board:studio" },
      { ref: "card:c", board_ref: "board:studio" },
    ];
    expect(beforeCardRefForInsert(work, peers, 0)).toBe("card:b");
    expect(beforeCardRefForInsert(work, peers, 1)).toBe("card:b");
    expect(beforeCardRefForInsert(work, peers, 2)).toBe("card:c");
    expect(beforeCardRefForInsert(work, peers, 3)).toBe("");
  });
});

describe("WorkViews board drag", () => {
  it("moves a card when it is dropped on another phase column", async () => {
    Element.prototype.getAnimations ??= function getAnimations() {
      return [];
    };
    const onMove = vi.fn();
    const rows = [
      { ref: "card:one", title: "Sample one", phase: "backlog" },
      { ref: "card:two", title: "Sample two", phase: "in_progress" },
    ];
    const result = render(WorkViews, {
      records: rows,
      view: "board",
      workspaceHref: (path) => path,
      onMove,
    });
    const card = result.container.querySelector('[data-work-ref="card:one"]');
    const target = result.container.querySelector(
      'section[aria-label="In progress"]',
    );
    expect(card).toBeTruthy();
    expect(target).toBeTruthy();
    card.getBoundingClientRect = () => ({
      left: 10,
      top: 10,
      right: 110,
      bottom: 80,
      width: 100,
      height: 70,
      x: 10,
      y: 10,
    });
    target.getBoundingClientRect = () => ({
      left: 200,
      top: 0,
      right: 400,
      bottom: 400,
      width: 200,
      height: 400,
      x: 200,
      y: 0,
    });
    await fireEvent.pointerDown(card, {
      button: 0,
      pointerId: 1,
      clientX: 20,
      clientY: 30,
    });
    await fireEvent.pointerMove(window, {
      pointerId: 1,
      clientX: 250,
      clientY: 80,
    });
    await fireEvent.pointerUp(window, {
      pointerId: 1,
      clientX: 250,
      clientY: 80,
    });
    expect(onMove).toHaveBeenCalledTimes(1);
    expect(workKey(onMove.mock.calls[0][0])).toBe("card:one");
    expect(onMove.mock.calls[0][1]).toBe("in_progress");
    expect(onMove.mock.calls[0][2]).toEqual({
      index: expect.any(Number),
      pointer: true,
    });
  });

  it("does not abort the pointer session when the browser tries to drag the card link", async () => {
    Element.prototype.getAnimations ??= function getAnimations() {
      return [];
    };
    const onMove = vi.fn();
    const result = render(WorkViews, {
      records: [
        { ref: "card:one", title: "Sample one", phase: "backlog" },
        { ref: "card:two", title: "Sample two", phase: "in_progress" },
      ],
      view: "board",
      workspaceHref: (path) => path,
      onMove,
    });
    const card = result.container.querySelector('[data-work-ref="card:one"]');
    const target = result.container.querySelector(
      'section[aria-label="In progress"]',
    );
    const link = card.querySelector("a[href]");
    card.getBoundingClientRect = () => ({
      left: 10,
      top: 10,
      right: 110,
      bottom: 80,
      width: 100,
      height: 70,
      x: 10,
      y: 10,
    });
    target.getBoundingClientRect = () => ({
      left: 200,
      top: 0,
      right: 400,
      bottom: 400,
      width: 200,
      height: 400,
      x: 200,
      y: 0,
    });
    await fireEvent.pointerDown(card, {
      button: 0,
      pointerId: 1,
      clientX: 20,
      clientY: 30,
    });
    const dragStart = new Event("dragstart", {
      bubbles: true,
      cancelable: true,
    });
    link.dispatchEvent(dragStart);
    expect(dragStart.defaultPrevented).toBe(true);
    await fireEvent.pointerMove(window, {
      pointerId: 1,
      clientX: 250,
      clientY: 80,
    });
    await fireEvent.pointerUp(window, {
      pointerId: 1,
      clientX: 250,
      clientY: 80,
    });
    expect(onMove).toHaveBeenCalledTimes(1);
    expect(onMove.mock.calls[0][1]).toBe("in_progress");
  });

  it("keeps the drop hole until the parent list matches it", async () => {
    Element.prototype.getAnimations ??= function getAnimations() {
      return [];
    };
    let holeDuringMove = false;
    const rows = [
      {
        ref: "card:one",
        title: "Sample one",
        phase: "backlog",
        source: { authority: "nexus" },
      },
      {
        ref: "card:two",
        title: "Sample two",
        phase: "in_progress",
        source: { authority: "nexus" },
      },
    ];
    const result = render(WorkViews, {
      records: rows,
      view: "board",
      workspaceHref: (path) => path,
      onMove: () => {
        holeDuringMove = Boolean(
          result.container.querySelector("[data-placeholder]"),
        );
      },
    });
    const card = result.container.querySelector('[data-work-ref="card:one"]');
    const target = result.container.querySelector(
      'section[aria-label="In progress"]',
    );
    card.getBoundingClientRect = () => ({
      left: 10,
      top: 10,
      right: 110,
      bottom: 80,
      width: 100,
      height: 70,
      x: 10,
      y: 10,
    });
    target.getBoundingClientRect = () => ({
      left: 200,
      top: 0,
      right: 400,
      bottom: 400,
      width: 200,
      height: 400,
      x: 200,
      y: 0,
    });
    await fireEvent.pointerDown(card, {
      button: 0,
      pointerId: 1,
      clientX: 20,
      clientY: 30,
    });
    await fireEvent.pointerMove(window, {
      pointerId: 1,
      clientX: 250,
      clientY: 80,
    });
    await fireEvent.pointerUp(window, {
      pointerId: 1,
      clientX: 250,
      clientY: 80,
    });
    expect(holeDuringMove).toBe(true);
  });
});
