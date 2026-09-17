/**
 * Geometry-based rendering audit for Playwright pages.
 *
 * Screenshot diffs only catch regressions against a known-good baseline. This
 * audit instead asserts layout invariants that hold for every state of every
 * page, so it can be run after each UI transition (open a popover, submit a
 * form, fail a request...) without maintaining images:
 *
 * - `text-overlap`: two pieces of text are painted on top of each other with
 *   no opaque layer between them (e.g. a floating banner with a translucent
 *   fill showing the form underneath).
 * - `occluded-control`: a floating, non-modal layer covers an interactive
 *   control so it cannot be clicked.
 * - `page-overflow-x`: the document scrolls horizontally.
 * - `text-spill`: text escapes the box (border/background) that contains it.
 * - `fixed-offscreen`: a fixed layer is partially outside the viewport, where
 *   it cannot be scrolled into view.
 * - `clipped-text`: text cut off by an `overflow: hidden` ancestor with no
 *   ellipsis, line clamp or scroll to signal there is more.
 * - `layer-offscreen`: an absolutely positioned popover/menu hangs off the
 *   side of the screen.
 * - `unreachable-control`: a control is stuck under fixed chrome (mobile tab
 *   bar, header) with no scroll range left to bring it clear.
 *
 * Usage:
 *   await expectCleanLayout(page, "invite created");
 */

import { mkdirSync } from "node:fs";
import path from "node:path";

import { expect, test } from "@playwright/test";

/** Set LAYOUT_AUDIT_SCREENSHOTS=<dir> to save a PNG of every audited state. */
const screenshotDir = process.env.LAYOUT_AUDIT_SCREENSHOTS || "";

async function captureState(page, stateLabel) {
  if (!screenshotDir) return;
  const titlePath = test.info().titlePath.slice(1).join(" - ");
  const slug = (value) =>
    value
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-|-$/g, "");
  const dir = path.resolve(screenshotDir, slug(titlePath));
  mkdirSync(dir, { recursive: true });
  test
    .info()
    .annotations.push({ type: "layout-audit-state", description: stateLabel });
  const index = String(
    test.info().annotations.filter((a) => a.type === "layout-audit-state")
      .length,
  ).padStart(2, "0");
  await page.screenshot({
    path: path.join(dir, `${index}-${slug(stateLabel)}.png`),
  });
}

export const AUDIT_VIEWPORTS = [
  { name: "mobile", width: 375, height: 812 },
  { name: "tablet", width: 768, height: 1024 },
  // Between md and lg: narrow desktop window with the sidebar visible.
  { name: "narrow-desktop", width: 1000, height: 840 },
  { name: "desktop", width: 1440, height: 900 },
];

