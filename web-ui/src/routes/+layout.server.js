import { loadWorkspaceCatalog } from "$lib/server/workspaceCatalog";

export function load() {
  const catalog = loadWorkspaceCatalog();
  return {
    defaultWorkspace: catalog.defaultWorkspace.slug,
    workspaces: catalog.workspaces.map((workspace) => ({
      slug: workspace.slug,
      label: workspace.label,
      description: workspace.description,
    })),
  };
}
