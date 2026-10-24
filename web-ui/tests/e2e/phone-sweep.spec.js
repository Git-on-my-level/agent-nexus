import { expect as baseExpect, test } from "@playwright/test";

import {
  buildSceneUrl,
  installQaEnvironment,
  installQaRoutes,
} from "../../scripts/qa-visual.mjs";
import { QA_TOPICS } from "../fixtures/qa-seed.js";
import { expectCleanLayout, scrollPage } from "../helpers/layoutAudit.js";

// The dev server compiles routes on demand; first paint of a route can take
// several seconds under parallel runs.
const expect = baseExpect.configure({ timeout: 20_000 });
test.describe.configure({ timeout: 120_000 });

/**
 * Every workspace page except Watch, held in a phone's hand: 390px wide, with
 * a touch screen, so `(pointer: coarse)` rules apply as they do on a phone.
 *
 * Two things must hold on each page, at the top and scrolled to the end:
 * - the page never scrolls sideways (`scrollWidth <= clientWidth` on the
 *   document and on `<main>`, which is what the app shell scrolls);
 * - every button, field, select and disclosure is at least 40px tall, the
 *   floor `app.css` already sets for the shell's own controls on touch;
 * - no text runs under a row's always-shown touch action bar.
 *
 * Inline text links are left out on purpose: they sit in running text and
 * are as tall as the line, which is what WCAG 2.5.8 exempts.
 */

const W = "/o/local/w/local";
const MIN_TAP = 40;

const LAUNCH = QA_TOPICS[0];

/** Thread detail reads `/threads/{id}/workspace`, which the QA mock lacks. */
async function serveThreadWorkspace(page) {
  await page.route(`**/threads/${LAUNCH.thread_id}/workspace*`, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        thread_id: LAUNCH.thread_id,
        thread: { ...LAUNCH, id: LAUNCH.thread_id },
        topic: LAUNCH,
        context: {
          recent_events: [],
          key_artifacts: [],
          open_cards: [],
          documents: [],
        },
      }),
    }),
  );
}

/** Signed out, so the sign-in and invite forms render instead of redirecting. */
async function signOut(page) {
  await page.route("**/auth/session", (route) =>
    route.fulfill({
      status: 401,
      contentType: "application/json",
      body: JSON.stringify({ error: { code: "unauthenticated" } }),
    }),
  );
}

const ROUTES = [
  {
    name: "decide",
    path: `${W}/decide`,
    ready: "Review fleet onboarding runbook",
  },
  {
    name: "decide permalink",
    path: `${W}/decide/ask-91`,
    ready: "Merge battery-v2 calibration?",
  },
  {
    name: "initiatives",
    path: `${W}/initiatives`,
    ready: "Nothing tracked yet",
  },
  { name: "new initiative", path: `${W}/initiatives/new` },
  { name: "docs", path: `${W}/docs`, ready: "Launch checklist" },
  { name: "doc", path: `${W}/docs/doc-launch-checklist` },
  { name: "doc edit", path: `${W}/docs/doc-launch-checklist/edit` },
  { name: "threads", path: `${W}/threads`, ready: "Launch war room" },
  {
    name: "thread",
    path: `${W}/threads/${LAUNCH.thread_id}`,
    setup: serveThreadWorkspace,
  },
  { name: "agents", path: `${W}/agents` },
  { name: "access and hosts", path: `${W}/access` },
  { name: "activity", path: `${W}/events` },
  { name: "settings", path: `${W}/more` },
  { name: "integrations", path: `${W}/integrations` },
  { name: "secrets", path: `${W}/secrets` },
  { name: "archive", path: `${W}/archive` },
  { name: "pm setup", path: `${W}/pm/setup` },
  { name: "sign in", path: `${W}/login`, setup: signOut },
  {
    name: "invite",
    path: `${W}/login?token=oinv_qa_docs_open`,
    setup: signOut,
  },
];

