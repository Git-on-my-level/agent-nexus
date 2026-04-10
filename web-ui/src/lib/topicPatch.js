import { normalizeStringList, stringListsEqual } from "./listUtils.js";

const LIST_FIELDS = new Set(["tags"]);
const EDITABLE_FIELDS = [
  "title",
  "type",
  "status",
  "priority",
  "cadence",
  "next_check_in_at",
  "tags",
  "current_summary",
];

function normalizeScalar(key, value) {
  if (key === "next_check_in_at") {
    return value ? String(value) : null;
  }

  return value ?? "";
}

export function buildTopicPatch(original = {}, draft = {}) {
  const patch = {};

  for (const field of EDITABLE_FIELDS) {
    const originalValue = original[field];
    const draftValue = draft[field];

    if (LIST_FIELDS.has(field)) {
      const normalizedOriginal = normalizeStringList(originalValue);
      const normalizedDraft = normalizeStringList(draftValue);

      if (!stringListsEqual(normalizedOriginal, normalizedDraft)) {
        patch[field] = normalizedDraft;
      }
      continue;
    }

    const normalizedOriginal = normalizeScalar(field, originalValue);
    const normalizedDraft = normalizeScalar(field, draftValue);

    if (normalizedOriginal !== normalizedDraft) {
      patch[field] = normalizedDraft;
    }
  }

  return patch;
}

export function describeCron(expr) {
  const raw = String(expr ?? "").trim();
  if (!raw) return "";

  const parts = raw.split(/\s+/);
  if (parts.length !== 5) return "Custom schedule";

  const [minute, hour, dom, , dow] = parts;

  const isEveryField = (f) => f === "*";
  const isNumber = (f) => /^\d+$/.test(f);

  if (!isNumber(minute) || !isNumber(hour)) return "Custom schedule";

  const min = parseInt(minute, 10);
  const hr = parseInt(hour, 10);
  const timeStr = `${hr % 12 === 0 ? 12 : hr % 12}:${String(min).padStart(2, "0")} ${hr < 12 ? "AM" : "PM"}`;

  if (isEveryField(dom) && isEveryField(dow)) {
    return `Every day at ${timeStr}`;
  }

  if (isEveryField(dom) && isNumber(dow)) {
    const days = [
      "Sunday",
      "Monday",
      "Tuesday",
      "Wednesday",
      "Thursday",
      "Friday",
      "Saturday",
    ];
    const dayName = days[parseInt(dow, 10)];
    return dayName ? `Every ${dayName} at ${timeStr}` : "Custom schedule";
  }

  if (isNumber(dom) && isEveryField(dow)) {
    const d = parseInt(dom, 10);
    const suffix = d === 1 ? "st" : d === 2 ? "nd" : d === 3 ? "rd" : "th";
    return `${d}${suffix} of every month at ${timeStr}`;
  }

  return "Custom schedule";
}

export { parseListInput, serializeListInput } from "./typedRefs.js";
