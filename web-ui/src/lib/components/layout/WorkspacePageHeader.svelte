<script>
  let { title = "", subtitle = null, actions = null } = $props();
</script>

<div class="flex flex-wrap items-start justify-between gap-3">
  <div class="min-w-0">
    {#if title}
      <h1 class="text-subtitle font-semibold text-fg">{title}</h1>
    {/if}
    {#if subtitle}
      <div class="mt-0.5 text-meta text-fg-muted">{@render subtitle()}</div>
    {/if}
  </div>
  {#if actions}
    <div class="flex shrink-0 flex-wrap items-center gap-2">
      {@render actions()}
    </div>
  {/if}
</div>
