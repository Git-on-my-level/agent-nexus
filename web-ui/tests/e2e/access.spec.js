import { expect, test } from "@playwright/test";

async function seedAuthenticatedAgent(page) {
  await page.addInitScript(() => {
    window.sessionStorage.setItem(
      "oar_ui_refresh_token:local",
      "test-refresh-token",
    );
  });

  await page.route("**/auth/token", async (route) => {
    await route.fulfill({
      status: 200,
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        tokens: {
          access_token: "test-access-token",
          refresh_token: "test-refresh-token",
          token_type: "Bearer",
          expires_in: 3600,
        },
      }),
    });
  });

  await page.route("**/agents/me", async (route) => {
    await route.fulfill({
      status: 200,
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        agent: {
          agent_id: "agent-ops-ai",
          actor_id: "actor-ops-ai",
          username: "ops-ai",
          revoked: false,
          created_at: "2024-01-15T10:00:00Z",
          updated_at: "2024-01-15T10:00:00Z",
        },
        keys: [],
      }),
    });
  });
}

test.describe("Access management page", () => {
  test("shows access denied message for dev actor mode", async ({ page }) => {
    await page.addInitScript(() => {
      window.localStorage.setItem("oar_ui_actor_id", "actor-ops-ai");
    });

    await page.goto("/");

    await page.getByRole("link", { name: "Access", exact: true }).click();
    await expect(page).toHaveURL(/\/access$/);

    await expect(page.getByRole("heading", { name: "Access" })).toBeVisible();
    await expect(
      page.getByText("Sign in with a passkey to manage workspace access"),
    ).toBeVisible();
  });

  test("loads access data for authenticated agent", async ({ page }) => {
    await seedAuthenticatedAgent(page);

    await page.route("**/auth/bootstrap/status", async (route) => {
      await route.fulfill({
        status: 200,
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          bootstrap_registration_available: false,
        }),
      });
    });

    await page.route("**/auth/principals*", async (route) => {
      await route.fulfill({
        status: 200,
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          principals: [
            {
              agent_id: "agent-ops-ai",
              actor_id: "actor-ops-ai",
              username: "ops-ai",
              principal_kind: "agent",
              auth_method: "public_key",
              revoked: false,
              created_at: "2024-01-15T10:00:00Z",
              updated_at: "2024-01-15T10:00:00Z",
            },
          ],
        }),
      });
    });

    await page.route(/\/auth\/invites(?:\/.*)?$/, async (route) => {
      if (route.request().method() === "GET") {
        await route.fulfill({
          status: 200,
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            invites: [
              {
                id: "invite_abc123",
                kind: "agent",
                note: "CI bot",
                created_at: "2024-01-16T12:00:00Z",
              },
            ],
          }),
        });
      } else {
        await route.continue();
      }
    });

    await page.route("**/auth/audit*", async (route) => {
      await route.fulfill({
        status: 200,
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          events: [
            {
              event_id: "evt_001",
              event_type: "principal_registered",
              actor_agent_id: "agent-ops-ai",
              actor_actor_id: "actor-ops-ai",
              subject_agent_id: "agent-ops-ai",
              subject_actor_id: "actor-ops-ai",
              occurred_at: "2024-01-15T10:00:00Z",
              metadata: {},
            },
          ],
        }),
      });
    });

    await page.goto("/");

    await page.getByRole("link", { name: "Access", exact: true }).click();
    await expect(page).toHaveURL(/\/access$/);

    await expect(page.getByRole("heading", { name: "Access" })).toBeVisible();

    await expect(page.getByText("Bootstrap status")).toBeVisible();
    await expect(
      page.getByText("Bootstrap registration is closed"),
    ).toBeVisible();

    await expect(
      page.getByRole("heading", { name: "Principals" }),
    ).toBeVisible();
    await expect(page.getByText("actor-ops-ai")).toBeVisible();

    await expect(page.getByRole("heading", { name: "Invites" })).toBeVisible();
    await expect(page.getByText("invite_abc123")).toBeVisible();

    await expect(
      page.getByRole("heading", { name: "Recent auth events" }),
    ).toBeVisible();
    await expect(
      page.getByText("Principal agent-ops-ai registered"),
    ).toBeVisible();
  });

  test("creates an invite and reveals one-time token", async ({ page }) => {
    await seedAuthenticatedAgent(page);

    await page.route("**/auth/bootstrap/status", async (route) => {
      await route.fulfill({
        status: 200,
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          bootstrap_registration_available: false,
        }),
      });
    });

    await page.route("**/auth/principals*", async (route) => {
      await route.fulfill({
        status: 200,
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ principals: [] }),
      });
    });

    let createInviteCalled = false;
    await page.route(/\/auth\/invites(?:\/.*)?$/, async (route) => {
      const method = route.request().method();
      if (method === "POST") {
        createInviteCalled = true;
        await route.fulfill({
          status: 200,
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            invite: {
              id: "invite_new",
              kind: "agent",
              created_by_agent_id: "agent-ops-ai",
              created_by_actor_id: "actor-ops-ai",
              note: "Test invite",
              created_at: "2024-01-17T14:00:00Z",
            },
            token: "otok_one_time_secret_token_xyz",
          }),
        });
      } else {
        await route.fulfill({
          status: 200,
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ invites: [] }),
        });
      }
    });

    await page.route("**/auth/audit*", async (route) => {
      await route.fulfill({
        status: 200,
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ events: [] }),
      });
    });

    await page.goto("/local/access");

    await expect(page.getByRole("heading", { name: "Access" })).toBeVisible();

    await page.getByLabel("Note (optional)").fill("Test invite");
    await page.getByRole("button", { name: "Create invite" }).click();

    await expect(page.getByText("Invite created successfully")).toBeVisible();
    await expect(
      page.getByText("otok_one_time_secret_token_xyz"),
    ).toBeVisible();
    await expect(
      page.getByText("This one-time token will not be shown again"),
    ).toBeVisible();

    expect(createInviteCalled).toBe(true);
  });

  test("revokes an invite", async ({ page }) => {
    await seedAuthenticatedAgent(page);

    await page.route("**/auth/bootstrap/status", async (route) => {
      await route.fulfill({
        status: 200,
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          bootstrap_registration_available: false,
        }),
      });
    });

    await page.route("**/auth/principals*", async (route) => {
      await route.fulfill({
        status: 200,
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ principals: [] }),
      });
    });

    let revokeCalled = false;
    let inviteRevoked = false;

    await page.route(/\/auth\/invites(?:\/.*)?$/, async (route) => {
      const method = route.request().method();
      const url = route.request().url();

      if (method === "POST" && url.includes("/revoke")) {
        revokeCalled = true;
        inviteRevoked = true;
        await route.fulfill({
          status: 200,
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ ok: true }),
        });
      } else {
        const invites = [
          {
            id: "invite_to_revoke",
            kind: "agent",
            created_by_agent_id: "agent-ops-ai",
            created_by_actor_id: "actor-ops-ai",
            note: "Pending invite",
            created_at: "2024-01-16T12:00:00Z",
            revoked_at: inviteRevoked ? "2024-01-17T15:00:00Z" : null,
          },
        ];
        await route.fulfill({
          status: 200,
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ invites }),
        });
      }
    });

    await page.route("**/auth/audit*", async (route) => {
      await route.fulfill({
        status: 200,
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ events: [] }),
      });
    });

    await page.goto("/local/access");

    await expect(page.getByRole("heading", { name: "Access" })).toBeVisible();

    await expect(page.getByText("invite_to_revoke")).toBeVisible();

    await page.getByRole("button", { name: "Revoke" }).first().click();

    await page.waitForTimeout(500);

    expect(revokeCalled).toBe(true);
  });

  test("shows error states when API fails", async ({ page }) => {
    await seedAuthenticatedAgent(page);

    await page.route("**/auth/bootstrap/status", async (route) => {
      await route.fulfill({
        status: 500,
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ error: "Internal server error" }),
      });
    });

    await page.route("**/auth/principals*", async (route) => {
      await route.fulfill({
        status: 500,
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ error: "Internal server error" }),
      });
    });

    await page.route(/\/auth\/invites(?:\/.*)?$/, async (route) => {
      await route.fulfill({
        status: 500,
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ error: "Internal server error" }),
      });
    });

    await page.route("**/auth/audit*", async (route) => {
      await route.fulfill({
        status: 500,
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ error: "Internal server error" }),
      });
    });

    await page.goto("/local/access");

    await expect(page.getByRole("heading", { name: "Access" })).toBeVisible();

    await expect(page.getByText(/Internal server error/)).toHaveCount(4);
  });

  test("shows empty states when no data", async ({ page }) => {
    await seedAuthenticatedAgent(page);

    await page.route("**/auth/bootstrap/status", async (route) => {
      await route.fulfill({
        status: 200,
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          bootstrap_registration_available: true,
        }),
      });
    });

    await page.route("**/auth/principals*", async (route) => {
      await route.fulfill({
        status: 200,
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ principals: [] }),
      });
    });

    await page.route("**/auth/invites*", async (route) => {
      await route.fulfill({
        status: 200,
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ invites: [] }),
      });
    });

    await page.route("**/auth/audit*", async (route) => {
      await route.fulfill({
        status: 200,
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ events: [] }),
      });
    });

    await page.goto("/local/access");

    await expect(page.getByRole("heading", { name: "Access" })).toBeVisible();

    await expect(
      page.getByText("Bootstrap registration is available"),
    ).toBeVisible();
    await expect(page.getByText("No principals found")).toBeVisible();
    await expect(
      page.getByText(
        "No invites yet. Create one above to onboard new principals",
      ),
    ).toBeVisible();
    await expect(page.getByText("No audit events yet")).toBeVisible();
  });
});