/* Runs in the browser. */
function measure(minTap) {
  const visible = (el) => {
    const rect = el.getBoundingClientRect();
    if (rect.width < 2 || rect.height < 2) return false;
    const style = getComputedStyle(el);
    return style.visibility !== "hidden" && style.display !== "none";
  };
  const describe = (el) => {
    const rect = el.getBoundingClientRect();
    const text = (
      el.getAttribute("aria-label") ||
      el.innerText ||
      el.getAttribute("placeholder") ||
      el.getAttribute("name") ||
      ""
    )
      .replace(/\s+/g, " ")
      .trim()
      .slice(0, 40);
    return `<${el.tagName.toLowerCase()}> "${text}" ${Math.round(rect.width)}x${Math.round(rect.height)}`;
  };

  const overflow = [document.documentElement, document.querySelector("main")]
    .filter(Boolean)
    .filter((el) => el.scrollWidth > el.clientWidth)
    .map(
      (el) =>
        `<${el.tagName.toLowerCase()}> scrollWidth ${el.scrollWidth} > clientWidth ${el.clientWidth}`,
    );

  const controls = document.querySelectorAll(
    [
      "button",
      "select",
      "textarea",
      "summary",
      "[role='button']:not(a)",
      "input:not([type='hidden']):not([type='checkbox']):not([type='radio'])",
      // A checkbox is tapped through its label.
      "label:has(> input[type='checkbox'], > input[type='radio'])",
    ].join(", "),
  );
  const small = [...controls]
    .filter((el) => !el.closest(".sr-only, [aria-hidden='true'], [inert]"))
    .filter(visible)
    .filter((el) => el.getBoundingClientRect().height < minTap - 0.5)
    .map(describe);

  // On touch the row action bar is always shown; its row's own text must
  // flow around it, not run underneath it.
  const covered = [];
  for (const bar of document.querySelectorAll(".row-action-bar")) {
    if (!visible(bar)) continue;
    const box = bar.getBoundingClientRect();
    const walker = document.createTreeWalker(
      bar.parentElement,
      NodeFilter.SHOW_TEXT,
    );
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      if (!node.textContent.trim() || bar.contains(node)) continue;
      const range = document.createRange();
      range.selectNodeContents(node);
      // Text an ellipsis has cut off is not painted; only its box counts.
      const clip =
        getComputedStyle(node.parentElement).overflowX === "visible"
          ? null
          : node.parentElement.getBoundingClientRect();
      const hit = [...range.getClientRects()].some(
        (r) =>
          r.width > 0 &&
          r.left < box.right - 1 &&
          Math.min(r.right, clip?.right ?? r.right) > box.left + 1 &&
          r.top < box.bottom - 1 &&
          r.bottom > box.top + 1,
      );
      if (hit) covered.push(`"${node.textContent.trim().slice(0, 40)}"`);
    }
  }

  return { overflow, small: [...new Set(small)], covered };
}

test.describe("phone sweep", () => {
  test.use({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    colorScheme: "dark",
    reducedMotion: "reduce",
  });

  for (const route of ROUTES) {
    test(route.name, async ({ page, baseURL }) => {
      const scene = { name: route.name, workspaceMode: "workspace-default" };
      await installQaEnvironment(page, scene);
      await installQaRoutes(page, scene);
      await route.setup?.(page);
      await page.goto(buildSceneUrl(baseURL, route.path), {
        waitUntil: "domcontentloaded",
      });
      // Bootstrap placeholders carry the page's own heading, so wait for the
      // workspace to load before looking for the route's content. A route
      // compiled cold on a busy machine can take most of a minute.
      await expect(page.getByText("Loading workspace")).toHaveCount(0, {
        timeout: 60_000,
      });
      await expect(
        route.ready
          ? page.getByText(route.ready).first()
          : page.locator("main h1").first(),
      ).toBeVisible();
      await page.evaluate(() => document.fonts?.ready);

      for (const position of ["top", "bottom"]) {
        await scrollPage(page, position);
        const { overflow, small, covered } = await page.evaluate(
          measure,
          MIN_TAP,
        );
        expect
          .soft(overflow, `${route.name} @${position}: sideways scroll`)
          .toEqual([]);
        expect
          .soft(small, `${route.name} @${position}: under ${MIN_TAP}px tall`)
          .toEqual([]);
        expect
          .soft(covered, `${route.name} @${position}: text under row actions`)
          .toEqual([]);
      }
      // Taller touch controls must not overlap or clip what is around them.
      await scrollPage(page, "top");
      await expectCleanLayout(page, route.name, {
        scrollPositions: ["current", "bottom"],
      });
    });
  }
});
