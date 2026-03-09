import { expect, test } from "@playwright/test";

const APP_BASE_PATH = normalizeBasePath(
  process.env.PLAYWRIGHT_APP_BASE_PATH ?? "/oar",
);

function normalizeBasePath(value = "") {
  const trimmed = String(value ?? "").trim();
  if (!trimmed || trimmed === "/") {
    return "";
  }

  const normalized = trimmed.startsWith("/") ? trimmed : `/${trimmed}`;
  return normalized.replace(/\/+$/, "");
}

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
    window.localStorage.setItem("oar_ui_actor_id", "actor-ops-ai");
    window.localStorage.setItem("oar_ui_actor_id:local", "actor-ops-ai");
  });

  await page.goto(appPath("/"));

  await expect(page).toHaveURL(new RegExp(`${APP_BASE_PATH}/local/?$`));
  await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible();

  await expect(
    page.locator(`a[href="${appPath("/local/inbox")}"]`).first(),
  ).toBeVisible();
  await expect(
    page.locator(`a[href="${appPath("/local/threads")}"]`).first(),
  ).toBeVisible();
  await expect(
    page.locator(`a[href="${appPath("/local/artifacts")}"]`).first(),
  ).toBeVisible();

  await page
    .locator(`a[href="${appPath("/local/threads")}"]`)
    .first()
    .click();
  await expect(page).toHaveURL(new RegExp(`${APP_BASE_PATH}/local/threads/?$`));
  await expect(page.getByRole("heading", { name: "Threads" })).toBeVisible();
});
