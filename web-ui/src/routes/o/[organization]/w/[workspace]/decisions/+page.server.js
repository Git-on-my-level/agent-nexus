import { redirect } from "@sveltejs/kit";

import { workspacePath } from "$lib/workspacePaths";

export function load(event) {
  const params = new URLSearchParams(event.url.searchParams);
  if (params.get("decision") && !params.get("item")) {
    params.set("item", `decision:${params.get("decision")}`);
    params.delete("decision");
  }
  if (!params.get("mailbox")) params.set("mailbox", "needs-you");
  throw redirect(
    307,
    workspacePath(
      event.params.organization,
      event.params.workspace,
      `/inbox?${params}`,
    ),
  );
}
