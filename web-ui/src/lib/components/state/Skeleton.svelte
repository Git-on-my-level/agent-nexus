<script>
  let {
    rows = 3,
    class: className = "",
  } = $props();
</script>

<div class="space-y-3 {className}" aria-hidden="true">
  {#each Array(rows) as _, i}
    <div class="h-4 animate-pulse rounded bg-panel-hover" style="width: {i === 0 ? '100%' : i === 1 ? '85%' : '65%'}"></div>
  {/each}
</div>
