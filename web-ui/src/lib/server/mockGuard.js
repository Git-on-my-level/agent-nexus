import { json } from "@sveltejs/kit";
import { env } from "$env/dynamic/private";
import { normalizeBaseUrl } from "$lib/config.js";

function invalidMockJsonResponse() {
  return json({ error: "Invalid JSON body." }, { status: 400 });
}

export function mockResultToResponse(result, successStatus = 200) {
  if (result?.error === "conflict") {
    return json({ error: result.message ?? "Conflict." }, { status: 409 });
  }
  if (result?.error === "not_found") {
    return json({ error: result.message ?? "Not found." }, { status: 404 });
  }
  if (result?.error === "validation") {
    return json(
      { error: result.message ?? "Validation error." },
      { status: 400 },
    );
  }
  return json(result, { status: successStatus });
}

export async function readMockJsonBody(request) {
  try {
    return {
      ok: true,
      body: await request.json(),
    };
  } catch {
    return {
      ok: false,
      response: invalidMockJsonResponse(),
    };
  }
}

export function assertMockModeEnabled(pathname) {
  const coreBaseUrl = normalizeBaseUrl(env.OAR_CORE_BASE_URL);

  if (!coreBaseUrl) {
    return null;
  }

  return new Response(
    JSON.stringify({
      error: {
        code: "mock_route_disabled",
        message: `Mock API route ${pathname} is disabled because OAR_CORE_BASE_URL is set (${coreBaseUrl}). Configure proxying in src/hooks.server.js so requests reach oar-core.`,
      },
    }),
    {
      status: 500,
      headers: {
        "content-type": "application/json",
      },
    },
  );
}
