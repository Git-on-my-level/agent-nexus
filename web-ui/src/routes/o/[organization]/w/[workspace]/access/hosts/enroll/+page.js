import { redirect } from "@sveltejs/kit";

import { workspacePath } from "$lib/workspacePaths";

/**
 * The verification URL `anx host enroll` prints (`/access/hosts/enroll`)
 * lands on the pending requests in Access.
 */
export function load({ params }) {
  redirect(
    307,
    `${workspacePath(params.organization, params.workspace, "/access")}#host-requests`,
  );
}
