<script>
  /**
   * What the asker says a decision puts at risk: whether it can be undone
   * and what it costs. A claim by the asker, shown so the reader can size a
   * decision before opening it.
   */
  import SignalBadge from "$lib/components/work/SignalBadge.svelte";

  let { stakes = null, class: extraClass = "" } = $props();
</script>

{#if stakes?.irreversible}
  <SignalBadge tone="warn" class={extraClass}
    ><span data-source="computed:decisions.stakes" data-stakes-irreversible
      >Irreversible</span
    ></SignalBadge
  >
{/if}
{#if stakes?.cost}
  <SignalBadge class={extraClass}
    ><span data-source="computed:decisions.stakes" data-stakes-cost
      >Cost: {stakes.cost}</span
    ></SignalBadge
  >
{/if}
