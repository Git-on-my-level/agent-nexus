import { expect, test } from "@playwright/test";
import { normalizeBasePath } from "../../src/lib/pathUtils.js";

const APP_BASE_PATH = normalizeBasePath(
  process.env.PLAYWRIGHT_APP_BASE_PATH ?? "/anx",
);

function appPath(pathname = "/") {
  const normalizedPathname =
    pathname === "/"
      ? "/"
      : pathname.startsWith("/")
        ? pathname
        : `/${pathname}`;
  if (!APP_BASE_PATH) {
    return normalizedPathname;
  }

  return normalizedPathname === "/"
    ? APP_BASE_PATH
    : `${APP_BASE_PATH}${normalizedPathname}`;
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("anx_ui_actor_id:local", "actor-ops-ai");
    window.localStorage.setItem("workspaceTourSeen.local", "1");
  });

  await page.context().addCookies([
    {
      name: "anx_last_workspace",
      value: "local:local",
      domain: "127.0.0.1",
      path: "/",
    },
  ]);

  await page.route(/\/actors$/, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        actors: [{ id: "actor-ops-ai", display_name: "Ops AI" }],
      }),
    });
  });
});

test("preserves a configured mount prefix in redirects and generated links", async ({
  page,
}) => {
  await page.goto(appPath("/"));

  await expect(page).toHaveURL(
    new RegExp(`${APP_BASE_PATH}/o/local/w/local/decide/?$`),
  );
  await expect(
    page.getByRole("heading", { name: "Decide", exact: true }),
  ).toBeVisible();

  // Docs and Initiatives are one hop down now (settings, ⌘K); the primary
  // nav links Decide and Watch, and they carry the mount prefix.
  const watchLink = page
    .locator(`a[href="${appPath("/o/local/w/local/watch")}"]`)
    .first();
  await expect(watchLink).toBeVisible();
  await watchLink.click();
  await expect(page).toHaveURL(
    new RegExp(`${APP_BASE_PATH}/o/local/w/local/watch/?$`),
  );
  await expect(
    page.getByRole("heading", { name: "Watch", exact: true }),
  ).toBeVisible();
});

test("workspace root preserves query and fragment under a mount prefix without reloading", async ({
  page,
  baseURL,
}) => {
  const documents = [];
  page.on("request", (request) => {
    if (request.resourceType() === "document") documents.push(request.url());
  });
  await page.goto(
    appPath("/o/local/w/local?dashboard=weekly#overview-reports"),
  );
  await expect(page).toHaveURL(
    `${baseURL}${appPath("/o/local/w/local/decide?dashboard=weekly#overview-reports")}`,
  );
  await expect(
    page.getByRole("heading", { name: "Decide", exact: true }),
  ).toBeVisible();
  expect(documents).toHaveLength(1);
});
