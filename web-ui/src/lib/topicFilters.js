/** Lifecycle filter values for GET /topics and GET /threads (`state` query params, OR semantics). */
export const TOPIC_STATUSES = ["active", "archived", "trashed"];

const DEFAULT_TOPIC_LIST_STATES = ["active"];

function normalizeLifecycleStates(raw) {
  if (Array.isArray(raw)) {
    const ordered = [];
    for (const st of TOPIC_STATUSES) {
      if (raw.some((x) => String(x ?? "").trim() === st)) {
        ordered.push(st);
      }
    }
    return ordered.length > 0 ? ordered : [...DEFAULT_TOPIC_LIST_STATES];
  }
  const one = String(raw ?? "").trim();
  if (one && TOPIC_STATUSES.includes(one)) {
    return [one];
  }
  return [...DEFAULT_TOPIC_LIST_STATES];
}

/** Query string for GET /threads — repeated `state` and optional `q`. */
export function buildThreadFilterQueryString(filters = {}) {
  const params = new URLSearchParams();
  const states = normalizeLifecycleStates(filters.states ?? filters.state);
  for (const s of states) {
    params.append("state", s);
  }
  const q = String(filters.q ?? "").trim();
  if (q) {
    params.set("q", q);
  }

  return params.toString();
}

/** Request query object for listThreads — `state[]` arrays serialize to repeated keys. */
export function buildThreadFilterQueryParams(filters = {}) {
  const states = normalizeLifecycleStates(filters.states ?? filters.state);
  const query = { state: states };
  const q = String(filters.q ?? "").trim();
  if (q) {
    query.q = q;
  }

  return query;
}

/**
 * Thread / topic list URL: repeated `state`, optional `q`, and legacy `open=1` (→ states [active]).
 */
export function parseTopicListSearchParams(searchParams) {
  const sp =
    searchParams instanceof URLSearchParams
      ? searchParams
      : new URLSearchParams(searchParams);

  const legacyOpen = sp.get("open") === "1";
  let states = [...sp.getAll("state")]
    .map((s) => String(s ?? "").trim())
    .filter(Boolean);

  const q = String(sp.get("q") ?? "").trim();

  if (legacyOpen) {
    states = ["active"];
  }

  states = normalizeLifecycleStates(states);

  return { states, q };
}

/** Serialize list filters for `/topics` and `/threads` URL query strings. */
export function buildTopicListSearchString(state = {}) {
  const params = new URLSearchParams();

  let states = normalizeLifecycleStates(state.states ?? state.state);
  if (states.length !== 1 || states[0] !== "active") {
    for (const s of states) {
      params.append("state", s);
    }
  }
  const q = String(state.q ?? "").trim();
  if (q) {
    params.set("q", q);
  }

  return params.toString();
}

/**
 * Query object for GET /topics (`listTopics`); passes `state` as string[] for repeated query keys.
 */
export function buildTopicListApiQueryParams(state = {}) {
  const states = normalizeLifecycleStates(state.states ?? state.state);
  const query = { state: states };
  const q = String(state.q ?? "").trim();
  if (q) {
    query.q = q;
  }
  return query;
}

/**
 * Query for GET /threads (`listThreads`).
 */
export function buildThreadFilterQueryParamsFromThreadListState(state = {}) {
  return buildThreadFilterQueryParams(state);
}
