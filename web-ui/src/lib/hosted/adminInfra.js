import { formatStorageBytes } from "$lib/hosted/usageStats.js";

import {
  formatAgeSeconds,
  formatNumber,
  telemetryLabel,
} from "$lib/hosted/adminOverview.js";

export function percentUsed(used, total) {
  const u = Number(used ?? 0);
  const t = Number(total ?? 0);
  if (!Number.isFinite(u) || !Number.isFinite(t) || t <= 0) return null;
  return Math.max(0, Math.min(100, Math.round((u / t) * 100)));
}

export function formatPercent(value) {
  if (value == null) return "Unknown";
  const n = Number(value);
  if (!Number.isFinite(n)) return "Unknown";
  return `${Math.round(n)}%`;
}

export function formatBytePair(bytes = {}) {
  const used = Number(bytes.used_bytes ?? 0);
  const free = Number(bytes.free_bytes ?? 0);
  const total = Number(bytes.total_bytes ?? 0);
  if (total <= 0 && used <= 0 && free <= 0) return "Not wired";
  return `${formatStorageBytes(used)} used / ${formatStorageBytes(free)} free`;
}

export function filesystemUsage(fs = {}) {
  const bytes = fs.bytes ?? {};
  const inodes = fs.inodes ?? null;
  return {
    path: fs.path || "Unknown path",
    byteLabel: formatBytePair(bytes),
    bytePercent: percentUsed(bytes.used_bytes, bytes.total_bytes),
    inodeLabel: inodes
      ? `${formatNumber(inodes.used)} used / ${formatNumber(inodes.free)} free`
      : "Unknown",
    inodePercent: inodes ? percentUsed(inodes.used, inodes.total) : null,
  };
}

export function runtimeCountsForHost(workspaces = [], host = {}) {
  const hostID = String(host.id ?? "").trim();
  const hostLabel = String(host.label ?? "").trim();
  const matched = (workspaces ?? []).filter((ws) => {
    if (hostID && ws.host_id === hostID) return true;
    return hostLabel && ws.host_label === hostLabel;
  });
  const counts = {
    total: matched.length,
    running: 0,
    stopped: 0,
    draining: 0,
    unknown: 0,
    staleHeartbeat: 0,
    staleRuntimeMetadata: 0,
  };
  const imageTags = new Map();
  for (const ws of matched) {
    const state = String(ws.runtime_power_state || "unknown").toLowerCase();
    if (state === "running") counts.running += 1;
    else if (state === "stopped") counts.stopped += 1;
    else if (state === "draining") counts.draining += 1;
    else counts.unknown += 1;
    if (ws.heartbeat_freshness !== "fresh") counts.staleHeartbeat += 1;
    if (isRuntimeMetadataStale(ws)) counts.staleRuntimeMetadata += 1;
    const tag = ws.runtime_image_tag || "unknown";
    imageTags.set(tag, (imageTags.get(tag) ?? 0) + 1);
  }
  return {
    ...counts,
    rows: matched,
    imageTags: [...imageTags.entries()]
      .map(([reference, count]) => ({ reference, count }))
      .sort(
        (a, b) => b.count - a.count || a.reference.localeCompare(b.reference),
      ),
  };
}

export function isRuntimeMetadataStale(workspace = {}) {
  const state = String(workspace.runtime_power_state || "").toLowerCase();
  const hasContainer = Boolean(
    String(workspace.container_id_short || "").trim(),
  );
  if (state === "running" && !hasContainer) return true;
  return (
    state === "unknown" &&
    Boolean(workspace.listen_port || workspace.host_id || workspace.host_label)
  );
}

export function hostWarning(
  host = {},
  counts = runtimeCountsForHost([], host),
) {
  const freshness = String(host.telemetry_freshness || "unknown").toLowerCase();
  if (freshness === "unknown") return "Live resource telemetry is not wired";
  if (freshness === "stale") {
    return `Telemetry stale ${formatAgeSeconds(host.telemetry_age_seconds)}`;
  }
  if (host.drain_mode || host.placement_available === false) {
    return "Placement unavailable";
  }
  if (counts.staleHeartbeat || counts.staleRuntimeMetadata) {
    return "Workspace runtime attention needed";
  }
  return "";
}

export function telemetryResourceCards(host = {}) {
  const payload = host.latest_snapshot?.payload ?? null;
  if (!payload) {
    return [
      { key: "cpu", label: "CPU load", value: "Not wired", subvalue: "" },
      { key: "memory", label: "Memory", value: "Not wired", subvalue: "" },
      {
        key: "workspace_disk",
        label: "Workspace disk",
        value: "Not wired",
        subvalue: "",
      },
      {
        key: "docker_disk",
        label: "Docker disk",
        value: "Not wired",
        subvalue: "",
      },
    ];
  }
  const workspaceFs = filesystemUsage(payload.workspace_root_disk);
  const dockerFs = filesystemUsage(payload.docker_root_disk);
  const memory = payload.memory ?? {};
  return [
    {
      key: "cpu",
      label: "CPU load",
      value: formatLoad(payload.cpu?.load1),
      subvalue: `5m ${formatLoad(payload.cpu?.load5)} / 15m ${formatLoad(
        payload.cpu?.load15,
      )}`,
    },
    {
      key: "memory",
      label: "Memory",
      value: formatPercent(percentUsed(memory.used_bytes, memory.total_bytes)),
      subvalue: formatBytePair(memory),
    },
    {
      key: "workspace_disk",
      label: "Workspace disk",
      value: formatPercent(workspaceFs.bytePercent),
      subvalue: workspaceFs.byteLabel,
    },
    {
      key: "docker_disk",
      label: "Docker disk",
      value: formatPercent(dockerFs.bytePercent),
      subvalue: dockerFs.byteLabel,
    },
  ];
}

export function hostFreshnessLabel(host = {}) {
  return telemetryLabel(host.telemetry_freshness, host.telemetry_age_seconds);
}

function formatLoad(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return "Unknown";
  return n.toFixed(2);
}

export function saturationTone(pct) {
  if (pct == null) return "bg-bg text-fg-subtle";
  const n = Number(pct);
  if (!Number.isFinite(n)) return "bg-bg text-fg-subtle";
  if (n >= 85) return "bg-danger-soft text-danger-text";
  if (n >= 65) return "bg-warn-soft text-warn-text";
  return "bg-ok-soft text-ok-text";
}

const SATURATION_DRIVER_LABELS = {
  cpu: "CPU",
  memory: "Memory",
  workspace_disk: "Workspace disk",
  docker_disk: "Docker disk",
  inodes: "Inodes",
  slots: "Slot count",
  disk: "Workspace disk",
  slots_used: "Slot count",
  telemetry: "Telemetry missing",
  drained: "Draining",
};

export function saturationDriverLabel(driver) {
  if (!driver) return "—";
  return SATURATION_DRIVER_LABELS[driver] ?? driver;
}
