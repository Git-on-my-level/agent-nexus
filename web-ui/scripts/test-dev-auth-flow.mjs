#!/usr/bin/env node

/**
 * Integration test for the dev auth flow through the SvelteKit proxy.
 *
 * Requires `make serve` (or equivalent) to be running.
 * Not included in `make check` or CI — run manually:
 *
 *   node web-ui/scripts/test-dev-auth-flow.mjs
 *
 * Tests:
 *   1. GET /auth/dev/default-persona  → returns a human persona
 *   2. POST /auth/dev/session         → sets auth cookies
 *   3. GET /auth/session              → returns authenticated agent
 *   4. GET /topics                    → 200 (workspace-business, works without auth in dev)
 *   5. GET /auth/principals           → 200 (authenticated-principal, requires Bearer via proxy)
 *   6. GET /auth/invites              → 200 (authenticated-principal)
 *   7. GET /auth/audit                → 200 (authenticated-principal)
 *   8. GET /secrets                   → 200 (authenticated-principal)
 *
 * Steps 5-8 are the ones that fail when cookies aren't properly attached by
 * the proxy as Bearer tokens. This script isolates the exact point of failure.
 */

const WEB_UI_BASE = process.env.ANX_WEB_UI_BASE_URL || "http://127.0.0.1:5173";
const WORKSPACE_SLUG = process.env.ANX_WORKSPACE_SLUG || "local";
const WORKSPACE_HEADER = "x-anx-workspace-slug";

const results = [];
let cookieJar = {};

function log(label, ...args) {
  console.log(`  ${label}`, ...args);
}

function parseCookies(response) {
  const cookies = {};
  const setCookieHeaders = response.headers.getSetCookie?.() ?? [];
  for (const header of setCookieHeaders) {
    const [pair] = header.split(";");
    const eqIdx = pair.indexOf("=");
    if (eqIdx > 0) {
      const name = pair.slice(0, eqIdx).trim();
      const value = pair.slice(eqIdx + 1).trim();
      cookies[name] = value;
    }
  }
  return cookies;
}

function mergeCookies(existing, incoming) {
  return { ...existing, ...incoming };
}

function cookieHeader(jar) {
  return Object.entries(jar)
    .map(([k, v]) => `${k}=${v}`)
    .join("; ");
}

async function request(method, path, { body, withCookies = false } = {}) {
  const url = `${WEB_UI_BASE}${path}`;
  const headers = {
    [WORKSPACE_HEADER]: WORKSPACE_SLUG,
  };
  if (body) {
    headers["content-type"] = "application/json";
  }
  if (withCookies && Object.keys(cookieJar).length > 0) {
    headers["cookie"] = cookieHeader(cookieJar);
  }

  const init = { method, headers, redirect: "manual" };
  if (body) {
    init.body = JSON.stringify(body);
  }

  const response = await fetch(url, init);
  const incoming = parseCookies(response);
  if (Object.keys(incoming).length > 0) {
    cookieJar = mergeCookies(cookieJar, incoming);
  }

  let payload;
  const ct = response.headers.get("content-type") ?? "";
  if (ct.includes("application/json")) {
    try {
      payload = await response.json();
    } catch {
      payload = null;
    }
  } else {
    payload = await response.text().catch(() => null);
  }

  return { status: response.status, payload, cookies: incoming, response };
}

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

async function runTest(name, fn) {
  process.stdout.write(`  ${name} ... `);
  try {
    await fn();
    console.log("PASS");
    results.push({ name, pass: true });
  } catch (error) {
    console.log(`FAIL: ${error.message}`);
    results.push({ name, pass: false, error: error.message });
  }
}

