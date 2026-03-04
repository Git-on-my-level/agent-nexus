export const THREAD_STATUSES = ["active", "paused", "closed"];
export const THREAD_PRIORITIES = ["p0", "p1", "p2", "p3"];
export const THREAD_CADENCES = [
  "reactive",
  "daily",
  "weekly",
  "monthly",
  "custom",
];
export const STALENESS_MODES = ["all", "stale", "fresh"];

export function parseTagFilterInput(rawValue) {
  return String(rawValue ?? "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

export function buildThreadFilterQuery(filters = {}) {
  const params = new URLSearchParams();

  if (filters.status) {
    params.set("status", filters.status);
  }

  if (filters.priority) {
    params.set("priority", filters.priority);
  }

  if (filters.cadence) {
    params.set("cadence", filters.cadence);
  }

  for (const tag of filters.tags ?? []) {
    params.append("tag", tag);
  }

  if (filters.staleness === "stale") {
    params.set("stale", "true");
  } else if (filters.staleness === "fresh") {
    params.set("stale", "false");
  }

  return params.toString();
}

export function buildThreadFilterRequestQuery(filters = {}) {
  const tags = filters.tags ?? [];
  const query = {};

  if (filters.status) {
    query.status = filters.status;
  }

  if (filters.priority) {
    query.priority = filters.priority;
  }

  if (filters.cadence) {
    query.cadence = filters.cadence;
  }

  if (tags.length > 0) {
    query.tag = tags;
  }

  if (filters.staleness === "stale") {
    query.stale = true;
  } else if (filters.staleness === "fresh") {
    query.stale = false;
  }

  return query;
}

export function computeStaleness(thread) {
  if (!thread?.next_check_in_at) {
    return {
      stale: false,
      label: "No check-in",
      className: "bg-slate-100 text-slate-700",
    };
  }

  const stale = Date.parse(String(thread.next_check_in_at)) < Date.now();
  return stale
    ? { stale: true, label: "Stale", className: "bg-rose-100 text-rose-700" }
    : {
        stale: false,
        label: "Fresh",
        className: "bg-emerald-100 text-emerald-700",
      };
}
