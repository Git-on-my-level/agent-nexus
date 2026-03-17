import { redirectToDefaultWorkspace } from "$lib/server/workspaceRedirect";

export function load({ params }) {
  redirectToDefaultWorkspace(`/snapshots/${params.snapshotId}`);
}
