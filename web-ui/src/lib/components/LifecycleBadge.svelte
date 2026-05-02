<script>
  /**
   * Lifecycle state pill (active / archived / trashed / ...).
   *
   * Hidden by default when state === "active": Active is the implicit default
   * in list views, so surfacing it on every row is noise. Pass `forceShow`
   * for surfaces (detail headers, mixed-state rows you want to be explicit on)
   * where the Active state should still render.
   *
   * @type {{
   *   state?: string,
   *   label?: string,
   *   forceShow?: boolean,
   *   class?: string,
   * }}
   */
  let {
    state = "",
    label = "",
    forceShow = false,
    class: extraClass = "",
  } = $props();

  const TONE = {
    active: "text-ok-text bg-ok-soft",
    archived: "text-warn-text bg-warn-soft",
    trashed: "text-slate-300 bg-slate-500/10",
  };

  let normalized = $derived(
    String(state ?? "")
      .trim()
      .toLowerCase(),
  );
  let visible = $derived(
    Boolean(normalized) && (forceShow || normalized !== "active"),
  );
  let tone = $derived(TONE[normalized] ?? "text-fg-muted bg-line");
  let text = $derived(
    label ||
      (normalized ? normalized[0].toUpperCase() + normalized.slice(1) : ""),
  );
</script>

{#if visible}
  <span
    class="inline-flex shrink-0 rounded px-1.5 py-0.5 text-micro font-semibold {tone} {extraClass}"
  >
    {text}
  </span>
{/if}
