import { json } from "@sveltejs/kit";

import { listMockInboxItems } from "$lib/mockCoreData";
import { assertMockModeEnabled } from "$lib/server/mockGuard";

export function GET({ url }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  return json({
    items: listMockInboxItems(),
    generated_at: new Date().toISOString(),
  });
}
