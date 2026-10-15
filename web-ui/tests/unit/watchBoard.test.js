// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/svelte";
import { tick } from "svelte";
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

// jsdom has no Web Animations; the board animates cards into place (flip).
if (typeof Element.prototype.getAnimations !== "function")
  Element.prototype.getAnimations = () => [];
if (typeof Element.prototype.animate !== "function")
  Element.prototype.animate = () => ({
    cancel() {},
    finish() {},
    onfinish: null,
    finished: Promise.resolve(),
  });

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

it("lands a dropped card in its new column before the move resolves", async () => {
  let resolve;
  const onMove = vi.fn(() => new Promise((r) => (resolve = r)));
  const { container } = render(WatchBoard, {
    initiatives: [initiative],
    onMove,
  });
  layout(container);
  await pan(container.querySelector('[data-work-ref="card:ship"]'), "mouse");
  expect(onMove).toHaveBeenCalledTimes(1);
  const ready = container.querySelector('[data-watch-column="ready"]');
  expect(ready.querySelector('[data-work-ref="card:ship"]')).not.toBeNull();
  expect(
    ready
      .querySelector('[data-work-ref="card:ship"]')
      .getAttribute("aria-busy"),
  ).toBe("true");
  // The page declined the move (nothing changed in the rows): it goes back.
  resolve();
  await new Promise((r) => setTimeout(r, 0));
  await Promise.resolve();
  expect(
    container
      .querySelector('[data-watch-column="backlog"]')
      .querySelector('[data-work-ref="card:ship"]'),
  ).not.toBeNull();
});

it("moves a focused card one stage with the arrow keys", async () => {
  const onMove = vi.fn();
  const { container } = render(WatchBoard, {
    initiatives: [initiative],
    onMove,
  });
  const slot = container.querySelector('[data-work-ref="card:ship"]');
  await fireEvent.keyDown(slot, { key: "ArrowRight" });
  expect(onMove).toHaveBeenCalledTimes(1);
  expect(onMove.mock.calls[0][1]).toBe("ready");
  await fireEvent.keyDown(slot, { key: "ArrowLeft" });
  // Already in the first stage: nothing to move to.
  expect(onMove).toHaveBeenCalledTimes(1);
});

it("ignores a repeated arrow key", async () => {
  const onMove = vi.fn();
  const { container } = render(WatchBoard, {
    initiatives: [initiative],
    onMove,
  });
  const slot = container.querySelector('[data-work-ref="card:ship"]');
  await fireEvent.keyDown(slot, { key: "ArrowRight", repeat: true });
  expect(onMove).not.toHaveBeenCalled();
  await fireEvent.keyDown(slot, { key: "ArrowRight" });
  expect(onMove).toHaveBeenCalledTimes(1);
});

it("does not send the card under the drop gap as an anchor", async () => {
  const onMove = vi.fn();
  const peer = {
    ...initiative,
    ref: "card:peer",
    id: "peer",
    title: "Peer on another board",
    placement: "ready",
  };
  const { container } = render(WatchBoard, {
    initiatives: [initiative, peer],
    onMove,
  });
  layout(container);
  for (const slot of container.querySelectorAll("[data-work-slot]")) {
    const column = slot.closest("[data-watch-column]");
    const index = ["backlog", "ready", "in_progress", "review", "done"].indexOf(
      column.getAttribute("data-watch-column"),
    );
    slot.getBoundingClientRect = () => box(index * 280 + 8, 40, 240, 96);
  }
  await pan(container.querySelector('[data-work-ref="card:ship"]'), "mouse");
  expect(onMove).toHaveBeenCalledTimes(1);
  expect(onMove.mock.calls[0][1]).toBe("ready");
  expect(onMove.mock.calls[0][2]).toBe("");
});

it("keeps the latest column when an older move settles", async () => {
  let resolveFirst;
  const onMove = vi.fn((row, placement) => {
    if (placement === "ready")
      return new Promise((resolve) => (resolveFirst = resolve));
    return Promise.resolve(true);
  });
  const { container } = render(WatchBoard, {
    initiatives: [initiative],
    onMove,
  });
  const slot = container.querySelector('[data-work-ref="card:ship"]');
  await fireEvent.keyDown(slot, { key: "ArrowRight" });
  await tick();
  const ready = container.querySelector(
    '[data-watch-column="ready"] [data-work-ref="card:ship"]',
  );
  await fireEvent.keyDown(ready, { key: "ArrowRight" });
  expect(onMove.mock.calls.map((call) => call[1])).toEqual([
    "ready",
    "in_progress",
  ]);
  await tick();
  resolveFirst(false);
  await tick();
  expect(
    container.querySelector(
      '[data-watch-column="in_progress"] [data-work-ref="card:ship"]',
    ),
  ).not.toBeNull();
});

it("keeps a landed card when a brief refresh still shows the old column", async () => {
  let resolve;
  const onMove = vi.fn(() => new Promise((r) => (resolve = r)));
  const { container, rerender } = render(WatchBoard, {
    initiatives: [initiative],
    onMove,
  });
  layout(container);
  await pan(container.querySelector('[data-work-ref="card:ship"]'), "mouse");
  expect(
    container.querySelector(
      '[data-watch-column="ready"] [data-work-ref="card:ship"]',
    ),
  ).not.toBeNull();
  await rerender({ initiatives: [{ ...initiative }], onMove });
  expect(
    container.querySelector(
      '[data-watch-column="ready"] [data-work-ref="card:ship"]',
    ),
  ).not.toBeNull();
  resolve(true);
  await tick();
  await rerender({ initiatives: [{ ...initiative }], onMove });
  expect(
    container.querySelector(
      '[data-watch-column="ready"] [data-work-ref="card:ship"]',
    ),
  ).not.toBeNull();
});
