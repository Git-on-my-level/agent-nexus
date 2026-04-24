import { env as privateEnv } from "$env/dynamic/private";

import { getOutOfWorkspaceProvider } from "$lib/server/outOfWorkspace/index.js";

const handler = (method) => (event) =>
  (
    event.locals?.outOfWorkspace ?? getOutOfWorkspaceProvider(privateEnv)
  ).proxyHostedApi({
    event,
    method,
    subpath: String(event.params.segments ?? ""),
  });

export const GET = handler("GET");
export const POST = handler("POST");
export const PATCH = handler("PATCH");
export const DELETE = handler("DELETE");
