import { env as privateEnv } from "$env/dynamic/private";

import {
  DEFAULT_WORKSPACE_SLUG,
  normalizeWorkspaceSlug,
} from "$lib/workspacePaths";
import { normalizeBaseUrl } from "$lib/config";
import { resolveWorkspaceEnv } from "$lib/compat/workspaceCompat";

function normalizeWorkspaceEntry(entry, index) {
  if (!entry || typeof entry !== "object") {
    throw new Error(`OAR_WORKSPACES entry ${index + 1} must be an object.`);
  }

  const slug = normalizeWorkspaceSlug(entry.slug);
  if (!slug) {
    throw new Error(
      `OAR_WORKSPACES entry ${index + 1} is missing a valid slug.`,
    );
  }

  return {
    slug,
    label: String(entry.label ?? slug).trim() || slug,
    description: String(entry.description ?? "").trim(),
    coreBaseUrl: normalizeBaseUrl(entry.coreBaseUrl ?? entry.core_base_url),
  };
}

function parseWorkspaceEntries(rawValue) {
  const trimmed = String(rawValue ?? "").trim();
  if (!trimmed) {
    return [];
  }

  let parsed;
  try {
    parsed = JSON.parse(trimmed);
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error);
    throw new Error(`OAR_WORKSPACES must be valid JSON. ${reason}`);
  }

  const entries = Array.isArray(parsed)
    ? parsed
    : Object.entries(parsed ?? {}).map(([slug, value]) =>
        value && typeof value === "object"
          ? {
              slug,
              ...value,
            }
          : {
              slug,
              coreBaseUrl: value,
            },
      );

  return entries.map(normalizeWorkspaceEntry);
}

function fallbackSingleWorkspace(env) {
  return [
    {
      slug: DEFAULT_WORKSPACE_SLUG,
      label: "Local",
      description: "",
      coreBaseUrl: normalizeBaseUrl(env.OAR_CORE_BASE_URL),
    },
  ];
}

export function loadWorkspaceCatalog(env = privateEnv) {
  const resolved = resolveWorkspaceEnv(env);
  const configuredWorkspaces = parseWorkspaceEntries(resolved.OAR_WORKSPACES);
  const workspaces =
    configuredWorkspaces.length > 0
      ? configuredWorkspaces
      : fallbackSingleWorkspace(env);
  const defaultCandidate = normalizeWorkspaceSlug(
    resolved.OAR_DEFAULT_WORKSPACE ||
      workspaces[0]?.slug ||
      DEFAULT_WORKSPACE_SLUG,
  );
  const defaultWorkspace =
    workspaces.find((workspace) => workspace.slug === defaultCandidate) ??
    workspaces[0];

  if (!defaultWorkspace) {
    throw new Error("At least one OAR workspace must be configured.");
  }

  const devActorMode =
    env.OAR_DEV_ACTOR_MODE === "true" || env.OAR_DEV_ACTOR_MODE === "1";
  return {
    defaultWorkspace,
    workspaces,
    workspaceBySlug: new Map(
      workspaces.map((workspace) => [workspace.slug, workspace]),
    ),
    devActorMode,
  };
}

export function getWorkspaceBySlug(workspaceSlug, env = privateEnv) {
  const catalog = loadWorkspaceCatalog(env);
  return (
    catalog.workspaceBySlug.get(normalizeWorkspaceSlug(workspaceSlug)) ?? null
  );
}

export function toPublicWorkspaceCatalog(catalog) {
  return {
    defaultWorkspace: catalog.defaultWorkspace.slug,
    workspaces: catalog.workspaces.map((workspace) => ({
      slug: workspace.slug,
      label: workspace.label,
      description: workspace.description,
    })),
    devActorMode: catalog.devActorMode ?? false,
  };
}
