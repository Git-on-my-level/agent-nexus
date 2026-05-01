const BYTES_PER_STORAGE_GB = 1024 * 1024 * 1024;

export function pct(used, total) {
  const u = Number(used ?? 0);
  const t = Number(total ?? 0);
  if (!Number.isFinite(u) || !Number.isFinite(t) || t <= 0) return 0;
  if (u > 0 && u < t) return Math.max(1, Math.round((u / t) * 100));
  return Math.min(100, Math.round((u / t) * 100));
}

export function bytesFromStorageGB(gb) {
  const n = Number(gb ?? 0);
  if (!Number.isFinite(n) || n <= 0) return 0;
  return n * BYTES_PER_STORAGE_GB;
}

export function storageBytes(value, fallbackGB = 0) {
  const bytes = Number(value ?? 0);
  if (Number.isFinite(bytes) && bytes > 0) return bytes;
  return bytesFromStorageGB(fallbackGB);
}

export function formatStorageBytes(bytes) {
  const n = Number(bytes ?? 0);
  if (!Number.isFinite(n) || n <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = n;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }
  const digits =
    unitIndex > 0 && value < 10 && !Number.isInteger(value) ? 1 : 0;
  return `${value.toFixed(digits)} ${units[unitIndex]}`;
}

export function storageMetric(usage = {}, plan = {}, quota = {}) {
  // Control-plane `storage_gb` is a legacy ceiling field, so any nonzero
  // workspace can report 1 GB. Hosted meters must use byte fields when present.
  const used = storageBytes(usage.storage_bytes, usage.storage_gb);
  const total = storageBytes(
    plan.included_storage_bytes,
    plan.included_storage_gb,
  );
  const remaining = storageBytes(quota.storage_bytes_remaining);
  return {
    label: "Storage (org)",
    used,
    total,
    remaining: remaining || Math.max(0, total - used),
    displayUsed: formatStorageBytes(used),
    displayTotal: formatStorageBytes(total),
    displayRemaining: formatStorageBytes(
      remaining || Math.max(0, total - used),
    ),
  };
}
