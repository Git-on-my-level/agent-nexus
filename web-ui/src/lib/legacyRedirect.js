import { redirect } from "@sveltejs/kit";
import { workspacePath } from "$lib/workspacePaths";

/**
 * Old destinations (Inbox, Overview, Tasks) moved to Decide, Watch and
 * Initiatives. Links already sent out (notifications, agent output, core's
 * `/tasks/<id>` hrefs) still land in the right place.
 */
export function redirectTo(params, url, path) {
  throw redirect(
    307,
    workspacePath(
      params.organization,
      params.workspace,
      `${path}${url.search}${url.hash}`,
    ),
  );
}
