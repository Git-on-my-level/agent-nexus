export const KNOWN_REF_PREFIXES = new Set([
  "artifact",
  "card",
  "event",
  "thread",
  "topic",
  "url",
  "inbox",
  "document",
  "document_revision",
  "board",
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

export function parseListInput(rawValue) {
  return String(rawValue ?? "")
    .split(/\r?\n|,/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function serializeListInput(items) {
  if (!Array.isArray(items)) {
    return "";
  }
  return items
    .map((item) => String(item).trim())
    .filter(Boolean)
    .join("\n");
}

export function validateTypedRefs(refs = []) {
  const invalidRefs = [];
  refs.forEach((refValue) => {
    const parsed = parseRef(refValue);
    if (!parsed.prefix || !parsed.value) {
      invalidRefs.push(refValue);
    }
  });
  return {
    valid: invalidRefs.length === 0,
    invalidRefs,
  };
}
