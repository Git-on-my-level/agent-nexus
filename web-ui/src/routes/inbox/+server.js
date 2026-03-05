import { json } from "@sveltejs/kit";

import { listMockInboxItems } from "$lib/mockCoreData";
import { guardMockRoute } from "$lib/server/mockGuard";

export function GET({ url }) {
  const guardResponse = guardMockRoute(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  return json({
    items: listMockInboxItems(),
    generated_at: new Date().toISOString(),
  });
}
