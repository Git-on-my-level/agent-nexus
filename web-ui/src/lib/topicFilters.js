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
 * Thread / topic list page URL: `open` (open-only, client-side), `state`, and `q`
 * (substring search; sent as GET /topics and GET /threads `q`).
 */
export function parseTopicListSearchParams(searchParams) {
  const sp =
    searchParams instanceof URLSearchParams
      ? searchParams
      : new URLSearchParams(searchParams);

  const openOnly = sp.get("open") === "1";
  let state = String(sp.get("state") ?? "").trim();
  const q = String(sp.get("q") ?? "").trim();

  if (openOnly) {
    state = "";
  }

  if (state && !TOPIC_STATUSES.includes(state)) {
    state = "";
  }

  return { state, q, openOnly };
}

/** Serialize list filters for `/topics` and `/threads` URL query strings. */
export function buildTopicListSearchString(state = {}) {
  const params = new URLSearchParams();

  if (state.openOnly) {
    params.set("open", "1");
  }
  if (!state.openOnly && state.state) {
    params.set("state", state.state);
  }
  const q = String(state.q ?? "").trim();
  if (q) {
    params.set("q", q);
  }

  return params.toString();
}

/** Query object for GET /topics (`listTopics`); only OpenAPI list parameters. */
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
  if (!state.openOnly && state.state) {
    const st = String(state.state ?? "").trim();
    if (TOPIC_STATUSES.includes(st)) {
      query.state = st;
    }
  }
  return query;
}

/**
 * Query for GET /threads (`listThreads`). Omits `state` when `openOnly` (active rows
 * filtered client-side). Passes `q` when set.
 */
export function buildThreadFilterQueryParamsFromThreadListState(state = {}) {
  const base = buildThreadFilterQueryParams({
    state: state.openOnly ? "" : state.state,
  });
  const q = String(state.q ?? "").trim();
  if (q) {
    base.q = q;
  }
  return base;
}

function isNonActiveLifecycleRow(row) {
  const s = String(row?.state ?? "").trim();
  if (!s) {
    return false;
  }
  return s !== "active";
}

/**
 * Client filters for backing-thread list rows (lifecycle only).
 */
export function applyBackingThreadListClientFilters(threads, state = {}) {
  let list = threads ?? [];
  if (state.openOnly) {
    list = list.filter((t) => !isNonActiveLifecycleRow(t));
  }
  return list;
}

/** Active / non-closed filter for thread rows (client-side, when `open=1` in the URL). */
export function applyThreadListClientFilters(threads, state = {}) {
  let list = threads ?? [];
  if (state.openOnly) {
    list = list.filter((t) => !isNonActiveLifecycleRow(t));
  }
  return list;
}

/**
 * Client-only filter for topic list when `open=1` is set (server `state` omitted in that case).
 * Search is sent as `q` to the API.
 */
export function applyTopicListClientFilters(items, state = {}) {
  return applyBackingThreadListClientFilters(items, state);
}
