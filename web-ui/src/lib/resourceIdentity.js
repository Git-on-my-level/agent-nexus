import { parseRef } from "./typedRefs.js";

function asText(value) {
  return String(value ?? "").trim();
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
  return (
    resourceHandle(resource) ||
    refValue(resourceRef(resource, kind), kind) ||
    asText(resource?.id)
  );
}

export function revisionRouteSegment(revision, kind = "") {
  return (
    resourceHandle(revision) ||
    refValue(resourceRef(revision, kind), kind) ||
    asText(revision?.revision_id)
  );
}

export function typedResourceRef(kind, resource) {
  const ref = resourceRef(resource, kind);
  if (ref) return ref;
  const handle = resourceHandle(resource);
  if (kind && handle) return `${kind}:${handle}`;
  const id = asText(resource?.id);
  if (kind && id) return `${kind}:${id}`;
  return "";
}

export function resourceCopyValue(kind, resource) {
  return (
    typedResourceRef(kind, resource) || resourceRouteSegment(resource, kind)
  );
}

export function resourceDisplayLabel(resource, fallback = "") {
  return (
    asText(resource?.title) ||
    asText(resource?.summary) ||
    resourceHandle(resource) ||
    asText(fallback) ||
    asText(resource?.ref) ||
    asText(resource?.id)
  );
}
