<script>
  /**
   * Dense dotted metric row for workspace list surfaces (boards, topics, documents).
   * Use either `displayValue` or numeric `count` (default String(count ?? 0)).
   *
   * @typedef {{
   *   key?: string,
   *   label: string,
   *   dotClass: string,
   *   count?: number,
   *   displayValue?: string,
   * }} WorkspaceMetricChip
   * @type {{
   *   items: WorkspaceMetricChip[],
   *   footer?: string,
   * }}
   */
  let { items = [], footer = "" } = $props();
</script>

{#if items.length > 0}
  <div
    class="mt-1.5 grid grid-cols-3 gap-x-2 gap-y-1 text-micro text-fg-muted sm:flex sm:flex-wrap sm:items-center sm:gap-x-3"
  >
    {#each items as item (item.key ?? item.label)}
      {@const value =
        item.displayValue != null ? item.displayValue : String(item.count ?? 0)}
      {@const isZero =
        item.displayValue == null && Number(item.count ?? 0) === 0}
      <span class="inline-flex min-w-0 items-center gap-1.5">
        <span
          class="h-1.5 w-1.5 shrink-0 rounded-full {item.dotClass} {isZero
            ? 'opacity-40'
            : ''}"
          aria-hidden="true"
        ></span>
        <span class={isZero ? "text-fg-muted" : "text-fg"}>{value}</span>
        <span class="truncate {isZero ? 'text-fg-subtle' : ''}"
          >{item.label}</span
        >
      </span>
    {/each}
    {#if footer}
      <span class="hidden sm:inline text-fg-subtle">·</span>
      <span class="col-span-3 text-fg-subtle sm:col-span-1 sm:text-fg-muted"
        >{footer}</span
      >
    {/if}
  </div>
{/if}
