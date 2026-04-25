import { dev } from "$app/environment";
import { env as privateEnv } from "$env/dynamic/private";

import {
  normalizeOrganizationSlug,
  normalizeWorkspaceSlug,
  workspaceCompositeKey,
} from "$lib/workspacePaths";
import { normalizeBaseUrl } from "$lib/config";
import { resolveWorkspaceEnv } from "$lib/compat/workspaceCompat";

/** Shown in parse errors; keep in sync with web-ui README / runbook examples. */
const ANX_WORKSPACES_JSON_EXAMPLE =
  '[{"organizationSlug":"local","slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000"}]';

/** `..."http://..." ]` instead of `..."http://..."} ]` — common .env / copy-paste mistake. */
function hintAnxWorkspacesBracketTypo(trimmed) {
  if (!/"http[s]?:\/\/[^"]+"\s*\]/.test(trimmed)) {
    return "";
  }
  return ' Hint: close the workspace object with "}" before the final "]" (e.g. ..."8000"}] not ..."8000"]).';
}

function formatAnxWorkspacesJsonError(trimmed, error) {
  const reason = error instanceof Error ? error.message : String(error);
  const posMatch = reason.match(/position (\d+)/);
  const pos = posMatch ? Number(posMatch[1]) : NaN;
  let context = "";
  if (Number.isFinite(pos) && pos >= 0 && trimmed.length > 0) {
    const windowStart = Math.max(0, pos - 24);
    const windowEnd = Math.min(trimmed.length, pos + 24);
    const slice = trimmed.slice(windowStart, windowEnd);
    const caretOffset = Math.min(pos - windowStart, slice.length);
    const caret = `${" ".repeat(caretOffset)}^`;
    context = ` Context around failure: ${JSON.stringify(slice)} ${caret}`;
  }
  const typoHint = /Expected ',' or '}'/.test(reason)
    ? hintAnxWorkspacesBracketTypo(trimmed)
    : "";
  return `ANX_WORKSPACES must be valid JSON. ${reason}.${context}${typoHint} Unset ANX_WORKSPACES to fall back to an empty catalog, or fix the value. Example: ${ANX_WORKSPACES_JSON_EXAMPLE}`;
}

/**
 * Fixes two copy/paste typos seen in ANX_WORKSPACES without changing valid JSON.
 * Only applied after JSON.parse fails.
 */
function repairCommonAnxWorkspacesJsonTypos(trimmed) {
  let s = trimmed;
  const afterSwap = s.replace(/("http[s]?:\/\/[^"]+")\s*\]\s*\}\s*$/, "$1}]");
  if (afterSwap !== s) return afterSwap;
  return s.replace(/("http[s]?:\/\/[^"]+")\s*\]\s*$/, "$1}]");
}

function normalizeWorkspaceEntry(entry, index, defaultOrganizationSlugHint) {
  if (!entry || typeof entry !== "object") {
    throw new Error(`ANX_WORKSPACES entry ${index + 1} must be an object.`);
  }

  const slug = normalizeWorkspaceSlug(entry.slug);
  if (!slug) {
    throw new Error(
      `ANX_WORKSPACES entry ${index + 1} is missing a valid slug.`,
    );
  }

  const organizationSlug = normalizeOrganizationSlug(
    entry.organizationSlug ??
      entry.organization_slug ??
      defaultOrganizationSlugHint ??
      "",
  );
  if (!organizationSlug) {
    throw new Error(
      `ANX_WORKSPACES entry ${index + 1} is missing organizationSlug (set it on each entry, or set ANX_DEFAULT_ORGANIZATION for object-form entries).`,
    );
  }

  return {
    organizationSlug,
    slug,
    label: String(entry.label ?? slug).trim() || slug,
    description: String(entry.description ?? "").trim(),
    coreBaseUrl: normalizeBaseUrl(entry.coreBaseUrl ?? entry.core_base_url),
    publicOrigin: normalizeBaseUrl(entry.publicOrigin ?? entry.public_origin),
  };
}

function parseWorkspaceEntries(rawValue, defaultOrganizationSlugHint = "") {
  const trimmed = String(rawValue ?? "").trim();
  if (!trimmed) {
    return [];
  }

  let parsed;
  try {
    parsed = JSON.parse(trimmed);
  } catch (firstError) {
    const repaired = repairCommonAnxWorkspacesJsonTypos(trimmed);
    if (repaired !== trimmed) {
      try {
        parsed = JSON.parse(repaired);
      } catch {
        throw new Error(formatAnxWorkspacesJsonError(trimmed, firstError));
      }
    } else {
      throw new Error(formatAnxWorkspacesJsonError(trimmed, firstError));
    }
  }

  const defaultOrg = normalizeOrganizationSlug(defaultOrganizationSlugHint);

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

  return entries.map((e, i) => normalizeWorkspaceEntry(e, i, defaultOrg));
}

