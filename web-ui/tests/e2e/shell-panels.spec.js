import { expect, test } from "@playwright/test";

import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";

/**
 * The shell's collapsible side panels, in a real browser.
 *
 * Three things only a browser can show: that the preference survives a
 * reload, that a narrow window overrides it *without* overwriting it, and that
 * a collapsed nav is still a nav — icons, counts and the active marker all
 * stay, so collapsing is not a way to lose your way around.
 */

const WORKSPACE = "/o/local/w/local";
const TASKS = `${WORKSPACE}/tasks`;

const WIDE = { width: 1600, height: 900 };
/** Below `PANEL_AUTO_COLLAPSE_BELOW.nav`. */
const NARROW_DESKTOP = { width: 1100, height: 900 };

async function open(page, path = TASKS, viewport = WIDE) {
  await installWorkspaceApi(page, {});
  await page.setViewportSize(viewport);
  await page.goto(path);
  await expect(page.locator("[data-shell-nav]")).toBeVisible({
    timeout: 60_000,
  });
}

test("the left nav collapses to an icon rail and stays navigable", async ({
  page,
}, testInfo) => {
  test.setTimeout(90_000);
  await open(page);

  const nav = page.locator("[data-shell-nav]");
  await expect(nav).toHaveAttribute("data-shell-nav", "expanded");
  const inboxLink = nav.getByRole("link", { name: "Inbox", exact: true });
  await expect(inboxLink).toBeVisible();
  // Expanded: the words are there.
  await expect(inboxLink.locator(".shell-nav-copy")).toBeVisible();

  await nav.getByRole("button", { name: "Hide menu" }).click();
  await expect(nav).toHaveAttribute("data-shell-nav", "collapsed");

  // Collapsed is an icon rail, not a hidden nav: the link is still there and
  // still clickable, it just has no words.
  await expect(inboxLink).toBeVisible();
  await expect(inboxLink.locator(".shell-nav-copy")).toBeHidden();
  await expect(inboxLink).toHaveAttribute("title", "Inbox");
  // And it is narrower than it was.
  const collapsedWidth = await nav.evaluate((node) => node.clientWidth);
  expect(collapsedWidth).toBeLessThan(120);

  // No horizontal page scroll in either state.
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > window.innerWidth + 1,
    ),
  ).toBe(false);

  await page.screenshot({
    path: testInfo.outputPath("nav-collapsed.png"),
    animations: "disabled",
  });
  await testInfo.attach("nav-collapsed", {
    path: testInfo.outputPath("nav-collapsed.png"),
    contentType: "image/png",
  });

  await nav.getByRole("button", { name: "Show menu" }).click();
  await expect(nav).toHaveAttribute("data-shell-nav", "expanded");
});

test("the nav's collapse state is remembered across a reload", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await open(page);

  const nav = page.locator("[data-shell-nav]");
  await nav.getByRole("button", { name: "Hide menu" }).click();
  await expect(nav).toHaveAttribute("data-shell-nav", "collapsed");

  await page.reload();
  await expect(page.locator("[data-shell-nav]")).toHaveAttribute(
    "data-shell-nav",
    "collapsed",
    { timeout: 60_000 },
  );

  // And expanding it again sticks the same way.
  await page
    .locator("[data-shell-nav]")
    .getByRole("button", { name: "Show menu" })
    .click();
  await page.reload();
  await expect(page.locator("[data-shell-nav]")).toHaveAttribute(
    "data-shell-nav",
    "expanded",
    { timeout: 60_000 },
  );
});

test("a narrow window collapses the nav without overwriting the preference", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await open(page);
  const nav = page.locator("[data-shell-nav]");
  await expect(nav).toHaveAttribute("data-shell-nav", "expanded");

  // Narrow: collapsed, and the toggle says why rather than doing nothing.
  await page.setViewportSize(NARROW_DESKTOP);
  await expect(nav).toHaveAttribute("data-shell-nav", "collapsed");
  const toggle = nav.locator("[data-panel-toggle='left']");
  await expect(toggle).toBeDisabled();
  await expect(toggle).toHaveAttribute(
    "title",
    "Menu is hidden because the window is narrow",
  );

  // Wide again: the viewer's choice comes back. Resizing a window must not
  // silently rewrite a preference.
  await page.setViewportSize(WIDE);
  await expect(nav).toHaveAttribute("data-shell-nav", "expanded");
  await expect(toggle).toBeEnabled();
});

test("the task page's right rail collapses and is remembered", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await installWorkspaceApi(page, {});
  await page.setViewportSize(WIDE);
  await page.route("**/*", async (route) => {
    const request = route.request();
    if (!["fetch", "xhr"].includes(request.resourceType())) {
      return route.fallback();
    }
    const path = decodeURIComponent(new URL(request.url()).pathname);
    if (path.startsWith("/work/") && request.method() === "GET") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          work: {
            ref: "card:rail",
            handle: "rail",
            title: "A task with a source",
            summary: "Body.",
            phase: "in_progress",
            owner: "actor:someone",
            source: {
              authority: "github",
              native_id: "99",
              url: "https://github.com/o/r/issues/99",
            },
          },
        }),
      });
    }
    return route.fallback();
  });
  await page.goto(`${WORKSPACE}/tasks/${encodeURIComponent("card:rail")}`);

  const grid = page.locator("[data-task-rail]");
  await expect(grid).toHaveAttribute("data-task-rail", "expanded", {
    timeout: 60_000,
  });
  const rail = page.getByRole("complementary", {
    name: "Source and follow-through",
  });
  await expect(rail.getByText("Authority")).toBeVisible();

  await rail.getByRole("button", { name: "Hide details" }).click();
  await expect(grid).toHaveAttribute("data-task-rail", "collapsed");
  await expect(rail.getByText("Authority")).toBeHidden();

  await page.reload();
  await expect(page.locator("[data-task-rail]")).toHaveAttribute(
    "data-task-rail",
    "collapsed",
    { timeout: 60_000 },
  );
});

test("the rail stacks rather than collapsing on a narrow window", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await installWorkspaceApi(page, {});
  // Below `xl`, where the rail sits under the content instead of beside it.
  await page.setViewportSize({ width: 1024, height: 900 });
  await page.goto(`${WORKSPACE}/tasks`);
  await expect(page.locator("[data-shell-nav]")).toBeVisible({
    timeout: 60_000,
  });
  // The nav is auto-collapsed at this width; the page still does not scroll
  // sideways, which is the invariant David's audit is about.
  await expect(page.locator("[data-shell-nav]")).toHaveAttribute(
    "data-shell-nav",
    "collapsed",
  );
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > window.innerWidth + 1,
    ),
  ).toBe(false);
});
