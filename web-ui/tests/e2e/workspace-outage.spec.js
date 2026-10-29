import { expect, test } from "@playwright/test";
import { installWorkspaceApi } from "../helpers/workspaceApiMock.js";

/*
 * A signed-in viewer whose workspace stops answering sees a calm, retryable
 * "temporarily unreachable" state (never the sign-in page), and it clears on
 * Retry or when the browser comes back online. A workspace that is gone shows
 * a definitive "not found" state instead.
 */
const WORKSPACE = "/o/local/w/local";
const PATH = `${WORKSPACE}/docs`;

for (const resume of ["Retry", "online"]) {
  test(`a handshake outage shows a retryable state that clears on ${resume}`, async ({
    page,
  }) => {
    test.setTimeout(60_000);
    await installWorkspaceApi(page);
    let down = true;
    await page.route("**/meta/handshake", (route) =>
      down
        ? route.fulfill({
            status: 503,
            contentType: "application/json",
            body: JSON.stringify({ error: { code: "core_unreachable" } }),
          })
        : route.fallback(),
    );

    await page.goto(PATH);

    const alert = page.locator("[data-workspace-activation-error]");
    await expect(alert).toContainText(
      "This workspace is temporarily unreachable. Try again in a moment.",
    );
    await expect(
      alert.getByRole("button", { name: "Retry", exact: true }),
    ).toBeEnabled();
    expect(new URL(page.url()).pathname).toBe(PATH);

    down = false;
    if (resume === "Retry")
      await alert.getByRole("button", { name: "Retry", exact: true }).click();
    else await page.evaluate(() => window.dispatchEvent(new Event("online")));

    await expect(alert).toHaveCount(0);
    await expect(
      page.getByRole("heading", { name: "Docs", exact: true }),
    ).toBeVisible();
  });
}

test("a missing workspace shows a definitive not-found state, not sign-in", async ({
  page,
}) => {
  test.setTimeout(60_000);
  await installWorkspaceApi(page);
  await page.route("**/auth/session", (route) =>
    route.fulfill({
      status: 404,
      contentType: "application/json",
      body: JSON.stringify({
        authenticated: false,
        agent: null,
        error: { code: "workspace_not_found" },
      }),
    }),
  );

  await page.goto(PATH);

  const alert = page.locator("[data-workspace-not-found]");
  await expect(alert).toContainText(
    "This workspace doesn't exist, or it was moved or removed.",
  );
  await expect(
    alert.getByRole("link", { name: "Go to your workspaces" }),
  ).toBeVisible();
  await expect(alert.getByRole("button", { name: "Retry" })).toHaveCount(0);
  expect(new URL(page.url()).pathname).toBe(PATH);
});
