<script>
  /**
   * Deterministic actor avatar (initials + palette seeded by actor id or label).
   * OSS-safe shared component — do not import from hosted/.
   */

  /** @type {{
   *   label?: string,
   *   seed?: string,
   *   size?: 'xs'|'sm'|'md'|'lg',
   *   class?: string,
   * }} */
  let { label = "", seed = "", size = "md", class: extraClass = "" } = $props();

  const PALETTE = [
    { bg: "bg-accent-soft", fg: "text-accent-text" },
    { bg: "bg-ok-soft", fg: "text-ok-text" },
    { bg: "bg-warn-soft", fg: "text-warn-text" },
    { bg: "bg-sky-500/15", fg: "text-sky-300" },
    { bg: "bg-rose-500/15", fg: "text-rose-300" },
    { bg: "bg-violet-500/15", fg: "text-violet-300" },
    { bg: "bg-teal-500/15", fg: "text-teal-300" },
    { bg: "bg-fuchsia-500/15", fg: "text-fuchsia-300" },
  ];

  function hashSeed(input) {
    const s = String(input ?? "");
    let h = 0;
    for (let i = 0; i < s.length; i++) {
      h = (h * 31 + s.charCodeAt(i)) | 0;
    }
    return Math.abs(h);
  }

  function initialsOf(input) {
    const s = String(input ?? "").trim();
    if (!s) return "·";
    const parts = s.split(/[\s_.@-]+/).filter(Boolean);
    if (parts.length >= 2) {
      return (parts[0][0] + parts[1][0]).toUpperCase();
    }
    return s.slice(0, 2).toUpperCase();
  }

  const palette = $derived(PALETTE[hashSeed(seed || label) % PALETTE.length]);
  const initials = $derived(initialsOf(label));
  const sizeClasses = $derived(
    size === "xs"
      ? "h-5 w-5 rounded text-[9px]"
      : size === "sm"
        ? "h-6 w-6 rounded text-micro"
        : size === "lg"
          ? "h-9 w-9 rounded-md text-meta"
          : "h-7 w-7 rounded-md text-micro",
  );
</script>

<span
  aria-hidden="true"
  class="inline-flex shrink-0 items-center justify-center font-semibold uppercase {sizeClasses} {palette.bg} {palette.fg} {extraClass}"
>
  {initials}
</span>
