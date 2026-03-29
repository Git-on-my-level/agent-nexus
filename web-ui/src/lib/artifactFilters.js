import { KIND_LABELS } from "$lib/artifactKinds";
import {
  buildSearchString,
  readEnumSearchParam,
  readStringSearchParam,
} from "$lib/urlState";

export const ARTIFACT_KIND_VALUES = Object.freeze(Object.keys(KIND_LABELS));

export const DEFAULT_ARTIFACT_LIST_FILTERS = Object.freeze({
  kind: "",
  thread_id: "",
  created_after: "",
  created_before: "",
});

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

export function parseArtifactListSearchParams(searchParams) {
  return {
    kind: readEnumSearchParam(searchParams, "kind", ARTIFACT_KIND_VALUES, ""),
    thread_id: readStringSearchParam(searchParams, "thread_id"),
    created_after: normalizeTimestampValue(
      readStringSearchParam(searchParams, "created_after"),
    ),
    created_before: normalizeTimestampValue(
      readStringSearchParam(searchParams, "created_before"),
    ),
  };
}

export function buildArtifactListSearchString(filters = {}) {
  return buildSearchString({
    kind: filters.kind,
    thread_id: filters.thread_id,
    created_after: normalizeTimestampValue(filters.created_after),
    created_before: normalizeTimestampValue(filters.created_before),
  });
}

export function buildArtifactListQuery(filters = {}) {
  return {
    kind: String(filters.kind ?? "").trim(),
    thread_id: String(filters.thread_id ?? "").trim(),
    created_after: normalizeTimestampValue(filters.created_after),
    created_before: normalizeTimestampValue(filters.created_before),
  };
}

export function hasArtifactListFilters(filters = {}) {
  return Boolean(
    String(filters.kind ?? "").trim() ||
    String(filters.thread_id ?? "").trim() ||
    String(filters.created_after ?? "").trim() ||
    String(filters.created_before ?? "").trim(),
  );
}
