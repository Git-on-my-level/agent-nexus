import {
  loadWorkspaceCatalog,
  toPublicWorkspaceCatalog,
} from "$lib/server/workspaceCatalog";

export function load() {
  return toPublicWorkspaceCatalog(loadWorkspaceCatalog());
}
