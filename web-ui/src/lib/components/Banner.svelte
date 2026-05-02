<script>
  /** Destructive or high-attention banner row; styling-only shell (see `ui-banner`). */
  let { class: className = "", children, actions } = $props();
</script>

<div class="ui-banner {className}">
  <div class="min-w-0 flex-1">
    {@render children?.()}
  </div>
  {#if actions}
    <div class="flex shrink-0 flex-wrap items-center gap-2">
      {@render actions?.()}
    </div>
  {/if}
</div>
