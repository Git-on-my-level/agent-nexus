// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/svelte";
import { afterEach, expect, it, vi } from "vitest";

import WatchBoard from "../../src/lib/components/watch/WatchBoard.svelte";

vi.mock("$app/stores", () => ({
  page: {
    subscribe(run) {
      run({
        url: new URL("http://localhost/o/acme/w/ops/watch"),
        params: { organization: "acme", workspace: "ops" },
      });
      return () => {};
    },
  },
}));

const initiative = {
  ref: "card:ship",
  id: "ship",
  title: "Ship the brief",
  state: "done",
  placement: "backlog",
  promise: null,
  progress: null,
  blockedCount: 0,
  waitingOnYouCount: 0,
  note: null,
  possiblyMoot: null,
  promiseMoves: 0,
  lastEvidenceAt: "",
};

function box(left, top, width, height) {
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

function layout(container) {
  const order = ["backlog", "ready", "in_progress", "review", "done"];
  for (const column of container.querySelectorAll("[data-watch-column]")) {
    const index = order.indexOf(column.getAttribute("data-watch-column"));
    column.getBoundingClientRect = () => box(index * 280, 0, 256, 480);
  }
}

async function pan(slot, pointerType) {
  await fireEvent.pointerDown(slot, {
    button: 0,
    pointerId: 1,
    pointerType,
    clientX: 20,
    clientY: 30,
  });
  await fireEvent.pointerMove(window, {
    pointerId: 1,
    pointerType,
    clientX: 300,
    clientY: 40,
  });
  await fireEvent.pointerUp(window, {
    pointerId: 1,
    pointerType,
    clientX: 300,
    clientY: 40,
  });
}

afterEach(cleanup);

it("does not move an initiative when a touch pans across the board", async () => {
  const onMove = vi.fn();
  const { container } = render(WatchBoard, {
    initiatives: [initiative],
    onMove,
  });
  layout(container);
  const slot = container.querySelector('[data-work-ref="card:ship"]');
  expect(container.querySelector("[data-watch-board]").className).toContain(
    "overflow-x-auto",
  );

  await pan(slot, "touch");

  expect(onMove).not.toHaveBeenCalled();
  expect(slot.className).not.toContain("touch-pan-y");
});

it("still moves an initiative when a mouse drag ends on another column", async () => {
  const onMove = vi.fn();
  const { container } = render(WatchBoard, {
    initiatives: [initiative],
    onMove,
  });
  layout(container);
  const slot = container.querySelector('[data-work-ref="card:ship"]');

  await pan(slot, "mouse");

  expect(onMove).toHaveBeenCalledTimes(1);
  expect(onMove.mock.calls[0][0].ref).toBe("card:ship");
  expect(onMove.mock.calls[0][1]).toBe("ready");
  expect(onMove.mock.calls[0][2]).toBe("");
});
