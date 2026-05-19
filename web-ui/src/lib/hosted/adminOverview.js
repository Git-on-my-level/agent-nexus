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
  return new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(d);
}

export function detailHref(kind, id) {
  const clean = encodeURIComponent(String(id ?? "").trim());
  if (!clean) return "/hosted/admin";
  if (kind === "org") return `/hosted/admin/organizations/${clean}`;
  if (kind === "workspace") return `/hosted/admin/workspaces/${clean}`;
  if (kind === "account") return `/hosted/admin/accounts/${clean}`;
  return "/hosted/admin";
}
