export const THREAD_STATUSES = ["active", "paused", "closed"];
export const THREAD_PRIORITIES = ["p0", "p1", "p2", "p3"];
export const THREAD_PRIORITY_LABELS = {
  p0: "Critical (P0)",
  p1: "High (P1)",
  p2: "Medium (P2)",
  p3: "Low (P3)",
};

export function getPriorityLabel(priority) {
  return THREAD_PRIORITY_LABELS[priority] ?? priority;
}

export const THREAD_SCHEDULE_PRESETS = [
  "reactive",
  "daily",
  "weekly",
  "monthly",
  "custom",
];
export const THREAD_SCHEDULE_PRESET_LABELS = {
  reactive: "Reactive",
  daily: "Daily",
  weekly: "Weekly",
  monthly: "Monthly",
  custom: "Custom",
};
export const CADENCE_PRESET_TO_CRON = {
  daily: "0 9 * * *",
  weekly: "0 9 * * 1",
  monthly: "0 9 1 * *",
};
export const STALENESS_MODES = ["all", "stale", "fresh"];

export function parseTagFilterInput(rawValue) {
  return String(rawValue ?? "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

export function isLikelyCronExpression(value) {
  const raw = String(value ?? "").trim();
  if (!raw) {
    return false;
  }

  const parts = raw.split(/\s+/);
  if (parts.length !== 5) {
    return false;
  }

  return parts.every((part) => /^[A-Za-z0-9*/,_-]+$/.test(part));
}

function normalizeCadence(value) {
  return String(value ?? "").trim();
}

export function cadencePresetFromValue(value) {
  const cadence = normalizeCadence(value);

  if (!cadence || cadence === "reactive") {
    return "reactive";
  }

  if (cadence === "daily" || cadence === CADENCE_PRESET_TO_CRON.daily) {
    return "daily";
  }

  if (cadence === "weekly" || cadence === CADENCE_PRESET_TO_CRON.weekly) {
    return "weekly";
  }

  if (cadence === "monthly" || cadence === CADENCE_PRESET_TO_CRON.monthly) {
    return "monthly";
  }

  if (cadence === "custom" || isLikelyCronExpression(cadence)) {
    return "custom";
  }

  return "custom";
}

export function formatCadenceLabel(cadence, options = {}) {
  const includeExpression = options.includeExpression ?? true;
  const value = normalizeCadence(cadence);
  const preset = cadencePresetFromValue(value);

  if (preset !== "custom") {
    return THREAD_SCHEDULE_PRESET_LABELS[preset];
  }

  if (!value || value === "custom" || !includeExpression) {
    return THREAD_SCHEDULE_PRESET_LABELS.custom;
  }

  return `${THREAD_SCHEDULE_PRESET_LABELS.custom} (${value})`;
}

export function cadenceToRequestValue({
  preset,
  customCron,
  fallbackCadence = "",
} = {}) {
  if (preset === "reactive") {
    return "reactive";
  }

  if (preset === "daily") {
    return CADENCE_PRESET_TO_CRON.daily;
  }

  if (preset === "weekly") {
    return CADENCE_PRESET_TO_CRON.weekly;
  }

  if (preset === "monthly") {
    return CADENCE_PRESET_TO_CRON.monthly;
  }

  if (preset === "custom") {
    const customValue = normalizeCadence(customCron);
    if (customValue) {
      return customValue;
    }

    const fallback = normalizeCadence(fallbackCadence);
    if (fallback && cadencePresetFromValue(fallback) === "custom") {
      return fallback;
    }

    return "";
  }

  return normalizeCadence(fallbackCadence);
}

export function validateCadenceSelection({
  preset,
  customCron,
  fallbackCadence = "",
  allowLegacyCustom = false,
} = {}) {
  if (!THREAD_SCHEDULE_PRESETS.includes(preset)) {
    return "Schedule preset is required.";
  }

  if (preset !== "custom") {
    return "";
  }

  const customValue = normalizeCadence(customCron);
  if (customValue) {
    if (!isLikelyCronExpression(customValue)) {
      return "Custom schedule must be a 5-field cron expression.";
    }
    return "";
  }

  if (allowLegacyCustom && normalizeCadence(fallbackCadence) === "custom") {
    return "";
  }

  return "Custom schedule cron expression is required.";
}

export function cadenceMatchesFilter(cadence, filterCadence) {
  const filter = normalizeCadence(filterCadence);
  if (!filter) {
    return true;
  }

  const value = normalizeCadence(cadence);

  if (isLikelyCronExpression(filter)) {
    return value === filter;
  }

  if (filter === "custom") {
    return cadencePresetFromValue(value) === "custom";
  }

  return cadencePresetFromValue(value) === cadencePresetFromValue(filter);
}

export function buildThreadFilterQueryString(filters = {}) {
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

export function buildThreadFilterQueryParams(filters = {}) {
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

export function readBackendStaleState(thread) {
  if (typeof thread?.stale === "boolean") {
    return thread.stale;
  }

  return null;
}

export function computeStaleness(thread) {
  const backendStale = readBackendStaleState(thread);
  if (typeof backendStale === "boolean") {
    return backendStale
      ? { stale: true, label: "Stale", className: "bg-red-500/10 text-red-400" }
      : {
          stale: false,
          label: "Fresh",
          className: "bg-emerald-500/10 text-emerald-400",
        };
  }

  if (!thread?.next_check_in_at) {
    return {
      stale: false,
      label: "No check-in",
      className: "bg-gray-200 text-gray-600",
    };
  }

  const stale = Date.parse(String(thread.next_check_in_at)) < Date.now();
  return stale
    ? { stale: true, label: "Stale", className: "bg-red-500/10 text-red-400" }
    : {
        stale: false,
        label: "Fresh",
        className: "bg-emerald-500/10 text-emerald-400",
      };
}
