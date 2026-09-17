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

test("preserves a configured mount prefix in redirects and generated links", async ({
  page,
}) => {
  await page.addInitScript(() => {
    window.localStorage.setItem("anx_ui_actor_id:local", "actor-ops-ai");
    window.localStorage.setItem("workspaceTourSeen.local", "1");
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

  await page.goto(appPath("/"));

  await expect(page).toHaveURL(
    new RegExp(`${APP_BASE_PATH}/o/local/w/local/inbox/?$`),
  );
  await expect(
    page.getByRole("heading", { name: "Inbox", exact: true }),
  ).toBeVisible();

  await expect(
    page.locator(`a[href="${appPath("/o/local/w/local/tasks")}"]`).first(),
  ).toBeVisible();
  await expect(
    page.locator(`a[href="${appPath("/o/local/w/local/docs")}"]`).first(),
  ).toBeVisible();

  await page
    .locator(`a[href="${appPath("/o/local/w/local/docs")}"]`)
    .first()
    .click();
  await expect(page).toHaveURL(
    new RegExp(`${APP_BASE_PATH}/o/local/w/local/docs/?$`),
  );
  await expect(
    page.getByRole("heading", { name: "Docs", exact: true }),
  ).toBeVisible();
});
