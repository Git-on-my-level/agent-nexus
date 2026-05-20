import { formatStorageBytes } from "$lib/hosted/usageStats.js";

const COUNT_LABELS = {
  "24h": "Last 24h",
  "7d": "Last 7d",
  "30d": "Last 30d",
  active: "Active",
  archived: "Archived",
  fresh: "Fresh",
  none: "None",
  older: "Older",
  pending: "Pending",
  provisioning: "Provisioning",
  quota: "Quota",
  read_only: "Read only",
  read_write: "Read/write",
  ready: "Ready",
  running: "Running",
  stale: "Stale",
  stopped: "Stopped",
  suspended: "Suspended",
  unknown: "Unknown",
};

const EVENT_LABELS = {
  billing_webhook_failed: "Billing webhook failed",
  organization_plan_entitlement_granted: "Entitlement granted",
  organization_plan_entitlement_revoked: "Entitlement revoked",
  organization_restriction_changed: "Organization restriction changed",
  provisioning_failed: "Provisioning failed",
  quota_enforcement_applied: "Quota enforcement applied",
  quota_enforcement_failed: "Quota enforcement failed",
  workspace_backup_failed: "Backup failed",
  workspace_restore_failed: "Restore failed",
  workspace_restriction_changed: "Workspace restriction changed",
  workspace_session_exchanged: "Session exchanged",
};

export function countLabel(key) {
  const raw = String(key ?? "").trim();
  if (!raw) return "Unknown";
  return COUNT_LABELS[raw] ?? raw.replaceAll("_", " ");
}

export function countRows(counts = {}, options = {}) {
  const entries = Object.entries(counts ?? {});
  const unknownLast = options.unknownLast !== false;
  return entries
    .map(([key, value]) => ({
      key,
      label: countLabel(key),
      value: Number(value ?? 0),
      tone: telemetryTone(key),
    }))
    .sort((a, b) => {
      if (unknownLast && a.key === "unknown" && b.key !== "unknown") return 1;
      if (unknownLast && b.key === "unknown" && a.key !== "unknown") return -1;
      return b.value - a.value || a.label.localeCompare(b.label);
    });
}

export function telemetryTone(value) {
  const v = String(value ?? "").toLowerCase();
  if (v === "fresh" || v === "ready" || v === "running" || v === "active") {
    return "ok";
  }
  if (v === "stale" || v === "unknown" || v === "read_only") {
    return "warn";
  }
  if (v === "failed" || v === "suspended" || v === "degraded") {
    return "danger";
  }
  return "neutral";
}

export function telemetryLabel(value, ageSeconds = null) {
  const v = String(value ?? "")
    .trim()
    .toLowerCase();
  if (!v || v === "unknown") return "Unknown telemetry";
  if (v === "stale") {
    const age = formatAgeSeconds(ageSeconds);
    return age ? `Stale (${age})` : "Stale telemetry";
  }
  if (v === "fresh") return "Fresh";
  return countLabel(v);
}

export function formatAgeSeconds(seconds) {
  if (seconds == null || seconds === "") return "";
  const n = Number(seconds);
  if (!Number.isFinite(n) || n < 0) return "";
  if (n < 60) return `${Math.round(n)}s`;
  if (n < 3600) return `${Math.round(n / 60)}m`;
  if (n < 86400) return `${Math.round(n / 3600)}h`;
  return `${Math.round(n / 86400)}d`;
}

export function formatNumber(value) {
  const n = Number(value ?? 0);
  if (!Number.isFinite(n)) return "0";
  return new Intl.NumberFormat("en-US").format(n);
}

export function usageMetricCards(usage = {}) {
  return [
    {
      key: "storage",
      label: "Storage",
      value: formatStorageBytes(usage.storage_bytes),
      subvalue: `${formatStorageBytes(usage.db_bytes)} db / ${formatStorageBytes(
        usage.blob_bytes,
      )} blobs`,
    },
    {
      key: "artifacts",
      label: "Artifacts",
      value: formatNumber(usage.artifact_count),
      subvalue: `${formatNumber(usage.document_count)} docs`,
    },
    {
      key: "events",
      label: "Events",
      value: formatNumber(usage.event_count),
      subvalue: `${formatNumber(usage.agent_count)} agents`,
    },
    {
      key: "workspaces",
      label: "Workspaces",
      value: formatNumber(usage.workspace_count),
      subvalue: "Reported rows",
    },
  ];
}

export function eventLabel(eventType) {
  const raw = String(eventType ?? "").trim();
  if (!raw) return "Unknown event";
  return EVENT_LABELS[raw] ?? raw.replaceAll("_", " ");
}

export function formatDateTime(value) {
  const raw = String(value ?? "").trim();
  if (!raw) return "Unknown";
  const d = new Date(raw);
  if (Number.isNaN(d.getTime())) return "Unknown";
  const formatted = new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(d);
  return `${formatted} (local)`;
}

