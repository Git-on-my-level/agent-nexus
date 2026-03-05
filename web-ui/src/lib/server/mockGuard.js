import { env } from "$env/dynamic/private";

function normalizeCoreBaseUrl(value) {
  return String(value ?? "")
    .trim()
    .replace(/\/+$/, "");
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
