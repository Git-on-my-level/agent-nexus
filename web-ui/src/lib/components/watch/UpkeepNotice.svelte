<script>
  /**
   * One line when housekeeping suggestions have waited days: no agent is
   * tending the workspace. It names the one step that fixes it.
   */
  import { upkeepNotice } from "$lib/briefOutcome.js";

  let { upkeep = null } = $props();
  let notice = $derived(upkeepNotice(upkeep));
</script>

{#if notice}
  <p class="upkeep text-meta" data-source="computed:upkeep" data-upkeep-notice>
    <span class="font-medium text-warn-text"
      >{notice.count} upkeep suggestions have waited more than {notice.days} days.
      No agent is tending this workspace.</span
    >
    <span class="text-fg-muted"
      >Give one agent the project-manager skill:
      <code>anx skills sync --pm</code></span
    >
  </p>
{/if}

<style>
  .upkeep {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    column-gap: 8px;
    row-gap: 4px;
    margin: 0 0 16px;
    border-radius: var(--radius-md);
    background: var(--warn-soft);
    padding: 8px 12px;
  }
</style>
