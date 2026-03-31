import { expect, test } from "@playwright/test";
import { createSign, generateKeyPairSync } from "node:crypto";

const bridgeProofKey = (() => {
  const { publicKey, privateKey } = generateKeyPairSync("ec", {
    namedCurve: "prime256v1",
    publicKeyEncoding: { format: "der", type: "spki" },
    privateKeyEncoding: { format: "der", type: "pkcs8" },
  });
  return {
    privateKey,
    publicKeyB64: Buffer.from(publicKey).toString("base64"),
  };
})();

function stableJsonValue(value) {
  if (Array.isArray(value)) {
    return value.map((item) => stableJsonValue(item));
  }
  if (value && typeof value === "object") {
    return Object.keys(value)
      .sort()
      .reduce((normalized, key) => {
        normalized[key] = stableJsonValue(value[key]);
        return normalized;
      }, {});
  }
  return value;
}

function signCheckinPayload(content) {
  const signer = createSign("sha256");
  signer.update(
    Buffer.from(
      JSON.stringify(
        stableJsonValue({
          v: "agent-bridge-checkin-proof/v1",
          handle: String(content.handle ?? "").trim(),
          actor_id: String(content.actor_id ?? "").trim(),
          workspace_id: String(content.workspace_id ?? "").trim(),
          bridge_instance_id: String(content.bridge_instance_id ?? "").trim(),
          checked_in_at: String(content.checked_in_at ?? "").trim(),
          expires_at: String(content.expires_at ?? "").trim(),
        }),
      ),
      "utf8",
    ),
  );
  signer.end();
  return signer
    .sign({ key: bridgeProofKey.privateKey, format: "der", type: "pkcs8" })
    .toString("base64");
}

test("renders the access page without auth seeding", async ({ page }) => {
  await page.goto("/local/access");

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
              bridge_signing_public_key_spki_b64: bridgeProofKey.publicKeyB64,
              bridge_checkin_event_id: "event-bridge-checkin-hermes",
              bridge_instance_id: "bridge-hermes-1",
              bridge_checked_in_at: "2099-03-20T12:00:00Z",
              bridge_expires_at: "2099-03-20T12:05:00Z",
              workspace_bindings: [{ workspace_id: "local", enabled: true }],
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

  await page.route("**/events/event-bridge-checkin-hermes", async (route) => {
    const payload = {
      version: "agent-bridge-checkin/v1",
      handle: "m4-hermes",
      actor_id: "actor-ops-ai",
      workspace_id: "local",
      bridge_instance_id: "bridge-hermes-1",
      checked_in_at: "2099-03-20T12:00:00Z",
      expires_at: "2099-03-20T12:05:00Z",
    };
    await route.fulfill({
      status: 200,
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        event: {
          id: "event-bridge-checkin-hermes",
          type: "agent_bridge_checked_in",
          payload: {
            ...payload,
            proof_signature_b64: await signCheckinPayload(payload),
          },
        },
      }),
    });
  });

  await page.route("**/auth/audit?**", async (route) => {
    await route.fulfill({
      status: 200,
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ events: [] }),
    });
  });

  await page.goto("/local/access");

  await expect(page.getByText("m4-hermes", { exact: true })).toBeVisible();
  await expect(
    page.getByText("agent via public_key", { exact: true }),
  ).toBeVisible();
  const onlineBadge = page.getByRole("button", { name: "Online" });
  await expect(onlineBadge).toBeVisible();
  await onlineBadge.click();
  await expect(
    page.getByText(
      "Online for bound workspace local, but this page has no durable workspace ID to confirm the current workspace match.",
      { exact: true },
    ),
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
