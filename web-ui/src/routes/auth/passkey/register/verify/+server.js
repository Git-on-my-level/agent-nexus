import {
  resolveWorkspaceSlugFromEvent,
  proxyWorkspaceAuthVerify,
} from "$lib/server/authSession";

export async function POST(event) {
  const resolved = await resolveWorkspaceSlugFromEvent(event);
  if (resolved.error) {
    return new Response(JSON.stringify(resolved.error.payload), {
      status: resolved.error.status,
      headers: {
        "content-type": "application/json",
      },
    });
  }

  return proxyWorkspaceAuthVerify({
    event,
    workspaceSlug: resolved.workspaceSlug,
    coreBaseUrl: resolved.coreBaseUrl,
    pathname: "/auth/passkey/register/verify",
  });
}
