<script>
  /**
   * The one tooltip layer, mounted once by the shell.
   *
   * Every `use:tooltip` on the page reports into the same store, so a table of
   * badges costs one floating element rather than one per badge — the same
   * reason `AnxRefPreview` is a singleton.
   *
   * The surface is opaque: a translucent tip leaves whatever is under it
   * readable through the words, which is worse than no tip at all.
   */
  import { onMount } from "svelte";

  import {
    activeTooltip,
    hideTooltip,
    tooltipPosition,
  } from "$lib/actions/tooltip.js";

  let tip = $derived($activeTooltip);
  let element = $state();
  let box = $state({ width: 0, height: 0 });
  let viewport = $state({ width: 0, height: 0 });

  // Measured after paint: the layer is positioned from its own size, which is
  // only known once the text is in it.
  $effect(() => {
    if (!tip || !element) return;
    const rect = element.getBoundingClientRect();
    if (rect.width !== box.width || rect.height !== box.height) {
      box = { width: rect.width, height: rect.height };
    }
  });

  let place = $derived(
    tip && box.width
      ? tooltipPosition(tip.rect, box, viewport)
      : { top: -9999, left: -9999, placement: "top" },
  );

  onMount(() => {
    const measure = () => {
      viewport = { width: window.innerWidth, height: window.innerHeight };
    };
    measure();
    // A tooltip anchored to a box that has since scrolled away is a lie.
    window.addEventListener("scroll", hideTooltip, true);
    window.addEventListener("resize", measure);
    return () => {
      window.removeEventListener("scroll", hideTooltip, true);
      window.removeEventListener("resize", measure);
    };
  });
</script>

{#if tip}
  <div
    class="anx-tooltip"
    bind:this={element}
    data-anx-tooltip={place.placement}
    style:top={`${place.top}px`}
    style:left={`${place.left}px`}
    role="tooltip"
    aria-hidden="true"
  >
    {tip.text}
  </div>
{/if}

<style>
  .anx-tooltip {
    position: fixed;
    z-index: 70;
    max-width: min(22rem, calc(100vw - 1rem));
    padding: 4px 8px;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius-sm, 4px);
    /* Opaque, so the page does not read through the words. */
    background: var(--panel);
    color: var(--fg);
    font-size: 11px;
    line-height: 16px;
    box-shadow: var(--shadow-menu);
    pointer-events: none;
    overflow-wrap: anywhere;
  }
</style>
