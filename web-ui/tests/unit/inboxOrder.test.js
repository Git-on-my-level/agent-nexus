import { describe, expect, it } from "vitest";
import { createInboxOrder } from "../../src/lib/inboxOrder.js";

const row = (id, stale = false) => ({ id, stale });
const visible = (result) =>
  [...result.currentRows, ...result.staleRows, ...result.lateRows].map(
    (item) => item.id,
  );

describe("Inbox arrival order", () => {
  it("preserves the first ranked rows and appends late high-priority rows", () => {
    const order = createInboxOrder();
    const ask = row("ask");
    const task = row("task");
    expect(visible(order("needs-you", [ask, task], false))).toEqual([
      "ask",
      "task",
    ]);
    const next = order(
      "needs-you",
      [row("decision"), ask, row("older-task"), task],
      false,
    );
    expect(visible(next)).toEqual(["ask", "task", "decision", "older-task"]);
    expect(next.added).toBe(2);
    expect(visible(order("watching", [row("decision"), ask], false))).toEqual([
      "decision",
      "ask",
    ]);
  });

  it("keeps fresh arrivals below an expanded stale selection and folds stale arrivals", () => {
    const order = createInboxOrder();
    const stale = row("stale", true);
    order("needs-you", [row("ask"), stale], true);
    const next = order(
      "needs-you",
      [row("decision"), row("ask"), stale, row("late-stale", true)],
      true,
    );
    expect(visible(next)).toEqual(["ask", "stale", "late-stale", "decision"]);
    expect(next.staleRows.map((item) => item.id)).toEqual([
      "stale",
      "late-stale",
    ]);
    const folded = order(
      "needs-you",
      [row("decision"), row("ask"), stale, row("late-stale", true)],
      false,
    );
    expect(folded.lateRows.map((item) => item.id)).toEqual(["decision"]);
    expect(folded.staleRows.map((item) => item.id)).toEqual([
      "stale",
      "late-stale",
    ]);
  });
});
