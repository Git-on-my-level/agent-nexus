import { json } from "@sveltejs/kit";
import { env } from "$env/dynamic/private";

function normalizeCoreBaseUrl(value) {
  return String(value ?? "")
    .trim()
    .replace(/\/+$/, "");
}

export function mockResultToResponse(result, successStatus = 200) {
  if (result?.error === "conflict") {
    return json({ error: result.message ?? "Conflict." }, { status: 409 });
  }
  if (result?.error === "not_found") {
    return json({ error: result.message ?? "Not found." }, { status: 404 });
  }
  if (result?.error === "validation") {
    return json({ error: result.message ?? "Validation error." }, { status: 400 });
  }
  return json(result, { status: successStatus });
}

export function guardMockRoute(pathname) {
  const coreBaseUrl = normalizeCoreBaseUrl(env.OAR_CORE_BASE_URL);

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
