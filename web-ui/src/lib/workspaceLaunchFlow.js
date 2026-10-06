import {
  normalizeOrganizationSlug,
  normalizeWorkspaceSlug,
  workspacePath,
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

/**
 * Characters a browser strips or rewrites wherever they appear, so they can
 * hide the rest of a value — and, in a `Location` header, split the response.
 * Rejected across the whole value.
 */
function containsControlCharacter(value) {
  for (let i = 0; i < value.length; i += 1) {
    const code = value.charCodeAt(i);
    if (code < 0x20 || code === 0x7f) {
      return true;
    }
  }
  return false;
}

/**
 * A backslash is not a separator in a URL string, but a browser normalises it
 * to `/` when it resolves the location, so a path that looks confined to one
 * prefix can walk out of it. Only the path matters: a backslash after `?` stays
 * query text.
 */
function containsBackslash(value) {
  return value.includes("\\");
}

/**
 * `%2f` and `%5c` pass every textual check and become separators in whatever
 * decodes them next. One regex over the raw value would miss anything that
 * only spells the escape after a round of decoding (`%252%66`), so this walks
 * the same decoding rounds {@link decodeForPathSegmentCheck} does.
 */
function encodesSeparator(value) {
  let current = value;
  for (let round = 0; round < 3; round += 1) {
    if (/%(?:2f|5c)/i.test(current)) {
      return true;
    }
    let decoded;
    try {
      decoded = decodeURIComponent(current);
    } catch {
      // This round cannot be decoded, so nothing downstream can decode it
      // either: what we just checked is the final form. A legitimate encoded
      // `%` (`/docs/100%25`) lands here, and must pass.
      return false;
    }
    if (decoded === current) {
      return false;
    }
    current = decoded;
  }
  // Still changing after three rounds. Nothing legitimate nests that deep, and
  // we can no longer say what the last decoder would see.
  return true;
}

/** Percent escapes in the path must be well formed. */
function hasMalformedPercentEscape(value) {
  for (let i = 0; i < value.length; i += 1) {
    if (value[i] !== "%") {
      continue;
    }
    if (!/^[0-9a-fA-F]{2}$/.test(value.slice(i + 1, i + 3))) {
      return true;
    }
    i += 2;
  }
  return false;
}

/**
 * Reduce a caller-supplied return path to something that can only ever address
 * a page inside this app. Anything else falls back, rather than throwing,
 * because every caller has a sensible default to go to instead.
 *
 * The separator rules apply to the path. A query may carry whatever it likes —
 * including the percent-encoded URLs an OAuth `redirect_uri` is made of, or a
 * slash a viewer typed into a search box — because nothing after `?` can become
 * part of the path a browser resolves.
 *
 * The path rules match what a deployment's own launch validator is likely to
 * enforce, but a deployment may be stricter still (one is known to reject an
 * encoded separator anywhere in the value, query included), so a return path
 * that survives here can still be refused further on.
 */
export function sanitizeReturnPath(value, fallback = "/") {
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
  if (containsControlCharacter(candidate) || candidate.includes("#")) {
    return normalizedFallback;
  }

  const pathname = candidate.split("?", 1)[0];
  if (!pathname.startsWith("/") || /^\/[/\\]/.test(pathname)) {
    return normalizedFallback;
  }
  if (
    containsBackslash(pathname) ||
    encodesSeparator(pathname) ||
    hasMalformedPercentEscape(pathname)
  ) {
    return normalizedFallback;
  }

  const decodedPath = decodeForPathSegmentCheck(pathname);
  if (
    containsBackslash(decodedPath) ||
    containsControlCharacter(decodedPath) ||
    !decodedPath.startsWith("/") ||
    /^\/[/\\]/.test(decodedPath)
  ) {
    return normalizedFallback;
  }
  if (containsDotSegmentEscape(candidate)) {
    return normalizedFallback;
  }
  return candidate;
}

/**
 * Where inside a workspace a caller-supplied return path may send the viewer.
 *
 * {@link sanitizeReturnPath} rejects the forms that carry a separator, but the
 * value is caller-influenced and a browser renormalizes whatever it is handed —
 * a backslash becomes `/`, a tab disappears — so the composed target is
 * re-resolved the way the browser will resolve it and required to be
 * same-origin and inside this workspace. Returning the normalized form makes
 * the browser's own resolution a fixed point.
 *
 * Every redirect that joins a workspace to a return path goes through here, so
 * the guarantee does not depend on the sanitizer alone.
 *
 * @param {{ origin: string, organizationSlug: string, workspaceSlug: string, returnPath?: string }} args
 * @returns {string} an app-absolute path inside this workspace
 */
export function confineWorkspaceReturnPath({
  origin,
  organizationSlug,
  workspaceSlug,
  returnPath,
}) {
  const prefix = workspacePath(organizationSlug, workspaceSlug);
  // The base only has to be *an* origin: a relative target resolves against it,
  // and an absolute or protocol-relative one resolves away from it, which is
  // what the comparison below is looking for. Callers that have the request's
  // own origin should pass it; the check is the same either way.
  const base = String(origin ?? "").trim() || "https://workspace.invalid";
  try {
    const target = workspacePath(
      organizationSlug,
      workspaceSlug,
      sanitizeReturnPath(returnPath),
    );
    const resolved = new URL(target, base);
    if (resolved.origin !== new URL(base).origin) {
      return prefix;
    }
    if (
      resolved.pathname !== prefix &&
      !resolved.pathname.startsWith(`${prefix}/`)
    ) {
      return prefix;
    }
    return `${resolved.pathname}${resolved.search}`;
  } catch {
    // An origin we cannot parse leaves nothing to check the target against.
    return prefix;
  }
}

export function readLaunchParams(searchParams) {
  const params =
    searchParams instanceof URLSearchParams
      ? searchParams
      : new URLSearchParams(searchParams ?? "");
  const organizationSlug = normalizeOrganizationSlug(
    params.get("organization") ?? params.get("org"),
  );
  const workspaceSlug = normalizeWorkspaceSlug(params.get("workspace"));
  const workspaceId = String(params.get("workspace_id") ?? "").trim();
  const returnPath = sanitizeReturnPath(
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

export function buildSignInPath({
  organizationSlug,
  workspaceSlug,
  workspaceId,
  returnPath = "/",
  targetPath = "/",
}) {
  const params = new URLSearchParams();
  const normalizedOrganizationSlug =
    normalizeOrganizationSlug(organizationSlug);
  const normalizedWorkspaceSlug = normalizeWorkspaceSlug(workspaceSlug);
  const normalizedWorkspaceID = String(workspaceId ?? "").trim();
  const sanitizedReturnPath = sanitizeReturnPath(returnPath);

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

  const normalizedTargetPath = String(targetPath ?? "").trim() || "/";
  return params.size > 0
    ? `${normalizedTargetPath}?${params.toString()}`
    : normalizedTargetPath;
}