export function opsWindowMs(window) {
  switch (String(window ?? "").trim()) {
    case "1h":
      return 60 * 60 * 1000;
    case "6h":
      return 6 * 60 * 60 * 1000;
    case "7d":
      return 7 * 24 * 60 * 60 * 1000;
    case "24h":
    default:
      return 24 * 60 * 60 * 1000;
  }
}

export function isWithinOpsWindow(isoTimestamp, window, now = Date.now()) {
  const raw = String(isoTimestamp ?? "").trim();
  if (!raw) return false;
  const t = Date.parse(raw);
  if (!Number.isFinite(t)) return false;
  return now - t <= opsWindowMs(window);
}

export function detailHref(kind, id) {
  const clean = encodeURIComponent(String(id ?? "").trim());
  if (!clean) return "/hosted/admin";
  if (kind === "org") return `/hosted/admin/organizations/${clean}`;
  if (kind === "workspace") return `/hosted/admin/workspaces/${clean}`;
  if (kind === "account") return `/hosted/admin/accounts/${clean}`;
  return "/hosted/admin";
}

export function formatListValue(value) {
  const raw = String(value ?? "").trim();
  if (!raw) return "Any";
  return countLabel(raw);
}

export function sortRows(rows = [], key, direction = "desc") {
  const dir = direction === "asc" ? 1 : -1;
  const sorted = [...(rows ?? [])];
  sorted.sort(
    (a, b) =>
      compareAdminValues(valueAtPath(a, key), valueAtPath(b, key)) * dir,
  );
  return sorted;
}

export function valueAtPath(row, path) {
  return String(path ?? "")
    .split(".")
    .filter(Boolean)
    .reduce((value, part) => (value == null ? undefined : value[part]), row);
}

export function compareAdminValues(a, b) {
  const an = Number(a ?? Number.NaN);
  const bn = Number(b ?? Number.NaN);
  if (Number.isFinite(an) && Number.isFinite(bn)) return an - bn;
  return String(a ?? "").localeCompare(String(b ?? ""), undefined, {
    numeric: true,
    sensitivity: "base",
  });
}

export function usagePressure(row = {}) {
  const quota = row.quota ?? row.plan_resolution?.quota ?? {};
  const usage = row.usage ?? row;
  const storageLimit = Number(
    quota.storage_bytes ??
      quota.storage_limit_bytes ??
      quota.max_storage_bytes ??
      quota.storageBytes ??
      0,
  );
  const storage = Number(usage.storage_bytes ?? usage.storageBytes ?? 0);
  if (storageLimit > 0 && storage >= storageLimit * 0.9) return "high";
  if (storageLimit > 0 && storage >= storageLimit * 0.75) return "medium";
  if (
    String(row.access_mode ?? "").toLowerCase() === "read_only" ||
    String(row.restriction_reason ?? "").toLowerCase() === "quota"
  ) {
    return "high";
  }
  return "normal";
}

export function backupFreshness(workspace = {}, now = Date.now()) {
  const raw = workspace.last_successful_backup_at ?? workspace.lastBackupAt;
  if (!raw) return "unknown";
  const t = Date.parse(raw);
  if (!Number.isFinite(t)) return "unknown";
  return now - t > 36 * 60 * 60 * 1000 ? "stale" : "fresh";
}

export function formatDuration(seconds) {
  if (seconds == null) return "—";
  const n = Number(seconds);
  if (!Number.isFinite(n) || n < 0) return "—";
  if (n < 60) return `${Math.round(n)}s`;
  if (n < 3600) {
    const m = Math.floor(n / 60);
    const s = Math.round(n % 60);
    return s ? `${m}m ${s}s` : `${m}m`;
  }
  if (n < 86400) {
    const h = Math.floor(n / 3600);
    const m = Math.round((n % 3600) / 60);
    return m ? `${h}h ${m}m` : `${h}h`;
  }
  const d = Math.floor(n / 86400);
  const h = Math.round((n % 86400) / 3600);
  return h ? `${d}d ${h}h` : `${d}d`;
}

export function formatRate(value) {
  if (value == null) return "—";
  const n = Number(value);
  if (!Number.isFinite(n)) return "—";
  return `${(n * 100).toFixed(1)}%`;
}

export function quotaPressureRatio(row = {}) {
  const planLimit = Number(
    row.plan_resolution?.entitlement?.included_storage_bytes ??
      row.plan?.included_storage_bytes ??
      row.quota?.storage_bytes ??
      0,
  );
  const used = Number(row.usage?.storage_bytes ?? row.storage_bytes ?? 0);
  if (!Number.isFinite(planLimit) || planLimit <= 0) return null;
  return Math.max(0, used / planLimit);
}

export function providerLabels(account = {}) {
  return (account.oauth_providers ?? [])
    .map((p) => countLabel(p))
    .filter(Boolean)
    .join(", ");
}
