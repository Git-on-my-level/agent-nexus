import { expect, test } from "@playwright/test";

import { findClippedContent } from "../helpers/workspaceApiMock.js";

/**
 * Tests for the clipping detector itself.
 *
 * `expectNoClippedContent` is the only check that sees content the workspace
 * shell cuts off silently — the shell clips its own main column, so the
 * document never scrolls sideways and the layout audit's overflow check stays
 * quiet. That makes the detector load-bearing: a hole in it passes exactly the
 * layouts it exists to catch, and nothing else is watching.
 *
 * It has had a hole at each end. It used to stop at the first clipping
 * ancestor, which called a working horizontal scroller a clip. The fix for
 * that introduced the opposite: an element with an ellipsis was accepted
 * immediately, without asking whether anything outside it had already cut the
 * ellipsis off the screen.
 *
 * So these are the detector's own fixtures, built from plain HTML rather than
 * from the app: each one is a layout whose verdict is obvious to a reader.
 */

const PHONE = { width: 390, height: 700 };

/** A page with no app, no fonts and no CSS but its own. */
async function layout(page, body) {
  await page.setViewportSize(PHONE);
  await page.setContent(
    `<!doctype html><html><head><meta charset="utf-8"><style>
       * { margin: 0; padding: 0; box-sizing: border-box; }
       body { font: 14px/1.4 system-ui, sans-serif; }
     </style></head><body>${body}</body></html>`,
  );
  await page.evaluate(() => document.fonts?.ready);
}

test("an ellipsis inside a narrower clipping parent is still clipped", async ({
  page,
}) => {
  // The reviewer's reproduction. The inner box says "I truncate" and it does —
  // but its own right edge, where the ellipsis would be, is 500px outside a
  // parent that cannot be scrolled. The reader sees text run to the edge of a
  // box and stop, with nothing to say more was there.
  await layout(
    page,
    `<div style="width: 300px; overflow: hidden">
       <div
         id="victim"
         style="width: 800px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis"
       >A step title long enough that it cannot possibly fit in three hundred pixels of column</div>
     </div>`,
  );

  const offenders = await findClippedContent(page);
  expect(offenders.join("\n")).toContain("A step title long enough");
});

test("an ellipsis that fits its parent is reachable", async ({ page }) => {
  // The same box, sized to its parent: the text is cut, but the ellipsis is on
  // screen saying so. This is the state the product is allowed to be in.
  await layout(
    page,
    `<div style="width: 300px; overflow: hidden">
       <div
         style="width: 300px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis"
       >A step title long enough that it cannot possibly fit in three hundred pixels of column</div>
     </div>`,
  );

  expect(await findClippedContent(page)).toEqual([]);
});

test("content past the right edge of a horizontal scroller is reachable", async ({
  page,
}) => {
  // The plan diagram: a node sitting beyond the fold of a box that scrolls
  // sideways is one gesture away, not lost. This is the case the previous
  // version got wrong in the other direction.
  await layout(
    page,
    `<div style="width: 300px; overflow-x: auto">
       <div style="width: 900px">
         <div style="width: 200px; margin-left: 640px; overflow: hidden">
           Release B shipped to every workspace
         </div>
       </div>
     </div>`,
  );

  expect(await findClippedContent(page)).toEqual([]);
});

test("a scroller that is itself clipped away does not rescue anything", async ({
  page,
}) => {
  // A scroller only helps if the reader can reach the scroller. Pushed wholly
  // outside a clipping parent, it is as gone as its contents.
  await layout(
    page,
    `<div style="width: 300px; overflow: hidden">
       <div style="width: 300px; margin-left: 600px; overflow-x: auto">
         <div style="width: 900px">Work that nobody can scroll to</div>
       </div>
     </div>`,
  );

  const offenders = await findClippedContent(page);
  expect(offenders.join("\n")).toContain("Work that nobody can scroll to");
});

test("text hanging off the page with nothing clipping it is clipped", async ({
  page,
}) => {
  // Nothing takes responsibility: no clip, no scroller, no ellipsis. The text
  // is simply painted where the reader is not.
  await layout(
    page,
    `<div style="width: 900px; white-space: nowrap">
       A line wider than the phone with no container to bound it
     </div>`,
  );

  const offenders = await findClippedContent(page);
  expect(offenders.join("\n")).toContain("A line wider than the phone");
});

test("a line clamp counts as truncation, under the same rule", async ({
  page,
}) => {
  // `-webkit-line-clamp` is the other way the product says "there is more" —
  // the summary clamp in a report panel. It gets the same treatment, including
  // being overruled by an outer clip.
  await layout(
    page,
    `<div id="ok" style="width: 300px; overflow: hidden">
       <div style="width: 300px; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden">
         A summary long enough to need clamping to two lines in a narrow panel
       </div>
     </div>
     <div style="width: 300px; overflow: hidden">
       <div style="width: 800px; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden">
         A clamped summary pushed past the edge of its column
       </div>
     </div>`,
  );

  const offenders = await findClippedContent(page);
  expect(offenders.join("\n")).toContain("A clamped summary pushed past");
  expect(offenders.join("\n")).not.toContain("A summary long enough to need");
});
