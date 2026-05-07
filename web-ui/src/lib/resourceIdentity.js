import { parseRef } from "./typedRefs.js";

function asText(value) {
  return String(value ?? "").trim();
}

const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function isInternalUuid(value) {
  return UUID_RE.test(asText(value));
}

export function resourceHandle(resource) {
  return asText(resource?.handle);
}

export function resourceRef(resource, kind = "") {
  const ref = asText(resource?.ref);
  if (!kind || ref.startsWith(`${kind}:`)) return ref;
  return "";
}

export function refValue(refValue, expectedKind = "") {
  const parsed = parseRef(refValue);
  const prefix = asText(parsed.prefix);
  const value = asText(parsed.value);
  if (!value) return "";
  if (expectedKind && prefix && prefix !== expectedKind) return "";
  return value;
}

export function resourceRouteSegment(resource, kind = "") {
  const id = asText(resource?.id);
  return (
    resourceHandle(resource) ||
    refValue(resourceRef(resource, kind), kind) ||
    (id && !UUID_RE.test(id) ? id : "")
  );
}

export function revisionRouteSegment(revision, kind = "") {
  const rid = asText(revision?.revision_id);
  return (
    resourceHandle(revision) ||
    refValue(resourceRef(revision, kind), kind) ||
    (rid && !UUID_RE.test(rid) ? rid : "")
  );
}

export function typedResourceRef(kind, resource) {
  const ref = resourceRef(resource, kind);
  if (ref) return ref;
  const handle = resourceHandle(resource);
  if (kind && handle) return `${kind}:${handle}`;
  return "";
}

export function resourceCopyValue(kind, resource) {
  return typedResourceRef(kind, resource);
}

export function resourceDisplayLabel(resource, fallback = "") {
  const fallbackText = asText(fallback);
  const id = asText(resource?.id);
  return (
    asText(resource?.title) ||
    asText(resource?.summary) ||
    resourceHandle(resource) ||
    asText(resource?.ref) ||
    (fallbackText && !UUID_RE.test(fallbackText) ? fallbackText : "") ||
    (id && !UUID_RE.test(id) ? id : "") ||
    "Untitled resource"
  );
}
