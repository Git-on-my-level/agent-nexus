<script>
  /**
   * Plan health, compactly.
   *
   * A health badge used to be a full-width pill that a tile header clipped to
   * `On tr…`. A badge that cannot show its own text is worse than no badge, so
   * this has two forms and neither truncates:
   *
   * - `icon` (the default inside a tight header): a tone-coloured glyph, with
   *   the full label and core's reason on hover and in the accessible name.
   * - `pill`: the short label in a tone-coloured pill, for a page header that
   *   has room for words.
   *
   * The box is `.ui-badge`, the same one every other badge uses, so a health
   * badge sits on the same optical line as the ones beside it.
   *
   * Nothing is rendered when core computed no health: an invented "on track"
   * would disagree with the next page load.
   */
  import { planHealthTitle } from "$lib/planHealth.js";

  let {
    /** A `planHealthModel` result. */
    health = null,
    /** `icon` | `pill` */
    variant = "icon",
    class: extraClass = "",
  } = $props();

  let title = $derived(planHealthTitle(health));
</script>

{#if health?.known}
  <span
    class="ui-badge ui-badge--{health.tone} health-badge health-badge--{variant} {extraClass}"
    data-health={health.state}
    {title}
    aria-label={title}
    role="img"
  >
    <span class="health-badge__glyph" aria-hidden="true">{health.glyph}</span>
    {#if variant === "pill"}
      <span class="health-badge__text">{health.short}</span>
    {/if}
  </span>
{/if}

<style>
  .health-badge {
    flex: none;
    gap: 4px;
    /* The point of the compact form: it is never the thing that gets clipped,
       so it never needs an ellipsis. */
    white-space: nowrap;
    cursor: help;
  }
  /* A square: the glyph carries the meaning and the tooltip carries the words. */
  .health-badge--icon {
    width: 18px;
    padding: 0;
  }
  .health-badge__glyph {
    font-size: 10px;
    line-height: 1;
  }
</style>
