import { json } from "@sveltejs/kit";

import { getMockBoardWorkspace } from "$lib/mockCoreData";
import { assertMockModeEnabled } from "$lib/server/mockGuard";

export function GET({ params, url }) {
  const guardResponse = assertMockModeEnabled(url.pathname);
  if (guardResponse) {
    return guardResponse;
  }

  const workspace = getMockBoardWorkspace(params.boardId);
  if (!workspace) {
    return json({ error: "Board not found" }, { status: 404 });
  }

  return json(workspace);
}
