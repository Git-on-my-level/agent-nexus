/** Lifecycle filter values for GET /topics and GET /threads (`state` query param). */
export const TOPIC_STATUSES = ["active", "archived", "trashed"];

/** Query string for GET /threads — `state` and optional `q`. */
export function buildThreadFilterQueryString(filters = {}) {
  const params = new URLSearchParams();

  if (filters.state) {
    params.set("state", filters.state);
  }
  const q = String(filters.q ?? "").trim();
  if (q) {
    params.set("q", q);
  }

  return params.toString();
}

/** Query object for GET /threads / listThreads. */
export function buildThreadFilterQueryParams(filters = {}) {
  const query = {};

  if (filters.state) {
    query.state = filters.state;
  }
  const q = String(filters.q ?? "").trim();
  if (q) {
    query.q = q;
  }

  return query;
}

/**
 * Thread / topic list URL: `state`, optional `q`, and legacy `open=1` (→ state active).
 */
export function parseTopicListSearchParams(searchParams) {
  const sp =
    searchParams instanceof URLSearchParams
      ? searchParams
      : new URLSearchParams(searchParams);

  const legacyOpen = sp.get("open") === "1";
  let state = String(sp.get("state") ?? "").trim();
  const q = String(sp.get("q") ?? "").trim();

  if (legacyOpen) {
    state = "active";
  }

  if (state && !TOPIC_STATUSES.includes(state)) {
    state = "";
  }

  if (!state) {
    state = "active";
  }

  return { state, q };
}

/** Serialize list filters for `/topics` and `/threads` URL query strings. */
export function buildTopicListSearchString(state = {}) {
  const params = new URLSearchParams();

  let st = String(state.state ?? "").trim();
  if (!TOPIC_STATUSES.includes(st)) {
    st = "active";
  }
  if (st !== "active") {
    params.set("state", st);
  }
  const q = String(state.q ?? "").trim();
  if (q) {
    params.set("q", q);
  }

  return params.toString();
}

/**
 * Query object for GET /topics (`listTopics`); only OpenAPI list parameters.
 * When `includeArchived` is true and lifecycle is `active`, `state` is omitted so the
 * server applies defaults that honor `include_archived` (explicit `state=active` would not).
 */
export function buildTopicListApiQueryParams(
  state = {},
  { includeArchived = false } = {},
) {
  const query = {};
  if (includeArchived) {
    query.include_archived = "true";
  }
  const q = String(state.q ?? "").trim();
  if (q) {
    query.q = q;
  }
  let st = String(state.state ?? "").trim();
  if (!TOPIC_STATUSES.includes(st)) {
    st = "active";
  }
  if (!(includeArchived && st === "active")) {
    query.state = st;
  }
  return query;
}

/**
 * Query for GET /threads (`listThreads`).
 */
export function buildThreadFilterQueryParamsFromThreadListState(state = {}) {
  let st = String(state.state ?? "").trim();
  if (!TOPIC_STATUSES.includes(st)) {
    st = "active";
  }
  const base = buildThreadFilterQueryParams({
    state: st,
  });
  const q = String(state.q ?? "").trim();
  if (q) {
    base.q = q;
  }
  return base;
}
