import {
  normalizeOrganizationSlug,
  normalizeWorkspaceSlug,
} from "$lib/workspacePaths.js";

function decodeForPathSegmentCheck(segment) {
  let current = String(segment ?? "");
  for (let i = 0; i < 3; i += 1) {
    let decoded;
    try {
      decoded = decodeURIComponent(current);
    } catch {
      return current;
    }
    if (decoded === current) {
      return decoded;
    }
    current = decoded;
  }
  return current;
}

function containsDotSegmentEscape(candidate) {
  const pathname = String(candidate ?? "").split(/[?#]/, 1)[0];
  const segments = pathname.split("/");
  return segments.some((segment) => {
    if (!segment) {
      return false;
    }
    const decoded = decodeForPathSegmentCheck(segment).trim();
    return decoded === "." || decoded === "..";
  });
}

export function sanitizeHostedReturnPath(value, fallback = "/") {
  const normalizedFallback =
    String(fallback ?? "")
      .trim()
      .startsWith("/") &&
    !String(fallback ?? "")
      .trim()
      .startsWith("//")
      ? String(fallback).trim()
      : "/";
  const candidate = String(value ?? "").trim();
  if (!candidate) {
    return normalizedFallback;
  }
  if (!candidate.startsWith("/") || candidate.startsWith("//")) {
    return normalizedFallback;
  }
  if (/[\r\n]/.test(candidate)) {
    return normalizedFallback;
  }
  if (containsDotSegmentEscape(candidate)) {
    return normalizedFallback;
  }
  return candidate;
}

export function readHostedLaunchParams(searchParams) {
  const params =
    searchParams instanceof URLSearchParams
      ? searchParams
      : new URLSearchParams(searchParams ?? "");
  const organizationSlug = normalizeOrganizationSlug(
    params.get("organization") ?? params.get("org"),
  );
  const workspaceSlug = normalizeWorkspaceSlug(params.get("workspace"));
  const workspaceId = String(params.get("workspace_id") ?? "").trim();
  const returnPath = sanitizeHostedReturnPath(
    params.get("return_path") ?? params.get("return_to") ?? "/",
  );

  return {
    organizationSlug,
    workspaceSlug,
    workspaceId,
    returnPath,
    hasContinuation: workspaceId !== "" || workspaceSlug !== "",
  };
}

export function buildHostedSignInPath({
  organizationSlug,
  workspaceSlug,
  workspaceId,
  returnPath = "/",
  targetPath = "/hosted/signin",
}) {
  const params = new URLSearchParams();
  const normalizedOrganizationSlug =
    normalizeOrganizationSlug(organizationSlug);
  const normalizedWorkspaceSlug = normalizeWorkspaceSlug(workspaceSlug);
  const normalizedWorkspaceID = String(workspaceId ?? "").trim();
  const sanitizedReturnPath = sanitizeHostedReturnPath(returnPath);

  if (normalizedOrganizationSlug) {
    params.set("organization", normalizedOrganizationSlug);
  }
  if (normalizedWorkspaceSlug) {
    params.set("workspace", normalizedWorkspaceSlug);
  }
  if (normalizedWorkspaceID) {
    params.set("workspace_id", normalizedWorkspaceID);
  }
  if (sanitizedReturnPath !== "/") {
    params.set("return_path", sanitizedReturnPath);
  }

  const normalizedTargetPath =
    String(targetPath ?? "").trim() || "/hosted/signin";
  return params.size > 0
    ? `${normalizedTargetPath}?${params.toString()}`
    : normalizedTargetPath;
}

export function normalizeHostedLaunchFinishURL(finishURL) {
  const normalized = String(finishURL ?? "").trim();
  if (!normalized) {
    return "";
  }
  if (/^https?:\/\//i.test(normalized)) {
    const currentOrigin =
      typeof window !== "undefined" ? window.location.origin : "";
    try {
      const parsed = new URL(normalized);
      if (currentOrigin && parsed.origin === currentOrigin) {
        return normalizeHostedLaunchFinishURL(
          `${parsed.pathname}${parsed.search}${parsed.hash}`,
        );
      }
    } catch {
      return "";
    }
    return "";
  }
  // CP currently returns relative finish_url paths; route them through the
  // same-origin hosted API proxy so browser navigation reaches control-plane.
  if (normalized.startsWith("/hosted/api/")) {
    return normalized;
  }
  if (normalized.startsWith("/")) {
    return `/hosted/api${normalized}`;
  }
  return `/hosted/api/${normalized.replace(/^\/+/, "")}`;
}