/* Runs inside the browser. Must stay self-contained (no closures). */
function collectLayoutViolations(options) {
  const ignoreSelectors = options?.ignore ?? [];
  const onlyChecks = options?.checks ?? null;
  const maxPerCheck = options?.maxPerCheck ?? 12;
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  const OPAQUE = 0.85;
  const violations = [];
  const counts = {};

  const enabled = (check) => !onlyChecks || onlyChecks.includes(check);
  const report = (check, message, elements) => {
    counts[check] = (counts[check] ?? 0) + 1;
    if (counts[check] > maxPerCheck) return;
    violations.push({ check, message, elements });
  };

  const isIgnored = (el) =>
    Boolean(
      el?.closest?.("[data-layout-audit-ignore]") ||
      ignoreSelectors.some((selector) => el?.closest?.(selector)),
    );

  const describe = (el) => {
    if (!el || el.nodeType !== 1) return String(el);
    const id = el.id ? `#${el.id}` : "";
    const testId = el.getAttribute("data-testid");
    const cls =
      typeof el.className === "string" && el.className.trim()
        ? `.${el.className.trim().split(/\s+/).slice(0, 4).join(".")}`
        : "";
    const text = (el.innerText ?? el.value ?? el.textContent ?? "")
      .replace(/\s+/g, " ")
      .trim()
      .slice(0, 60);
    return `<${el.tagName.toLowerCase()}${id}${testId ? `[data-testid=${testId}]` : ""}${cls}>${text ? ` "${text}"` : ""}`;
  };

  const styleCache = new WeakMap();
  const styleOf = (el) => {
    let style = styleCache.get(el);
    if (!style) {
      style = getComputedStyle(el);
      styleCache.set(el, style);
    }
    return style;
  };

  const colorAlpha = (color) => {
    if (!color || color === "transparent") return 0;
    const slash = color.match(/\/\s*([\d.]+%?)\s*\)$/);
    if (slash) {
      return slash[1].endsWith("%")
        ? parseFloat(slash[1]) / 100
        : parseFloat(slash[1]);
    }
    const rgba = color.match(/^rgba\(([^)]+)\)$/);
    if (rgba) {
      const parts = rgba[1].split(",");
      return parts.length === 4 ? parseFloat(parts[3]) : 1;
    }
    return 1;
  };

  const opacityCache = new WeakMap();
  const groupOpacity = (el) => {
    if (!el || el.nodeType !== 1) return 1;
    if (opacityCache.has(el)) return opacityCache.get(el);
    const value =
      parseFloat(styleOf(el).opacity || "1") * groupOpacity(el.parentElement);
    opacityCache.set(el, value);
    return value;
  };

  /** How much of whatever is behind `el` its own box hides (0..1). */
  const layerCoverage = (el) => {
    const style = styleOf(el);
    let alpha = colorAlpha(style.backgroundColor);
    if (/url\(/.test(style.backgroundImage)) alpha = 1;
    if (/^(IMG|VIDEO|CANVAS|IFRAME)$/.test(el.tagName)) alpha = 1;
    return alpha * groupOpacity(el);
  };

  /** Is `owner`'s own content visibly painted at the point? */
  const paintedAt = (owner, x, y) => {
    if (x < 0 || y < 0 || x >= vw || y >= vh) return false;
    const stack = document.elementsFromPoint(x, y);
    let index = stack.indexOf(owner);
    if (index === -1) {
      // Not hit-testable: clipped away, or pointer-events: none. For the
      // latter, its nearest hit-testable ancestor marks its paint layer.
      if (styleOf(owner).pointerEvents !== "none") return false;
      // Hit-testing cannot tell us about clipping here, so check it by hand.
      for (let el = owner; el && el !== document.body; el = el.parentElement) {
        const style = styleOf(el);
        if (style.overflowX === "visible" && style.overflowY === "visible")
          continue;
        const clip = el.getBoundingClientRect();
        if (x < clip.left || x > clip.right || y < clip.top || y > clip.bottom)
          return false;
      }
      let ancestor = owner.parentElement;
      while (ancestor && !stack.includes(ancestor)) {
        ancestor = ancestor.parentElement;
      }
      if (!ancestor) return false;
      index = stack.indexOf(ancestor);
    }
    let seeThrough = 1;
    for (let i = 0; i < index; i += 1) {
      if (owner.contains(stack[i])) continue;
      seeThrough *= 1 - layerCoverage(stack[i]);
      if (1 - seeThrough >= OPAQUE) return false;
    }
    return true;
  };

  const inViewport = (rect) =>
    rect.right > 0 && rect.bottom > 0 && rect.left < vw && rect.top < vh;

  const modal = Array.from(
    document.querySelectorAll('[aria-modal="true"], dialog[open]'),
  ).find((el) => el.getClientRects().length > 0);

  // ---- Collect painted text -------------------------------------------------
  const textRects = [];
  if (enabled("text-overlap") || enabled("text-spill")) {
    const walker = document.createTreeWalker(
      document.body,
      NodeFilter.SHOW_TEXT,
    );
    const range = document.createRange();
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      if (!node.nodeValue || !node.nodeValue.trim()) continue;
      const owner = node.parentElement;
      if (!owner) continue;
      if (/^(SCRIPT|STYLE|NOSCRIPT|OPTION|TITLE|TEMPLATE)$/.test(owner.tagName))
        continue;
      const style = styleOf(owner);
      if (style.visibility !== "visible") continue;
      if (groupOpacity(owner) < 0.1) continue;
      if (colorAlpha(style.color) < 0.1) continue;
      if (isIgnored(owner)) continue;
      range.selectNodeContents(node);
      for (const rect of range.getClientRects()) {
        if (rect.width < 3 || rect.height < 5) continue;
        if (!inViewport(rect)) continue;
        textRects.push({ owner, rect, kind: "text" });
      }
    }
    for (const control of document.querySelectorAll(
      "input:not([type=hidden]):not([type=checkbox]):not([type=radio]):not([type=file]), select, textarea",
    )) {
      const rect = control.getBoundingClientRect();
      if (rect.width < 8 || rect.height < 8 || !inViewport(rect)) continue;
      const style = styleOf(control);
      if (style.visibility !== "visible" || groupOpacity(control) < 0.1)
        continue;
      if (isIgnored(control)) continue;
      textRects.push({ owner: control, rect, kind: "control" });
    }
  }

  // ---- text-overlap ---------------------------------------------------------
  if (enabled("text-overlap")) {
    const seenPairs = new Set();
    const ids = new WeakMap();
    let nextId = 1;
    const idOf = (el) => {
      if (!ids.has(el)) ids.set(el, nextId++);
      return ids.get(el);
    };
    const sorted = textRects.slice().sort((a, b) => a.rect.top - b.rect.top);
    for (let i = 0; i < sorted.length; i += 1) {
      const a = sorted[i];
      for (let j = i + 1; j < sorted.length; j += 1) {
        const b = sorted[j];
        if (b.rect.top >= a.rect.bottom) break;
        if (a.owner === b.owner) continue;
        // A control's own label/placeholder text lives inside it.
        if (a.kind === "control" && a.owner.contains(b.owner)) continue;
        if (b.kind === "control" && b.owner.contains(a.owner)) continue;
        if (a.kind === "control" && b.kind === "control") {
          // Nested/adjacent controls are covered by occluded-control.
          continue;
        }
        const left = Math.max(a.rect.left, b.rect.left);
        const right = Math.min(a.rect.right, b.rect.right);
        const top = Math.max(a.rect.top, b.rect.top);
        const bottom = Math.min(a.rect.bottom, b.rect.bottom);
        const overlapW = right - left;
        const overlapH = bottom - top;
        if (overlapW < 4) continue;
        const minH = Math.min(a.rect.height, b.rect.height);
        if (overlapH < Math.max(3, minH * 0.4)) continue;
        // Text inside a control (select label, button in input group) is fine
        // when the text's owner is a wrapper of that control.
        if (a.owner.contains(b.owner) && b.kind === "control") continue;
        if (b.owner.contains(a.owner) && a.kind === "control") continue;
        const key = `${idOf(a.owner)}:${idOf(b.owner)}`;
        if (seenPairs.has(key)) continue;
        const x = (left + right) / 2;
        const y = (top + bottom) / 2;
        if (!paintedAt(a.owner, x, y) || !paintedAt(b.owner, x, y)) continue;
        seenPairs.add(key);
        report(
          "text-overlap",
          `Text is painted over other content near (${Math.round(x)}, ${Math.round(y)}) with no opaque layer between.`,
          [describe(a.owner), describe(b.owner)],
        );
      }
    }
  }

  // ---- text-spill -----------------------------------------------------------
  if (enabled("text-spill")) {
    const seen = new Set();
    for (const { owner, rect, kind } of textRects) {
      if (kind !== "text" || seen.has(owner)) continue;
      let box = owner;
      while (box && box !== document.body) {
        const style = styleOf(box);
        const hasBox =
          style.display !== "inline" &&
          (colorAlpha(style.backgroundColor) > 0 ||
            parseFloat(style.borderLeftWidth) +
              parseFloat(style.borderRightWidth) >
              0 ||
            style.overflowX !== "visible");
        if (hasBox) break;
        box = box.parentElement;
      }
      if (!box || box === document.body) continue;
      if (styleOf(box).overflowX !== "visible") continue;
      const boxRect = box.getBoundingClientRect();
      const spillRight = rect.right - boxRect.right;
      const spillLeft = boxRect.left - rect.left;
      const spill = Math.max(spillRight, spillLeft);
      if (spill <= 2) continue;
      // Deliberately out-of-box decorations (badges, absolutely placed).
      const position = styleOf(owner).position;
      if (position === "absolute" || position === "fixed") continue;
      // Clamp into the viewport: text that spills past the screen edge is
      // still a spill.
      const probeX = Math.min(
        vw - 1,
        Math.max(0, spillRight > spillLeft ? rect.right - 1 : rect.left + 1),
      );
      if (!paintedAt(owner, probeX, rect.top + rect.height / 2)) continue;
      seen.add(owner);
      report(
        "text-spill",
        `Text extends ${Math.round(spill)}px outside its containing box.`,
        [describe(owner), describe(box)],
      );
    }
  }

  // ---- occluded-control -----------------------------------------------------
  if (enabled("occluded-control")) {
    const overlayRoles =
      '[role="dialog"], [role="menu"], [role="listbox"], [role="tooltip"], [role="alertdialog"], dialog, [popover]';
    for (const control of document.querySelectorAll(
      'a[href], button, input:not([type=hidden]), select, textarea, summary, [role="button"], [role="tab"], [role="menuitem"]',
    )) {
      if (modal && !modal.contains(control)) continue;
      if (control.disabled) continue;
      const rect = control.getBoundingClientRect();
      if (rect.width < 6 || rect.height < 6) continue;
      const x = rect.left + rect.width / 2;
      const y = rect.top + rect.height / 2;
      if (x < 0 || y < 0 || x >= vw || y >= vh) continue;
      const style = styleOf(control);
      if (style.visibility !== "visible" || style.pointerEvents === "none")
        continue;
      if (groupOpacity(control) < 0.1) continue;
      if (isIgnored(control)) continue;
      const top = document.elementFromPoint(x, y);
      if (!top || top === control) continue;
      if (control.contains(top) || top.contains(control)) continue;
      if (top.closest("label")?.control === control) continue;
      // Clipped by a scroll container: not reachable at this point, fine.
      if (!document.elementsFromPoint(x, y).includes(control)) continue;
      if (top.closest(overlayRoles)) continue;
      // Fixed/sticky chrome (headers, tab bars, side rails) legitimately
      // covers content that scrolls beneath it.
      let chrome = false;
      let chromeRect = null;
      for (let el = top; el && el !== document.body; el = el.parentElement) {
        const position = styleOf(el).position;
        if (position === "sticky") {
          chrome = true;
          break;
        }
        if (position === "fixed") {
          const r = el.getBoundingClientRect();
          chrome =
            r.top <= 1 ||
            r.left <= 1 ||
            r.bottom >= vh - 1 ||
            r.right >= vw - 1;
          if (chrome) chromeRect = r;
          break;
        }
      }
      if (chrome) {
        // Chrome may cover content that can still scroll out from under it.
        // At the end of the scroll range there is nowhere left to go.
        if (!enabled("unreachable-control") || !chromeRect) continue;
        if (isIgnored(top)) continue;
        let scroller = control.parentElement;
        while (scroller && scroller !== document.documentElement) {
          if (
            /(auto|scroll)/.test(styleOf(scroller).overflowY) &&
            scroller.scrollHeight > scroller.clientHeight + 1
          )
            break;
          scroller = scroller.parentElement;
        }
        if (!scroller || scroller === document.documentElement) {
          scroller = document.scrollingElement;
        }
        let pinned = false;
        for (let el = control; el; el = el.parentElement) {
          if (styleOf(el).position === "fixed") pinned = true;
        }
        const atEnd =
          scroller.scrollTop + scroller.clientHeight >=
          scroller.scrollHeight - 1;
        const atStart = scroller.scrollTop <= 0;
        const fromBelow = chromeRect.bottom >= vh - 1 && chromeRect.top > 1;
        const fromAbove = chromeRect.top <= 1 && chromeRect.bottom < vh - 1;
        if (pinned || (fromBelow && atEnd) || (fromAbove && atStart)) {
          report(
            "unreachable-control",
            "Control sits under fixed chrome and cannot be scrolled clear of it.",
            [describe(control), describe(top)],
          );
        }
        continue;
      }
      if (isIgnored(top)) continue;
      report(
        "occluded-control",
        `Control is covered at its center (${Math.round(x)}, ${Math.round(y)}) by a non-modal layer.`,
        [describe(control), describe(top)],
      );
    }
  }

  // ---- page-overflow-x ------------------------------------------------------
  if (enabled("page-overflow-x")) {
    const root = document.documentElement;
    if (root.scrollWidth > root.clientWidth + 1) {
      const culprits = [];
      for (const el of document.body.querySelectorAll("*")) {
        const rect = el.getBoundingClientRect();
        if (rect.width === 0 || rect.right <= root.clientWidth + 1) continue;
        if (styleOf(el).position === "fixed") continue;
        let clipped = false;
        for (let p = el.parentElement; p && p !== root; p = p.parentElement) {
          if (styleOf(p).overflowX !== "visible") {
            clipped = true;
            break;
          }
        }
        if (clipped) continue;
        const hasCulpritChild = Array.from(el.children).some(
          (child) => child.getBoundingClientRect().right > root.clientWidth + 1,
        );
        if (!hasCulpritChild) culprits.push(describe(el));
        if (culprits.length >= 4) break;
      }
      report(
        "page-overflow-x",
        `Document is ${root.scrollWidth - root.clientWidth}px wider than the viewport.`,
        culprits,
      );
    }
  }

  // ---- fixed-offscreen ------------------------------------------------------
  if (enabled("fixed-offscreen")) {
    for (const el of document.body.querySelectorAll("*")) {
      const style = styleOf(el);
      if (style.position !== "fixed" || style.visibility !== "visible")
        continue;
      if (groupOpacity(el) < 0.1) continue;
      const rect = el.getBoundingClientRect();
      if (rect.width < 2 || rect.height < 2 || !inViewport(rect)) continue;
      if (isIgnored(el)) continue;
      const scrollable =
        /(auto|scroll)/.test(style.overflowY) ||
        /(auto|scroll)/.test(style.overflowX);
      if (scrollable) continue;
      const over = Math.max(
        -rect.left,
        -rect.top,
        rect.right - vw,
        rect.bottom - vh,
      );
      if (over <= 1) continue;
      report(
        "fixed-offscreen",
        `Fixed layer extends ${Math.round(over)}px outside the viewport and cannot be scrolled into view.`,
        [describe(el)],
      );
    }
  }

  // ---- clipped-text ---------------------------------------------------------
  // Text cut off by an `overflow: hidden` ancestor (the app shell clips its
  // main column, so this never shows up as page overflow). Scrollable boxes
  // and ellipsis / line-clamp truncation are fine: the reader can tell.
  if (enabled("clipped-text")) {
    const seen = new Set();
    const signposted = (el) => {
      const style = styleOf(el);
      const clamp = style.getPropertyValue("-webkit-line-clamp");
      return style.textOverflow === "ellipsis" || (clamp && clamp !== "none");
    };
    for (const { owner, rect, kind } of textRects) {
      if (kind !== "text" || seen.has(owner)) continue;
      let excused = false;
      let clipper = null;
      for (let el = owner; el && el !== document.body; el = el.parentElement) {
        if (signposted(el)) {
          excused = true;
          break;
        }
        const style = styleOf(el);
        if (style.overflowX === "visible") continue;
        if (/(auto|scroll)/.test(style.overflowX)) excused = true;
        clipper = el;
        break;
      }
      if (excused || !clipper) continue;
      const clip = clipper.getBoundingClientRect();
      if (clip.width < 8) continue; // visually-hidden helpers
      const cut = Math.max(rect.right - clip.right, clip.left - rect.left);
      if (cut <= 2) continue;
      // Entirely outside the clip: deliberately hidden (drawers, carousels).
      if (rect.left >= clip.right || rect.right <= clip.left) continue;
      const probeX = Math.min(
        vw - 1,
        Math.max(0, Math.max(rect.left, clip.left) + 2),
      );
      if (!paintedAt(owner, probeX, rect.top + rect.height / 2)) continue;
      seen.add(owner);
      report(
        "clipped-text",
        `Text is cut off ${Math.round(cut)}px by an overflow-hidden ancestor with no ellipsis or scroll.`,
        [describe(owner), describe(clipper)],
      );
    }
  }

  // ---- layer-offscreen -------------------------------------------------------
  // Absolutely positioned popovers/menus hanging off the side of the screen.
  if (enabled("layer-offscreen")) {
    const root = document.documentElement;
    const pageScrollsX = root.scrollWidth > root.clientWidth + 1;
    for (const el of document.body.querySelectorAll("*")) {
      const style = styleOf(el);
      if (style.position !== "absolute" || style.visibility !== "visible")
        continue;
      if (groupOpacity(el) < 0.1 || isIgnored(el)) continue;
      const rect = el.getBoundingClientRect();
      if (rect.width < 40 || rect.height < 16 || !inViewport(rect)) continue;
      const over = Math.max(-rect.left, pageScrollsX ? 0 : rect.right - vw);
      if (over <= 2) continue;
      if (!(el.innerText ?? "").trim()) continue; // decorative
      // Only layers that are actually painted where they are still on screen.
      const x = Math.min(vw - 2, Math.max(2, rect.left + rect.width / 2));
      const y = Math.min(
        vh - 2,
        Math.max(2, rect.top + Math.min(rect.height / 2, 12)),
      );
      if (!document.elementsFromPoint(x, y).some((hit) => el.contains(hit)))
        continue;
      report(
        "layer-offscreen",
        `Positioned layer extends ${Math.round(over)}px past the side of the viewport.`,
        [describe(el)],
      );
    }
  }

  return { violations, counts };
}

