import { HOME_FEED_PRESET } from "$lib/events/eventRows";
import taxonomy from "$lib/generated/taxonomy.json";
import {
  buildSearchString,
  readEnumSearchParam,
  readStringSearchParam,
} from "$lib/urlState";

const FALLBACK_EVENT_GROUP_ORDER = Object.freeze([
  "messages",
  "topics",
  "documents",
  "boards",
  "cards",
  "attention",
  "notifications",
  "reviews",
  "exceptions",
]);

const FALLBACK_BACKING_SCOPES = Object.freeze([
  "all",
  "standalone",
  "backing_only",
]);

/** @type {readonly string[]} */
export const EVENT_GROUP_ORDER =
  (taxonomy?.enums?.event_group?.values?.length ?? 0) > 0
    ? taxonomy.enums.event_group.values
    : FALLBACK_EVENT_GROUP_ORDER;

/** @type {readonly string[]} */
export const EVENT_BACKING_SCOPE_VALUES =
  (taxonomy?.enums?.backing_scope?.values?.length ?? 0) > 0
    ? taxonomy.enums.backing_scope.values
    : FALLBACK_BACKING_SCOPES;

export const DEFAULT_EVENT_LIST_FILTERS = Object.freeze({
  preset: "",
  type: "",
  event_group: [],
  backing_scope: "all",
  topic_id: "",
  actor_id: "",
  q: "",
  since: "",
  until: "",
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

const allowedEventGroups = new Set(EVENT_GROUP_ORDER);

/** @param {unknown} raw */
export function normalizeEventListGroups(raw) {
  const items = (Array.isArray(raw) ? raw : [])
    .map((s) => String(s ?? "").trim())
    .filter(Boolean);
  const seen = new Set();
  /** @type {string[]} */
  const out = [];
  for (const v of items) {
    if (!allowedEventGroups.has(v) || seen.has(v)) continue;
    seen.add(v);
    out.push(v);
  }
  return EVENT_GROUP_ORDER.filter((v) => out.includes(v));
}

/** @param {URLSearchParams | string[][] | Record<string,string>} searchParams */
export function parseEventListSearchParams(searchParams) {
  const sp =
    searchParams instanceof URLSearchParams
      ? searchParams
      : new URLSearchParams(searchParams);
  const presetRaw = readStringSearchParam(sp, "preset");
  const preset = presetRaw === HOME_FEED_PRESET ? HOME_FEED_PRESET : "";
  return {
    preset,
    type: readStringSearchParam(sp, "type"),
    event_group: normalizeEventListGroups(sp.getAll("event_group")),
    backing_scope: readEnumSearchParam(
      sp,
      "backing_scope",
      [...EVENT_BACKING_SCOPE_VALUES],
      "all",
    ),
    topic_id: readStringSearchParam(sp, "topic_id"),
    actor_id: readStringSearchParam(sp, "actor_id"),
    q: readStringSearchParam(sp, "q"),
    since: normalizeTimestampValue(readStringSearchParam(sp, "since")),
    until: normalizeTimestampValue(readStringSearchParam(sp, "until")),
  };
}

export function buildEventListSearchString(filters = {}) {
  const f = { ...DEFAULT_EVENT_LIST_FILTERS, ...filters };
  /** @type {Record<string, string | string[]>} */
  const entries = {
    preset:
      String(f.preset ?? "").trim() === HOME_FEED_PRESET
        ? HOME_FEED_PRESET
        : "",
    type: String(f.type ?? "").trim(),
    q: String(f.q ?? "").trim(),
    topic_id: String(f.topic_id ?? "").trim(),
    actor_id: String(f.actor_id ?? "").trim(),
    since: normalizeTimestampValue(f.since),
    until: normalizeTimestampValue(f.until),
  };
  const bs = String(f.backing_scope ?? "all").trim();
  if (bs && bs !== "all") entries.backing_scope = bs;
  const groups = normalizeEventListGroups(f.event_group);
  if (groups.length > 0) entries.event_group = groups;
  return buildSearchString(entries);
}

export function hasEventListFilters(filters = {}) {
  const f = { ...DEFAULT_EVENT_LIST_FILTERS, ...filters };
  if (String(f.preset ?? "").trim() === HOME_FEED_PRESET) return true;
  return Boolean(
    String(f.type ?? "").trim() ||
    String(f.q ?? "").trim() ||
    String(f.topic_id ?? "").trim() ||
    String(f.actor_id ?? "").trim() ||
    String(f.since ?? "").trim() ||
    String(f.until ?? "").trim() ||
    String(f.backing_scope ?? "all").trim() !== "all" ||
    normalizeEventListGroups(f.event_group).length > 0,
  );
}

/**
 * @param {Record<string, unknown>} filters
 * @param {{ cursor?: string; limit?: number }} [opts]
 */
export function buildEventListApiQuery(
  filters = {},
  { cursor = "", limit = 50 } = {},
) {
  const f = { ...DEFAULT_EVENT_LIST_FILTERS, ...filters };
  /** @type {Record<string, unknown>} */
  const query = {};
  const preset = String(f.preset ?? "").trim();
  if (preset) query.preset = preset;
  const type = String(f.type ?? "").trim();
  if (type) query.type = type;
  const groups = normalizeEventListGroups(f.event_group);
  if (groups.length > 0) query.event_group = groups;
  const bs = String(f.backing_scope ?? "all").trim();
  if (bs && bs !== "all") query.backing_scope = bs;
  for (const key of ["topic_id", "actor_id", "q"]) {
    const text = String(f[key] ?? "").trim();
    if (text) query[key] = text;
  }
  const since = normalizeTimestampValue(f.since);
  const until = normalizeTimestampValue(f.until);
  if (since) query.since = since;
  if (until) query.until = until;
  const c = String(cursor ?? "").trim();
  if (c) query.cursor = c;
  query.limit = limit;
  return query;
}
