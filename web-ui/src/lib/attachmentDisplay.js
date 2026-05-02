/** Shared formatting for attachment chips and artifact surfaces (pure helpers). */

const MIME_SHORT_LABEL = {
  "application/pdf": "PDF",
  "image/jpeg": "JPEG",
  "image/jpg": "JPEG",
  "image/pjpeg": "JPEG",
  "image/png": "PNG",
  "image/gif": "GIF",
  "image/webp": "WEBP",
  "image/svg+xml": "SVG",
  "text/plain": "TXT",
  "text/markdown": "MD",
  "text/html": "HTML",
  "application/json": "JSON",
  "application/zip": "ZIP",
  "application/x-zip-compressed": "ZIP",
  "application/msword": "DOC",
  "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
    "DOCX",
  "application/vnd.ms-excel": "XLS",
  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": "XLSX",
};

/**
 * Short uppercase badge for Content-Type (e.g. PNG, PDF). Unknown → FILE.
 * @param {string} contentType
 */
export function shortMimeBadge(contentType) {
  const m = String(contentType ?? "")
    .trim()
    .toLowerCase();
  if (!m) return "";
  const base = m.split(";")[0]?.trim() ?? "";
  if (MIME_SHORT_LABEL[base]) return MIME_SHORT_LABEL[base];
  const slash = base.indexOf("/");
  if (slash <= 0) return "FILE";
  const major = base.slice(0, slash);
  const minorRaw = base.slice(slash + 1);
  const minor = minorRaw.split("+")[0]?.trim() ?? "";
  if (!minor) return "FILE";
  if (major === "image") {
    if (minor === "jpeg" || minor === "jpg") return "JPEG";
    return minor.slice(0, 8).toUpperCase();
  }
  if (major === "text") {
    if (minor === "plain") return "TXT";
    const short = minor.slice(0, 6).toUpperCase();
    return short || "FILE";
  }
  if (major === "audio")
    return minor.includes("mpeg") ? "MP3" : minor.toUpperCase().slice(0, 8);
  if (major === "video") return minor.toUpperCase().slice(0, 8);
  return "FILE";
}

/**
 * Middle-truncate filename preserving extension (ellipsis in stem).
 * @param {string} name
 * @param {number} maxLen
 */
export function middleTruncateFilename(name, maxLen = 36) {
  const n = String(name ?? "").trim();
  if (!n || n.length <= maxLen) return n;

  const lastDot = n.lastIndexOf(".");
  let stem;
  /** @type {string} */
  let ext;
  if (lastDot > 0 && lastDot < n.length - 1 && lastDot <= n.length - 2) {
    ext = n.slice(lastDot);
    stem = n.slice(0, lastDot);
  } else {
    ext = "";
    stem = n;
  }

  const ellipsis = "…";
  const reserved = ext.length + ellipsis.length;
  const budget = maxLen - reserved;
  if (budget < 4) {
    if (!ext) {
      return `${stem.slice(0, Math.max(1, maxLen - ellipsis.length))}${ellipsis}`;
    }
    const keepStem = Math.max(1, maxLen - ext.length - ellipsis.length);
    return `${stem.slice(0, keepStem)}${ellipsis}${ext}`;
  }

  const left = Math.ceil(budget / 2);
  const right = Math.floor(budget / 2);
  if (stem.length <= left + right) return n;
  return `${stem.slice(0, left)}${ellipsis}${stem.slice(stem.length - right)}${ext}`;
}

/**
 * Human-readable size; empty string when unknown or zero (plan: hide 0 B).
 * @param {number} bytes
 */
export function formatBytes(bytes) {
  const n = Number(bytes);
  if (!Number.isFinite(n) || n <= 0) return "";
  const units = ["B", "KB", "MB", "GB"];
  let value = n;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const rounded =
    value >= 10 || unit === 0 ? Math.round(value) : Number(value.toFixed(1));
  return `${rounded} ${units[unit]}`;
}

/**
 * Whether merged attachment metadata marks the artifact as in trash (same rules as AttachmentChip).
 * @param {{ attachmentMeta?: object } | null | undefined} resolved
 * @param {object | null | undefined} artifactOverlay
 */
export function isTrashedAttachmentMeta(resolved, artifactOverlay) {
  const base =
    resolved?.attachmentMeta && typeof resolved.attachmentMeta === "object"
      ? resolved.attachmentMeta
      : {};
  const over =
    artifactOverlay && typeof artifactOverlay === "object"
      ? artifactOverlay
      : {};
  const m = { ...base, ...over };
  return Boolean(m.trashed_at ?? m.trashedAt);
}
