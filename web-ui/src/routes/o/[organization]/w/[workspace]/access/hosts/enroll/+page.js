import { redirect } from "@sveltejs/kit";

import { workspacePath } from "$lib/workspacePaths";

/**
 * Core's configured workspace web URL leads here from `anx host enroll`.
 * Redirect to the pending requests in Access.
 */
export function load({ params }) {
  redirect(
    307,
    `${workspacePath(params.organization, params.workspace, "/access")}#host-requests`,
  );
}
