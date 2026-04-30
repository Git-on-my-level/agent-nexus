import { KIND_LABELS } from "$lib/artifactKinds";
import {
  buildSearchString,
  readEnumSearchParam,
  readStringSearchParam,
} from "$lib/urlState";

export const ARTIFACT_STATE_VALUES = Object.freeze([
  "active",
  "archived",
  "trashed",
]);

export const DEFAULT_ARTIFACT_LIST_FILTERS = Object.freeze({
  kind: "",
  backing_scope: "all",
  thread_id: "",
  created_after: "",
  created_before: "",
  states: ["active"],
});

export const ARTIFACT_BACKING_SCOPE_VALUES = Object.freeze([
  "all",
  "standalone",
  "backing_only",
]);

function normalizeTimestampValue(value) {
  const raw = String(value ?? "").trim();
  if (!raw) {
    return "";
  }

  const parsed = Date.parse(raw);
  if (Number.isNaN(parsed)) {
    return "";
  }

  return new Date(parsed).toISOString();
}

function padDatePart(value) {
  return String(value).padStart(2, "0");
}

export function formatArtifactTimestampInputValue(value) {
  const normalized = normalizeTimestampValue(value);
  if (!normalized) {
    return "";
  }

  const date = new Date(normalized);
  const year = date.getFullYear();
  const month = padDatePart(date.getMonth() + 1);
  const day = padDatePart(date.getDate());
  const hours = padDatePart(date.getHours());
  const minutes = padDatePart(date.getMinutes());

  return `${year}-${month}-${day}T${hours}:${minutes}`;
}

/** @param {string[]} states */
export function normalizeArtifactLifecycleStates(states) {
  const raw = Array.isArray(states) ? states : [];
  const seen = new Set(
    raw
      .map((s) => String(s ?? "").trim())
      .filter((s) => ARTIFACT_STATE_VALUES.includes(s)),
  );
  const out = [];
  for (const canon of ARTIFACT_STATE_VALUES) {
    if (seen.has(canon)) {
      out.push(canon);
    }
  }
  return out.length > 0 ? out : ["active"];
}

/** @param {URLSearchParams | string[][] | Record<string,string>} searchParams */
function parseLifecycleStates(searchParams) {
  const sp =
    searchParams instanceof URLSearchParams
      ? searchParams
      : new URLSearchParams(searchParams);
  const raw = sp
    .getAll("state")
    .map((s) => String(s ?? "").trim())
    .filter(Boolean);
  return normalizeArtifactLifecycleStates(raw);
}

export function parseArtifactListSearchParams(searchParams) {
  return {
    kind: readEnumSearchParam(
      searchParams,
      "kind",
      Object.keys(KIND_LABELS),
      "",
    ),
    backing_scope: readEnumSearchParam(
      searchParams,
      "backing_scope",
      ARTIFACT_BACKING_SCOPE_VALUES,
      "all",
    ),
    thread_id: readStringSearchParam(searchParams, "thread_id"),
    created_after: normalizeTimestampValue(
      readStringSearchParam(searchParams, "created_after"),
    ),
    created_before: normalizeTimestampValue(
      readStringSearchParam(searchParams, "created_before"),
    ),
    states: parseLifecycleStates(searchParams),
  };
}

export function buildArtifactListSearchString(filters = {}) {
  const states = normalizeArtifactLifecycleStates(filters.states ?? ["active"]);

  /** @type {Record<string, string | string[]>} */
  const entries = {
    kind: String(filters.kind ?? "").trim(),
    backing_scope:
      String(filters.backing_scope ?? "all").trim() === "all"
        ? ""
        : String(filters.backing_scope ?? "").trim(),
    thread_id: String(filters.thread_id ?? "").trim(),
    created_after: normalizeTimestampValue(filters.created_after),
    created_before: normalizeTimestampValue(filters.created_before),
  };
  const defaultStates = states.length === 1 && String(states[0]) === "active";
  if (!defaultStates) {
    entries.state = states;
  }
  return buildSearchString(entries);
}

export function buildArtifactListQuery(filters = {}) {
  const states = normalizeArtifactLifecycleStates(filters.states ?? ["active"]);

  /** @type {Record<string, string | string[]>} */
  const q = {};
  q.kind = String(filters.kind ?? "").trim();
  q.backing_scope = String(filters.backing_scope ?? "all").trim() || "all";
  q.thread_id = String(filters.thread_id ?? "").trim();
  const ca = normalizeTimestampValue(filters.created_after);
  const cb = normalizeTimestampValue(filters.created_before);
  if (ca) {
    q.created_after = ca;
  }
  if (cb) {
    q.created_before = cb;
  }
  q.state = states;
  return q;
}

export function hasArtifactListFilters(filters = {}) {
  const f = { ...DEFAULT_ARTIFACT_LIST_FILTERS, ...filters };
  return Boolean(
    String(f.kind ?? "").trim() ||
    String(f.backing_scope ?? "all").trim() !== "all" ||
    String(f.thread_id ?? "").trim() ||
    String(f.created_after ?? "").trim() ||
    String(f.created_before ?? "").trim() ||
    (() => {
      const st = normalizeArtifactLifecycleStates(f.states ?? ["active"]);
      return !(st.length === 1 && st[0] === "active");
    })(),
  );
}
