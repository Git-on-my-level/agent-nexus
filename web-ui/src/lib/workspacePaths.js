import { base } from "$app/paths";
import { normalizeAppPath, normalizeBasePath } from "./pathUtils.js";

export const WORKSPACE_HEADER = "x-anx-workspace-slug";
export { normalizeAppPath, normalizeBasePath };

export function normalizeWorkspaceSlug(value) {
  return String(value ?? "")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-+|-+$/g, "");
}

/** Same normalization rules as workspace slugs; used for URL org segments. */
export function normalizeOrganizationSlug(value) {
  return normalizeWorkspaceSlug(value);
}

export const APP_BASE_PATH = normalizeBasePath(base);

export function appPath(pathname = "/", basePath = APP_BASE_PATH) {
  const normalizedPathname = normalizeAppPath(pathname);
  if (!basePath) {
    return normalizedPathname;
  }

  return normalizedPathname === "/"
    ? basePath
    : `${basePath}${normalizedPathname}`;
}

export function stripBasePath(pathname = "/", basePath = APP_BASE_PATH) {
  const normalizedPathname = normalizeAppPath(pathname);
  if (!basePath) {
    return normalizedPathname;
  }

  if (normalizedPathname === basePath) {
    return "/";
  }

  if (normalizedPathname.startsWith(`${basePath}/`)) {
    return normalizedPathname.slice(basePath.length);
  }

  return normalizedPathname;
}

const WORKSPACE_ROUTE_RE = /^\/o\/([^/]+)\/w\/([^/]+)(?:\/.*)?$/;

export function parseWorkspaceRouteSlugs(
  pathname = "/",
  basePath = APP_BASE_PATH,
) {
  const stripped = stripBasePath(pathname, basePath);
  const match = stripped.match(WORKSPACE_ROUTE_RE);
  if (!match) {
    return { organizationSlug: "", workspaceSlug: "" };
  }
  return {
    organizationSlug: String(match[1] ?? "").trim(),
    workspaceSlug: String(match[2] ?? "").trim(),
  };
}

/**
 * Build a path under `/o/{organizationSlug}/w/{workspaceSlug}`.
 * @param {string} organizationSlug
 * @param {string} workspaceSlug
 * @param {string} [pathname]
 * @param {string} [basePath]
 */
export function workspacePath(
  organizationSlug,
  workspaceSlug,
  pathname = "/",
  basePath = APP_BASE_PATH,
) {
  const org = normalizeOrganizationSlug(organizationSlug);
  const slug = normalizeWorkspaceSlug(workspaceSlug);
  if (!org) {
    throw new Error("organization slug is required");
  }
  if (!slug) {
    throw new Error("workspace slug is required");
  }

  const prefix = `/o/${org}/w/${slug}`;
  const normalizedPathname = normalizeAppPath(pathname);
  if (normalizedPathname === "/") {
    return appPath(prefix, basePath);
  }
  return appPath(`${prefix}${normalizedPathname}`, basePath);
}

export function workspaceCompositeKey(organizationSlug, workspaceSlug) {
  const org = normalizeOrganizationSlug(organizationSlug);
  const ws = normalizeWorkspaceSlug(workspaceSlug);
  if (!org || !ws) {
    return "";
  }
  return `${org}:${ws}`;
}

export function stripWorkspacePath(
  pathname,
  organizationSlug,
  workspaceSlug,
  basePath = APP_BASE_PATH,
) {
  const org = normalizeOrganizationSlug(organizationSlug);
  const slug = normalizeWorkspaceSlug(workspaceSlug);
  const normalizedPathname = stripBasePath(pathname, basePath);
  if (!org || !slug) {
    return normalizedPathname;
  }

  const prefix = `/o/${org}/w/${slug}`;
  if (normalizedPathname === prefix) {
    return "/";
  }

  if (normalizedPathname.startsWith(`${prefix}/`)) {
    return normalizedPathname.slice(prefix.length);
  }

  return normalizedPathname;
}

export function buildWorkspaceStorageKey(baseKey, workspaceSlug) {
  const slug = normalizeWorkspaceSlug(workspaceSlug);
  if (!slug) {
    throw new Error("workspace slug is required for storage key");
  }
  return `${baseKey}:${slug}`;
}
