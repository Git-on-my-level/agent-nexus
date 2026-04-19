import { redirect } from "@sveltejs/kit";

import { workspacePath } from "$lib/workspacePaths";

export async function load(event) {
  throw redirect(
    307,
    workspacePath(event.params.organization, event.params.workspace, "/topics"),
  );
}
