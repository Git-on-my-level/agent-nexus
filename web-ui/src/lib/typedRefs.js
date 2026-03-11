export const KNOWN_REF_PREFIXES = new Set([
  "artifact",
  "snapshot",
  "event",
  "thread",
  "url",
  "inbox",
  "document",
  "document_revision",
]);

export function parseRef(rawRef) {
  const input = String(rawRef ?? "");
  const separatorIndex = input.indexOf(":");

  if (separatorIndex === -1) {
    return { prefix: "", value: input };
  }

  return {
    prefix: input.slice(0, separatorIndex),
    value: input.slice(separatorIndex + 1),
  };
}

export function renderRef(ref) {
  if (typeof ref === "string") {
    return ref;
  }

  const prefix = String(ref?.prefix ?? "");
  const value = String(ref?.value ?? "");

  if (!prefix) {
    return value;
  }

  return `${prefix}:${value}`;
}

export function isKnownRefPrefix(prefix) {
  return KNOWN_REF_PREFIXES.has(prefix);
}
