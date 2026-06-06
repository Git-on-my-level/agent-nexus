<script>
  import { ICONS } from "$lib/icons.js";

  /** @type {{
   *   name: keyof typeof ICONS,
   *   class?: string,
   *   title?: string,
   * }} */
  let { name, class: extraClass = "h-3.5 w-3.5", title = "" } = $props();

  let def = $derived(ICONS[name]);
  let viewBox = $derived(def?.viewBox ?? "0 0 24 24");
  let isStroke = $derived(Boolean(def?.stroke));
  let isFilled = $derived(def?.fill === "currentColor" && !def?.stroke);
</script>

<svg
  class={extraClass}
  fill={isStroke ? "none" : (def?.fill ?? "none")}
  {viewBox}
  stroke={isStroke ? "currentColor" : undefined}
  stroke-width={isStroke ? "2" : undefined}
  aria-hidden={title ? undefined : "true"}
  role={title ? "img" : undefined}
>
  {#if title}
    <title>{title}</title>
  {/if}
  {#if def?.paths}
    {#each def.paths as pathD, i (pathD)}
      <path
        d={pathD}
        fill={def.fill ?? "currentColor"}
        fill-rule={i > 0 ? (def.fillRule ?? undefined) : undefined}
        clip-rule={i > 0 ? (def.clipRule ?? undefined) : undefined}
      />
    {/each}
  {:else if def?.d}
    <path
      d={def.d}
      stroke-linecap={isStroke ? "round" : undefined}
      stroke-linejoin={isStroke ? "round" : undefined}
      fill={isFilled ? "currentColor" : undefined}
    />
  {/if}
</svg>
