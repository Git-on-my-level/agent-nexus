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
    active: "ui-badge--ok",
    archived: "ui-badge--warn",
    trashed: "ui-badge--danger",
  };

  let normalized = $derived(
    String(state ?? "")
      .trim()
      .toLowerCase(),
  );
  let visible = $derived(
    Boolean(normalized) && (forceShow || normalized !== "active"),
  );
  let tone = $derived(TONE[normalized] ?? "ui-badge--neutral");
  let text = $derived(
    label ||
      (normalized ? normalized[0].toUpperCase() + normalized.slice(1) : ""),
  );
</script>

{#if visible}
  <span class="ui-badge {tone} shrink-0 {extraClass}">
    {text}
  </span>
{/if}
