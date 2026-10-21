import { expect, test } from "@playwright/test";
import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";

/**
 * `/r/<ref>`: the one link agents hand a human for any typed ref. It opens the
 * page that shows the ref, says plainly when there is nothing to open, sends a
 * signed-out reader through sign-in and back, and never leaves the workspace.
 */
const WORKSPACE = "/o/local/w/local";
const CARD = {
  id: "card-launch-plan",
  ref: "card:launch-plan",
  handle: "launch-plan",
  title: "Launch plan initiative",
  phase: "in_progress",
  source: { authority: "nexus" },
};

async function installRefApi(page, overrides = {}) {
  const api = await installWorkspaceApi(page, overrides);
  const reads = [];
  await page.route("**/work/**", (route) => {
    const path = decodeURIComponent(new URL(route.request().url()).pathname);
    reads.push(path);
    if (path.endsWith("/plan"))
      return route.fulfill({ json: { plan: { steps: [] } } });
    if (path.endsWith("/observations"))
      return route.fulfill({ json: { observations: [] } });
    if (path.endsWith("/participants"))
      return route.fulfill({ json: { participants: [] } });
    if (path === "/work/card:launch-plan")
      return route.fulfill({ json: { work: CARD } });
    return route.fulfill({
      status: 404,
      json: { error: { code: "not_found", message: "card not found" } },
    });
  });
  await page.route(
    (url) => url.pathname.startsWith("/docs/handbook"),
    (route) => {
      if (route.request().isNavigationRequest()) return route.continue();
      const path = new URL(route.request().url()).pathname;
      if (path !== "/docs/handbook")
        return route.fulfill({ json: { revisions: [] } });
      return route.fulfill({
        json: {
          document: {
            id: "doc-handbook",
            handle: "handbook",
            title: "Team handbook",
            status: "active",
            head_revision_id: "rev-1",
            head_revision_number: 1,
            updated_at: "2026-10-01T10:00:00Z",
          },
          revision: {
            revision_id: "rev-1",
            revision_number: 1,
            content_type: "text",
            content: "Hello.",
            created_at: "2026-10-01T10:00:00Z",
          },
        },
      });
    },
  );
  return { api, reads };
}

test("a card slug opens its initiative page", async ({ page }) => {
  await installRefApi(page);
  await page.goto(`${WORKSPACE}/r/card:launch-plan`);
  await expect(page).toHaveURL(`${WORKSPACE}/initiatives/card%3Alaunch-plan`);
  await expect(
    page.getByRole("heading", { name: CARD.title, exact: true }),
  ).toBeVisible();
});

test("a doc ref opens the doc, percent-encoded colon included", async ({
  page,
}) => {
  await installRefApi(page);
  await page.goto(`${WORKSPACE}/r/doc%3Ahandbook`);
  await expect(page).toHaveURL(`${WORKSPACE}/docs/handbook`);
  await expect(
    page.getByRole("heading", { name: "Team handbook" }).first(),
  ).toBeVisible();
});

test("an unknown or missing ref shows the friendly page", async ({ page }) => {
  await installRefApi(page);
  for (const ref of ["widget:launch-plan", "card:missing-card"]) {
    await page.goto(`${WORKSPACE}/r/${ref}`);
    const notFound = page.locator('[data-ref-route="not-found"]');
    await expect(notFound).toBeVisible();
    await expect(
      notFound.getByRole("heading", { name: "We couldn’t find this" }),
    ).toBeVisible();
    await expect(notFound.locator("[data-ref-route-ref]")).toHaveText(ref);
    await expect(
      notFound.getByRole("link", { name: "Go to workspace home" }),
    ).toHaveAttribute("href", WORKSPACE);
    await expect(page).toHaveURL(`${WORKSPACE}/r/${ref}`);
  }
});

test("a signed-out reader signs in and comes back to the ref", async ({
  page,
}) => {
  const { api, reads } = await installRefApi(page, { authenticated: false });
  await page.goto(`${WORKSPACE}/r/card%3Alaunch-plan`);
  await expect(page).toHaveURL(/\/login\?/);
  const login = new URL(page.url());
  expect(login.pathname).toBe(`${WORKSPACE}/login`);
  expect(login.searchParams.get("return_to")).toBe("/r/card%3Alaunch-plan");
  // Nothing was read about the ref before there was a session.
  expect(reads).toEqual([]);

  api.authenticated = true;
  await page.reload();
  await expect(page).toHaveURL(`${WORKSPACE}/initiatives/card%3Alaunch-plan`);
  await expect(
    page.getByRole("heading", { name: CARD.title, exact: true }),
  ).toBeVisible();
});

test("redirect and traversal attempts stay in the workspace", async ({
  page,
}) => {
  await installRefApi(page);
  await page.goto(`${WORKSPACE}/r/url:https%3A%2F%2Fevil.test%2Fphish`);
  await expect(page.locator('[data-ref-route="no-page"]')).toBeVisible();
  await expect(
    page.locator('[data-ref-route="no-page"]').getByRole("link"),
  ).toHaveAttribute("href", WORKSPACE);
  expect(new URL(page.url()).pathname).toBe(
    `${WORKSPACE}/r/url:https%3A%2F%2Fevil.test%2Fphish`,
  );

  for (const ref of ["card:..%2F..%2F..%2Fevil", "doc:%2F%2Fevil.test"]) {
    await page.goto(`${WORKSPACE}/r/${ref}`);
    await expect(page.locator('[data-ref-route="not-found"]')).toBeVisible();
    const url = new URL(page.url());
    expect(url.hostname).toBe("127.0.0.1");
    expect(url.pathname.startsWith(`${WORKSPACE}/r/`)).toBe(true);
  }
});

test("a core outage offers a retry instead of not found", async ({ page }) => {
  await installRefApi(page);
  let fail = true;
  await page.route(
    (url) =>
      url.pathname === "/work/card:launch-plan" ||
      url.pathname === "/work/card%3Alaunch-plan",
    (route) =>
      fail
        ? route.fulfill({
            status: 503,
            json: { error: { code: "unavailable", message: "down" } },
          })
        : route.fallback(),
  );
  await page.goto(`${WORKSPACE}/r/card:launch-plan`);
  const error = page.locator('[data-ref-route="error"]');
  await expect(error).toBeVisible();
  await expect(page.locator('[data-ref-route="not-found"]')).toHaveCount(0);
  fail = false;
  await error.getByRole("button", { name: "Retry" }).click();
  await expect(page).toHaveURL(`${WORKSPACE}/initiatives/card%3Alaunch-plan`);
});
