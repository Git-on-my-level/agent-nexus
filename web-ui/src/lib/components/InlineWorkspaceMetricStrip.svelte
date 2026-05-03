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
    class="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-micro text-fg-muted"
  >
    {#each items as item (item.key ?? item.label)}
      <span class="inline-flex items-center gap-1.5">
        <span
          class="h-1.5 w-1.5 rounded-full {item.dotClass}"
          aria-hidden="true"
        ></span>
        <span class="text-fg"
          >{#if item.displayValue != null}{item.displayValue}{:else}{String(
              item.count ?? 0,
            )}{/if}</span
        >
        <span>{item.label}</span>
      </span>
    {/each}
    {#if footer}
      <span class="text-fg-subtle">·</span>
      <span>{footer}</span>
    {/if}
  </div>
{/if}