/**
 * @param {import("@playwright/test").Page} page
 * @param {{ ignore?: string[], checks?: string[], maxPerCheck?: number, settleMs?: number }} [options]
 */
export async function auditLayout(page, options = {}) {
  const run = async () => {
    // Let transitions/animations finish so we audit the resting state.
    await page.evaluate(
      (settleMs) =>
        new Promise((resolve) => {
          const done = () => requestAnimationFrame(() => resolve());
          const animations = document
            .getAnimations()
            .filter(
              (a) => a.effect?.getComputedTiming().iterations !== Infinity,
            );
          Promise.race([
            Promise.allSettled(animations.map((a) => a.finished)),
            new Promise((r) => setTimeout(r, settleMs)),
          ]).then(done);
        }),
      options.settleMs ?? 600,
    );
    return page.evaluate(collectLayoutViolations, {
      ignore: options.ignore ?? [],
      checks: options.checks ?? null,
      maxPerCheck: options.maxPerCheck ?? 12,
    });
  };
  // A client-side navigation (e.g. replaceState cleanup of query params) can
  // tear down the execution context mid-audit; audit the page it lands on.
  for (let attempt = 0; ; attempt += 1) {
    try {
      return await run();
    } catch (error) {
      const navigated = /Execution context was destroyed|navigation/i.test(
        String(error?.message ?? error),
      );
      if (!navigated || attempt >= 2) throw error;
      await page.waitForLoadState("domcontentloaded");
    }
  }
}

