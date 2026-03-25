import { validateEventRefRule } from "./eventRefRules.js";

function typedRefPrefix(refValue) {
  const raw = String(refValue ?? "").trim();
  const separatorIndex = raw.indexOf(":");

  if (separatorIndex <= 0 || separatorIndex >= raw.length - 1) {
    return "";
  }

  return raw.slice(0, separatorIndex).trim();
}

function isObject(value) {
  return value && typeof value === "object" && !Array.isArray(value);
}

export function validateEventCreatePayload(body) {
  if (!isObject(body)) {
    return "request body must be a JSON object";
  }

  const actorId = String(body.actor_id ?? "").trim();
  if (!actorId) {
    return "actor_id is required";
  }

  const event = body.event;
  if (!isObject(event)) {
    return "event is required";
  }

  const type = String(event.type ?? "").trim();
  if (!type) {
    return "event.type is required";
  }

  if (typeof event.summary !== "string") {
    return "event.summary is required";
  }

  if (!Array.isArray(event.refs)) {
    return "event.refs must be a list of strings";
  }

  for (const ref of event.refs) {
    if (typeof ref !== "string") {
      return "event.refs must be a list of strings";
    }
    if (!typedRefPrefix(ref)) {
      return `event.refs contains invalid typed ref ${JSON.stringify(ref)}`;
    }
  }

  if (!isObject(event.provenance)) {
    return "event.provenance is required";
  }

  if (!Array.isArray(event.provenance.sources)) {
    return "event.provenance.sources must be a list of strings";
  }
  for (const source of event.provenance.sources) {
    if (typeof source !== "string") {
      return "event.provenance.sources must be a list of strings";
    }
  }

  if (event.thread_id !== undefined && String(event.thread_id).trim() === "") {
    return "event.thread_id must be non-empty when provided";
  }

  if (
    event.payload !== undefined &&
    event.payload !== null &&
    !isObject(event.payload)
  ) {
    return "event.payload must be an object";
  }

  const threadID = String(event.thread_id ?? "").trim();
  const payload = isObject(event.payload) ? event.payload : {};
  const ruleValidation = validateEventRefRule(type, event.refs, {
    ...payload,
    thread_id: threadID,
  });
  if (!ruleValidation.valid) {
    return ruleValidation.error;
  }

  return "";
}