async function main() {
  console.log(`\nDev auth flow integration test`);
  console.log(`  web-ui: ${WEB_UI_BASE}`);
  console.log(`  workspace: ${WORKSPACE_SLUG}\n`);

  // ── Step 0: Check connectivity ──────────────────────────────────────────
  try {
    const res = await fetch(`${WEB_UI_BASE}/auth/session`, {
      headers: { [WORKSPACE_HEADER]: WORKSPACE_SLUG },
    });
    if (!res.ok && res.status !== 401) {
      console.error(`Cannot reach web-ui at ${WEB_UI_BASE} (status ${res.status}). Is \`make serve\` running?`);
      process.exit(1);
    }
  } catch (error) {
    console.error(`Cannot reach web-ui at ${WEB_UI_BASE}: ${error.message}`);
    console.error("Start the dev environment with `make serve` first.");
    process.exit(1);
  }

  // ── Step 1: GET /auth/dev/default-persona ───────────────────────────────
  let personaId;
  await runTest("GET /auth/dev/default-persona returns human persona", async () => {
    const { status, payload } = await request("GET", "/auth/dev/default-persona");
    assert(status === 200, `expected 200, got ${status}`);
    if (payload?.persona?.persona_id) {
      personaId = payload.persona.persona_id;
      log("persona_id:", personaId, "actor_id:", payload.persona.actor_id);
    } else {
      log("WARNING: default-persona returned null — `default` field likely missing from local-identities.json");
      log("Falling back to /auth/dev/identities to find first human persona");
    }
  });

  // ── Step 1b: Fallback — find first human persona from identities ───────
  if (!personaId) {
    await runTest("GET /auth/dev/identities fallback for human persona", async () => {
      const { status, payload } = await request("GET", "/auth/dev/identities");
      assert(status === 200, `expected 200, got ${status}`);
      const personas = payload?.personas ?? [];
      const human = personas.find(
        (p) => String(p.principal_kind ?? "").toLowerCase() === "human",
      );
      assert(human, `no human persona found in ${personas.length} personas`);
      personaId = human.persona_id;
      log("persona_id:", personaId, "(fallback, missing default field)");
    });
  }

  if (!personaId) {
    console.error("\nCannot continue without a persona. Aborting.\n");
    process.exit(1);
  }

  // ── Step 2: POST /auth/dev/session ──────────────────────────────────────
  await runTest("POST /auth/dev/session sets auth cookies", async () => {
    const { status, payload, cookies } = await request(
      "POST",
      "/auth/dev/session",
      { body: { persona_id: personaId }, withCookies: false },
    );
    assert(status === 200, `expected 200, got ${status}: ${JSON.stringify(payload)}`);
    assert(payload?.ok === true, `response not ok: ${JSON.stringify(payload)}`);
    assert(payload?.agent, `no agent in response`);

    const cookieNames = Object.keys(cookies);
    log("cookies set:", cookieNames.join(", ") || "(none)");
    log("agent:", payload.agent?.username ?? payload.agent?.agent_id);

    const sessionCookie = `oar_ui_session_${WORKSPACE_SLUG}`;
    const accessCookie = `oar_ui_access_${WORKSPACE_SLUG}`;
    assert(
      cookies[sessionCookie] || cookieJar[sessionCookie],
      `refresh cookie ${sessionCookie} not set. Got: ${cookieNames.join(", ")}`,
    );
    assert(
      cookies[accessCookie] || cookieJar[accessCookie],
      `access cookie ${accessCookie} not set. Got: ${cookieNames.join(", ")}`,
    );
  });

  // ── Step 2b: Dump cookie state ──────────────────────────────────────────
  log("cookie jar:", Object.keys(cookieJar).join(", "));
  for (const [name, value] of Object.entries(cookieJar)) {
    const preview = value.length > 40 ? `${value.slice(0, 40)}...` : value;
    log(`  ${name} = ${preview}`);
  }

  // ── Step 3: GET /auth/session ───────────────────────────────────────────
  await runTest("GET /auth/session returns authenticated agent", async () => {
    const { status, payload } = await request("GET", "/auth/session", {
      withCookies: true,
    });
    assert(status === 200, `expected 200, got ${status}`);
    assert(payload?.authenticated === true, `not authenticated: ${JSON.stringify(payload)}`);
    assert(payload?.agent?.actor_id, `no actor_id: ${JSON.stringify(payload)}`);
    log("agent:", payload.agent.username, "actor:", payload.agent.actor_id);
  });

  // ── Step 4: GET /topics (workspace-business, works without auth) ───────
  await runTest("GET /topics (workspace-business) succeeds", async () => {
    const { status, payload } = await request("GET", "/topics", {
      withCookies: true,
    });
    assert(status === 200, `expected 200, got ${status}: ${JSON.stringify(payload)?.slice(0, 200)}`);
    log("topics:", Array.isArray(payload?.topics) ? payload.topics.length : "?");
  });

  // ── Step 5: GET /auth/principals (authenticated-principal) ─────────────
  await runTest("GET /auth/principals (authenticated-principal) succeeds", async () => {
    const { status, payload } = await request("GET", "/auth/principals", {
      withCookies: true,
    });
    assert(
      status === 200,
      `expected 200, got ${status}: ${JSON.stringify(payload)?.slice(0, 300)}`,
    );
    log("principals:", Array.isArray(payload?.principals) ? payload.principals.length : "?");
  });

  // ── Step 6: GET /auth/invites ──────────────────────────────────────────
  await runTest("GET /auth/invites (authenticated-principal) succeeds", async () => {
    const { status, payload } = await request("GET", "/auth/invites", {
      withCookies: true,
    });
    assert(
      status === 200,
      `expected 200, got ${status}: ${JSON.stringify(payload)?.slice(0, 300)}`,
    );
    log("invites:", Array.isArray(payload?.invites) ? payload.invites.length : "?");
  });

  // ── Step 7: GET /auth/audit ────────────────────────────────────────────
  await runTest("GET /auth/audit (authenticated-principal) succeeds", async () => {
    const { status, payload } = await request("GET", "/auth/audit", {
      withCookies: true,
    });
    assert(
      status === 200,
      `expected 200, got ${status}: ${JSON.stringify(payload)?.slice(0, 300)}`,
    );
    log("audit events:", Array.isArray(payload?.events) ? payload.events.length : "?");
  });

  // ── Step 8: GET /secrets ───────────────────────────────────────────────
  await runTest("GET /secrets (authenticated-principal) succeeds", async () => {
    const { status, payload } = await request("GET", "/secrets", {
      withCookies: true,
    });
    assert(
      status === 200,
      `expected 200, got ${status}: ${JSON.stringify(payload)?.slice(0, 300)}`,
    );
  });

  // ── Step 9: GET /auth/principals WITHOUT cookies → expect 401 ──────────
  await runTest("GET /auth/principals WITHOUT cookies fails with 401", async () => {
    const url = `${WEB_UI_BASE}/auth/principals`;
    const response = await fetch(url, {
      headers: { [WORKSPACE_HEADER]: WORKSPACE_SLUG },
    });
    assert(
      response.status === 401,
      `expected 401 without cookies, got ${response.status}`,
    );
  });

  // ── Summary ─────────────────────────────────────────────────────────────
  console.log("\n── Summary ──────────────────────────────────────────────────");
  const passed = results.filter((r) => r.pass).length;
  const failed = results.filter((r) => !r.pass).length;
  console.log(`  ${passed} passed, ${failed} failed\n`);

  if (failed > 0) {
    console.log("  Failures:");
    for (const r of results.filter((r) => !r.pass)) {
      console.log(`    ✗ ${r.name}`);
      console.log(`      ${r.error}`);
    }
    console.log();
    process.exit(1);
  }
}

main().catch((error) => {
  console.error("\nUnexpected error:", error);
  process.exit(1);
});