/**
 * Scrolls the window and every sizeable scroll container (the app shell
 * scrolls `<main>`, not the document) to the start or end of its range.
 */
export async function scrollPage(page, position) {
  await page.evaluate((to) => {
    const targets = [document.scrollingElement];
    for (const el of document.querySelectorAll("*")) {
      if (el.scrollHeight <= el.clientHeight + 1) continue;
      if (!/(auto|scroll)/.test(getComputedStyle(el).overflowY)) continue;
      const rect = el.getBoundingClientRect();
      if (rect.width * rect.height < innerWidth * innerHeight * 0.25) continue;
      targets.push(el);
    }
    for (const el of targets) {
      el.scrollTo({
        top: to === "bottom" ? el.scrollHeight : 0,
        behavior: "instant",
      });
    }
  }, position);
}

export function formatViolations(label, violations) {
  return violations
    .map(
      (v, i) =>
        `  ${i + 1}. [${v.check}] ${v.message}\n${v.elements
          .map((e) => `       ${e}`)
          .join("\n")}`,
    )
    .join("\n")
    .replace(/^/, `Layout audit failed for state "${label}":\n`);
}

/**
 * Audits at the current scroll position and, optionally, scrolled to the
 * bottom of the page (floating layers interact differently with content
 * there). Soft-asserts so one run reports every broken state.
 */
export async function expectCleanLayout(page, label, options = {}) {
  const positions = options.scrollPositions ?? ["current"];
  for (const position of positions) {
    if (position === "bottom" || position === "top") {
      await scrollPage(page, position);
    }
    const { violations } = await auditLayout(page, options);
    const stateLabel = position === "current" ? label : `${label} @${position}`;
    await captureState(page, stateLabel);
    expect
      .soft(violations.length, formatViolations(stateLabel, violations))
      .toBe(0);
  }
}
