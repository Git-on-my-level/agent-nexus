import { base } from "$app/paths";

export const DEFAULT_WORKSPACE_SLUG = "local";
export const WORKSPACE_HEADER = "x-oar-workspace-slug";

export function normalizeWorkspaceSlug(value) {
  return String(value ?? "")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-+|-+$/g, "");
}

export function normalizeAppPath(pathname = "/") {
  const raw = String(pathname ?? "").trim() || "/";
  const normalized = raw.startsWith("/") ? raw : `/${raw}`;
  if (normalized.length > 1 && normalized.endsWith("/")) {
    return normalized.slice(0, -1);
  }

  return normalized;
}

export function normalizeBasePath(pathname = "") {
  const normalized = normalizeAppPath(pathname);
  return normalized === "/" ? "" : normalized;
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

export function workspacePath(
  workspaceSlug,
  pathname = "/",
  basePath = APP_BASE_PATH,
) {
  const slug = normalizeWorkspaceSlug(workspaceSlug);
  if (!slug) {
    throw new Error("workspace slug is required");
  }

  const normalizedPathname = normalizeAppPath(pathname);
  return normalizedPathname === "/"
    ? appPath(`/${slug}`, basePath)
    : appPath(`/${slug}${normalizedPathname}`, basePath);
}

export function stripWorkspacePath(
  pathname,
  workspaceSlug,
  basePath = APP_BASE_PATH,
) {
  const slug = normalizeWorkspaceSlug(workspaceSlug);
  const normalizedPathname = stripBasePath(pathname, basePath);
  if (!slug) {
    return normalizedPathname;
  }

  const prefix = `/${slug}`;
  if (normalizedPathname === prefix) {
    return "/";
  }

  if (normalizedPathname.startsWith(`${prefix}/`)) {
    return normalizedPathname.slice(prefix.length);
  }

  return normalizedPathname;
}

export function buildWorkspaceStorageKey(baseKey, workspaceSlug) {
  const slug = normalizeWorkspaceSlug(workspaceSlug) || DEFAULT_WORKSPACE_SLUG;
  return `${baseKey}:${slug}`;
}
