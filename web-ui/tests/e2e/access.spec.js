import { expect, test } from "@playwright/test";

test("renders the access page without auth seeding", async ({ page }) => {
  await page.goto("/o/local/w/local/access");

  await expect(
    page.getByRole("heading", { name: "Select Actor Identity" }),
  ).toBeVisible();
  await expect(page.getByText("Prefer authenticated access?")).toBeVisible();
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

  await page.goto("/o/local/w/local/access");

  const session = await page.evaluate(async () => {
    const response = await fetch("/auth/session", {
      headers: {
        "x-anx-workspace-slug": "local",
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

test("does not repeat the username in principal rows", async ({ page }) => {
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

  await page.route("**/auth/principals?**", async (route) => {
    await route.fulfill({
      status: 200,
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        principals: [
          {
            agent_id: "agent-ops-ai",
            actor_id: "actor-ops-ai",
            username: "m4-hermes",
            principal_kind: "agent",
            auth_method: "public_key",
            created_at: "2026-03-01T10:00:00Z",
            last_seen_at: "2026-03-20T11:15:00Z",
            updated_at: "2026-03-28T10:00:00Z",
            revoked: false,
            registration: {
              version: "agent-registration/v1",
              handle: "m4-hermes",
              actor_id: "actor-ops-ai",
              status: "active",
              bridge_instance_id: "bridge-hermes-1",
              bridge_checked_in_at: "2099-03-20T12:00:00Z",
              bridge_expires_at: "2099-03-20T12:05:00Z",
              workspace_bindings: [{ workspace_id: "local", enabled: true }],
            },
            wake_routing: {
              applicable: true,
              handle: "m4-hermes",
              taggable: true,
              online: true,
              state: "online",
              summary: "Online as @m4-hermes.",
            },
          },
        ],
        active_human_principal_count: 0,
      }),
    });
  });

  await page.route("**/auth/invites", async (route) => {
    await route.fulfill({
      status: 200,
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ invites: [] }),
    });
  });

  await page.route("**/auth/audit?**", async (route) => {
    await route.fulfill({
      status: 200,
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ events: [] }),
    });
  });

  await page.goto("/o/local/w/local/access");

  await expect(page.getByText("m4-hermes", { exact: true })).toBeVisible();
  await expect(
    page.getByText("agent via public_key", { exact: true }),
  ).toBeVisible();
  const onlineBadge = page.getByRole("button", { name: "Online" });
  await expect(onlineBadge).toBeVisible();
  await onlineBadge.click();
  await expect(
    page.getByText("Online as @m4-hermes.", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("m4-hermes • agent via public_key", { exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByText("Joined Mar 1, 2026", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Last seen Mar 20, 2026", { exact: true }),
  ).toBeVisible();
});
