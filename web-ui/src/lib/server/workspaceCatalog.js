import { env as privateEnv } from "$env/dynamic/private";

import {
  DEFAULT_WORKSPACE_SLUG,
  normalizeWorkspaceSlug,
} from "$lib/workspacePaths";
import { normalizeBaseUrl } from "$lib/config";
import { resolveWorkspaceEnv } from "$lib/compat/workspaceCompat";
import { isSaasPackedHostDev } from "$lib/server/controlPlaneWorkspace.js";

/** Shown in parse errors; keep in sync with web-ui README / runbook examples. */
const ANX_WORKSPACES_JSON_EXAMPLE =
  '[{"slug":"local","label":"Local","coreBaseUrl":"http://127.0.0.1:8000"}]';

/** `..."http://..." ]` instead of `..."http://..."} ]` — common .env / copy-paste mistake. */
function hintOarWorkspacesBracketTypo(trimmed) {
  if (!/"http[s]?:\/\/[^"]+"\s*\]/.test(trimmed)) {
    return "";
  }
  return ' Hint: close the workspace object with "}" before the final "]" (e.g. ..."8000"}] not ..."8000"]).';
}

function formatOarWorkspacesJsonError(trimmed, error) {
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
    ? hintOarWorkspacesBracketTypo(trimmed)
    : "";
  return `ANX_WORKSPACES must be valid JSON. ${reason}.${context}${typoHint} Unset ANX_WORKSPACES (and ANX_PROJECTS) to fall back to ANX_CORE_BASE_URL, or fix the value. Example: ${ANX_WORKSPACES_JSON_EXAMPLE}`;
}

/**
 * Fixes two copy/paste typos seen in ANX_WORKSPACES without changing valid JSON.
 * Only applied after JSON.parse fails.
 */
function repairCommonOarWorkspacesJsonTypos(trimmed) {
  let s = trimmed;
  // `"...url"]}` → `"...url"}]`
  const afterSwap = s.replace(/("http[s]?:\/\/[^"]+")\s*\]\s*\}\s*$/, "$1}]");
  if (afterSwap !== s) return afterSwap;
  // `"...url"]` at end → `"...url"}]`
  return s.replace(/("http[s]?:\/\/[^"]+")\s*\]\s*$/, "$1}]");
}

function normalizeWorkspaceEntry(entry, index) {
  if (!entry || typeof entry !== "object") {
    throw new Error(`ANX_WORKSPACES entry ${index + 1} must be an object.`);
  }

  const slug = normalizeWorkspaceSlug(entry.slug);
  if (!slug) {
    throw new Error(
      `ANX_WORKSPACES entry ${index + 1} is missing a valid slug.`,
    );
  }

  return {
    slug,
    label: String(entry.label ?? slug).trim() || slug,
    description: String(entry.description ?? "").trim(),
    coreBaseUrl: normalizeBaseUrl(entry.coreBaseUrl ?? entry.core_base_url),
    publicOrigin: normalizeBaseUrl(entry.publicOrigin ?? entry.public_origin),
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
  } catch (firstError) {
    const repaired = repairCommonOarWorkspacesJsonTypos(trimmed);
    if (repaired !== trimmed) {
      try {
        parsed = JSON.parse(repaired);
      } catch {
        throw new Error(formatOarWorkspacesJsonError(trimmed, firstError));
      }
    } else {
      throw new Error(formatOarWorkspacesJsonError(trimmed, firstError));
    }
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
      coreBaseUrl: normalizeBaseUrl(env.ANX_CORE_BASE_URL),
    },
  ];
}

export function createWorkspaceCatalog({
  workspaces,
  defaultWorkspaceSlug = "",
  devActorMode = false,
  usesSyntheticDefaultWorkspace = false,
  hostedDevAllowEmpty = false,
}) {
  if (hostedDevAllowEmpty && workspaces.length === 0) {
    return {
      defaultWorkspace: null,
      workspaces: [],
      workspaceBySlug: new Map(),
      devActorMode,
      usesSyntheticDefaultWorkspace: false,
      hostedDevEmpty: true,
    };
  }

  const defaultCandidate = normalizeWorkspaceSlug(
    defaultWorkspaceSlug || workspaces[0]?.slug || DEFAULT_WORKSPACE_SLUG,
  );
  const defaultWorkspace =
    workspaces.find((workspace) => workspace.slug === defaultCandidate) ??
    workspaces[0];

  if (!defaultWorkspace) {
    throw new Error("At least one ANX workspace must be configured.");
  }

  return {
    defaultWorkspace,
    workspaces,
    workspaceBySlug: new Map(
      workspaces.map((workspace) => [workspace.slug, workspace]),
    ),
    devActorMode,
    usesSyntheticDefaultWorkspace,
    hostedDevEmpty: false,
  };
}

export function loadWorkspaceCatalog(env = privateEnv) {
  const resolved = resolveWorkspaceEnv(env);
  const configuredWorkspaces = parseWorkspaceEntries(resolved.ANX_WORKSPACES);
  const preconfiguredWorkspaces = parseWorkspaceEntries(
    env.ANX_SAAS_DEV_PRECONFIGURED_WORKSPACES,
  );
  const saasPackedHost = isSaasPackedHostDev(env);

  let workspaces;
  let usesSyntheticDefaultWorkspace = false;
  if (configuredWorkspaces.length > 0) {
    workspaces = configuredWorkspaces;
    usesSyntheticDefaultWorkspace = false;
  } else if (preconfiguredWorkspaces.length > 0) {
    workspaces = preconfiguredWorkspaces;
    usesSyntheticDefaultWorkspace = false;
  } else if (saasPackedHost) {
    workspaces = [];
    usesSyntheticDefaultWorkspace = false;
  } else {
    workspaces = fallbackSingleWorkspace(env);
    usesSyntheticDefaultWorkspace = true;
  }
  const devActorMode =
    env.ANX_DEV_ACTOR_MODE === "true" || env.ANX_DEV_ACTOR_MODE === "1";

  return createWorkspaceCatalog({
    workspaces,
    defaultWorkspaceSlug: resolved.ANX_DEFAULT_WORKSPACE,
    devActorMode,
    usesSyntheticDefaultWorkspace,
    hostedDevAllowEmpty: saasPackedHost && workspaces.length === 0,
  });
}

export function getWorkspaceBySlug(workspaceSlug, env = privateEnv) {
  const catalog = loadWorkspaceCatalog(env);
  return (
    catalog.workspaceBySlug.get(normalizeWorkspaceSlug(workspaceSlug)) ?? null
  );
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
    defaultWorkspace: catalog.defaultWorkspace.slug,
    workspaces: catalog.workspaces.map((workspace) => ({
      slug: workspace.slug,
      label: workspace.label,
      description: workspace.description,
    })),
    devActorMode: catalog.devActorMode ?? false,
  };
}
