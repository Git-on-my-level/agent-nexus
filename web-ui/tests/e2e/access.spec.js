import { expect, test } from "@playwright/test";

test("renders the access page without auth seeding", async ({ page }) => {
  await page.goto("/local/access");

  await expect(page.getByRole("heading", { name: "Access" })).toBeVisible();
  await expect(
    page.getByText("Sign in with a passkey to manage workspace access"),
  ).toBeVisible();
  await expect(page.locator("body")).not.toContainText("oar_ui_refresh_token");
});

test("reads the cookie-backed session from the same-origin endpoint", async ({
  page,
}) => {
  await page.context().addCookies([
    {
      name: "oar_ui_session_local",
      value: "test-refresh-token",
      domain: "127.0.0.1",
      path: "/",
      httpOnly: true,
    },
  ]);

  await page.route("**/auth/session", async (route) => {
    expect(route.request().headers().cookie ?? "").toContain(
      "oar_ui_session_local=test-refresh-token",
    );
    await route.fulfill({
      status: 200,
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        authenticated: true,
        agent: {
          agent_id: "agent-ops-ai",
          actor_id: "actor-ops-ai",
          username: "ops-ai",
        },
      }),
    });
  });

  await page.goto("/local/access");

  const session = await page.evaluate(async () => {
    const response = await fetch("/auth/session", {
      headers: {
        "x-oar-workspace-slug": "local",
      },
    });
    return response.json();
  });

  expect(session).toEqual({
    authenticated: true,
    agent: {
      agent_id: "agent-ops-ai",
      actor_id: "actor-ops-ai",
      username: "ops-ai",
    },
  });
});