export function createWorkspaceCatalog({
  workspaces,
  defaultWorkspaceSlug = "",
  defaultOrganizationSlug = "",
  devActorMode = false,
  usesSyntheticDefaultWorkspace = false,
  allowsEmptyCatalog = false,
}) {
  if (allowsEmptyCatalog && workspaces.length === 0) {
    return {
      defaultWorkspace: null,
      workspaces: [],
      workspaceByComposite: new Map(),
      devActorMode,
      usesSyntheticDefaultWorkspace: false,
      allowsEmptyCatalog: true,
    };
  }

  if (workspaces.length === 0) {
    return {
      defaultWorkspace: null,
      workspaces: [],
      workspaceByComposite: new Map(),
      devActorMode,
      usesSyntheticDefaultWorkspace: false,
      allowsEmptyCatalog: false,
    };
  }

  const defaultCandidate = normalizeWorkspaceSlug(defaultWorkspaceSlug);
  const defaultOrgCandidate = normalizeOrganizationSlug(
    defaultOrganizationSlug,
  );

  const defaultWorkspace =
    defaultCandidate && defaultOrgCandidate
      ? workspaces.find(
          (workspace) =>
            workspace.slug === defaultCandidate &&
            workspace.organizationSlug === defaultOrgCandidate,
        )
      : null;

  const resolvedDefault =
    defaultWorkspace ??
    (defaultCandidate
      ? workspaces.find((workspace) => workspace.slug === defaultCandidate)
      : null) ??
    workspaces[0];

  if (!resolvedDefault) {
    return {
      defaultWorkspace: null,
      workspaces: [],
      workspaceByComposite: new Map(),
      devActorMode,
      usesSyntheticDefaultWorkspace: false,
      allowsEmptyCatalog: false,
    };
  }

  return {
    defaultWorkspace: resolvedDefault,
    workspaces,
    workspaceByComposite: new Map(
      workspaces.map((workspace) => [
        workspaceCompositeKey(workspace.organizationSlug, workspace.slug),
        workspace,
      ]),
    ),
    devActorMode,
    usesSyntheticDefaultWorkspace,
    allowsEmptyCatalog: false,
  };
}

export function loadWorkspaceCatalog(
  env = privateEnv,
  { allowsEmptyStaticCatalog = false } = {},
) {
  const resolved = resolveWorkspaceEnv(env);
  const defaultOrgHint = String(resolved.ANX_DEFAULT_ORGANIZATION ?? "").trim();
  const configuredWorkspaces = parseWorkspaceEntries(
    resolved.ANX_WORKSPACES,
    defaultOrgHint,
  );
  const preconfiguredWorkspaces = parseWorkspaceEntries(
    env.ANX_SAAS_DEV_PRECONFIGURED_WORKSPACES,
    defaultOrgHint,
  );

  let workspaces;
  let usesSyntheticDefaultWorkspace = false;
  if (configuredWorkspaces.length > 0) {
    workspaces = configuredWorkspaces;
    usesSyntheticDefaultWorkspace = false;
  } else if (preconfiguredWorkspaces.length > 0) {
    workspaces = preconfiguredWorkspaces;
    usesSyntheticDefaultWorkspace = false;
  } else if (allowsEmptyStaticCatalog) {
    workspaces = [];
    usesSyntheticDefaultWorkspace = false;
  } else {
    workspaces = [];
    usesSyntheticDefaultWorkspace = false;
  }
  const devActorFromEnv =
    env.ANX_DEV_ACTOR_MODE === "true" || env.ANX_DEV_ACTOR_MODE === "1";
  const devActorMode = dev && devActorFromEnv;

  return createWorkspaceCatalog({
    workspaces,
    defaultWorkspaceSlug: resolved.ANX_DEFAULT_WORKSPACE,
    defaultOrganizationSlug: resolved.ANX_DEFAULT_ORGANIZATION,
    devActorMode,
    usesSyntheticDefaultWorkspace,
    allowsEmptyCatalog: allowsEmptyStaticCatalog && workspaces.length === 0,
  });
}

export function getWorkspaceBySlug(
  workspaceSlug,
  env = privateEnv,
  organizationSlug = "",
) {
  const catalog = loadWorkspaceCatalog(env);
  const org = normalizeOrganizationSlug(organizationSlug);
  if (!org) {
    return null;
  }
  const key = workspaceCompositeKey(org, workspaceSlug);
  return catalog.workspaceByComposite.get(key) ?? null;
}

export function toPublicWorkspaceCatalog(catalog) {
  if (!catalog.defaultWorkspace) {
    return {
      defaultWorkspace: null,
      workspaces: [],
      devActorMode: catalog.devActorMode ?? false,
    };
  }
  return {
    defaultWorkspace: {
      organizationSlug: catalog.defaultWorkspace.organizationSlug,
      slug: catalog.defaultWorkspace.slug,
    },
    workspaces: catalog.workspaces.map((workspace) => ({
      organizationSlug: workspace.organizationSlug,
      slug: workspace.slug,
      label: workspace.label,
      description: workspace.description,
    })),
    devActorMode: catalog.devActorMode ?? false,
  };
}
