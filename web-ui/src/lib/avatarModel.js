/**
 * Shared deterministic avatar initials + palette (workspace + hosted).
 * OSS-safe — lives in $lib; hosted/ may import this module.
 */

export const AVATAR_PALETTE = [
  { bg: "bg-accent-soft", fg: "text-accent-text" },
  { bg: "bg-ok-soft", fg: "text-ok-text" },
  { bg: "bg-warn-soft", fg: "text-warn-text" },
  { bg: "bg-sky-500/15", fg: "text-sky-300" },
  { bg: "bg-rose-500/15", fg: "text-rose-300" },
  { bg: "bg-violet-500/15", fg: "text-violet-300" },
  { bg: "bg-teal-500/15", fg: "text-teal-300" },
  { bg: "bg-fuchsia-500/15", fg: "text-fuchsia-300" },
];

/** @param {unknown} input */
export function hashSeed(input) {
  const s = String(input ?? "");
  let h = 0;
  for (let i = 0; i < s.length; i++) {
    h = (h * 31 + s.charCodeAt(i)) | 0;
  }
  return Math.abs(h);
}

/** @param {unknown} input */
export function initialsOf(input) {
  const s = String(input ?? "").trim();
  if (!s) return "·";
  const parts = s.split(/[\s_.@-]+/).filter(Boolean);
  if (parts.length >= 2) {
    return (parts[0][0] + parts[1][0]).toUpperCase();
  }
  return s.slice(0, 2).toUpperCase();
}

/**
 * @param {unknown} seed
 * @param {unknown} label
 */
export function paletteForSeed(seed, label = "") {
  return AVATAR_PALETTE[hashSeed(seed || label) % AVATAR_PALETTE.length];
}

/** @type {'xs'|'sm'|'md'|'lg'} */
export function avatarSizeClasses(size) {
  if (size === "xs") return "h-5 w-5 rounded text-[9px]";
  if (size === "sm") return "h-6 w-6 rounded text-micro";
  if (size === "lg") return "h-9 w-9 rounded-md text-meta";
  return "h-7 w-7 rounded-md text-micro";
}

/**
 * Truncate long handle-style display names for dense rows.
 * @param {unknown} name
 * @param {number} [maxLen]
 */
export function truncateActorDisplayName(name, maxLen = 24) {
  const n = String(name ?? "").trim();
  if (!n) return "—";
  if (n.length <= maxLen || n.includes(" ")) return n;
  const dotIdx = n.indexOf(".");
  if (dotIdx > 0 && dotIdx < n.length - 1) {
    const prefix = n.slice(0, dotIdx);
    const suffix = n.slice(dotIdx + 1, dotIdx + 9);
    return `${prefix}.${suffix}…`;
  }
  return `${n.slice(0, 20)}…`;
}
