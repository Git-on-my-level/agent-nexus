<script>
  import { formatTimestamp, formatAbsoluteDateTime } from "$lib/formatDate";
  let { freshness = {}, compact = false } = $props();
  const fields = [
    ["last_observed_at", "Observed"],
    ["source_activity_at", "Source activity"],
    ["meaningful_progress_at", "Meaningful progress"],
  ];
</script>

<dl class="grid gap-2 text-micro {compact ? 'grid-cols-1' : 'sm:grid-cols-3'}">
  {#each fields as [key, title]}
    <div>
      <dt class="text-fg-muted">{title}</dt>
      <dd class="mt-0.5 text-fg">
        {#if freshness[key]}<time
            datetime={freshness[key]}
            title={formatAbsoluteDateTime(freshness[key])}
            >{formatTimestamp(freshness[key])}</time
          >{:else}Not established{/if}
      </dd>
    </div>
  {/each}
</dl>
