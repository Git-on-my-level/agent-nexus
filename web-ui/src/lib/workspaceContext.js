import { get, writable } from "svelte/store";

export const currentWorkspaceSlug = writable("");

export function setCurrentWorkspaceSlug(workspaceSlug) {
  const normalized = String(workspaceSlug ?? "").trim();
  currentWorkspaceSlug.set(normalized);
  return normalized;
}

export function getCurrentWorkspaceSlug() {
  return get(currentWorkspaceSlug);
}

export const currentProjectSlug = currentWorkspaceSlug;
export const setCurrentProjectSlug = setCurrentWorkspaceSlug;
export const getCurrentProjectSlug = getCurrentWorkspaceSlug;
