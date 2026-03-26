export function normalizeAppPath(pathname = "/") {
  const raw = String(pathname ?? "").trim() || "/";
  const normalized = raw.startsWith("/") ? raw : `/${raw}`;
  if (normalized.length > 1) {
    return normalized.replace(/\/+$/, "") || "/";
  }

  return normalized;
}

export function normalizeBasePath(pathname = "") {
  const normalized = normalizeAppPath(pathname);
  return normalized === "/" ? "" : normalized;
}
