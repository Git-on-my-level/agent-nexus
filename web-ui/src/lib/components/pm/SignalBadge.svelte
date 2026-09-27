<script>
  /**
   * One badge. Tone only picks the colour; `.ui-badge` owns the box, so a
   * neutral badge and a warn badge are the same height on the same row.
   */
  let { tone = "neutral", class: extraClass = "", children } = $props();
  const TONES = ["neutral", "warn", "danger", "ok"];
  let toneClass = $derived(
    `ui-badge--${TONES.includes(tone) ? tone : "neutral"}`,
  );
</script>

<!-- The inner span carries the ellipsis: text in a centred flex box that
     overflows is clipped at both ends, not truncated at the end. -->
<span class="ui-badge {toneClass} {extraClass}"
  ><span class="min-w-0 truncate">{@render children?.()}</span></span
>
