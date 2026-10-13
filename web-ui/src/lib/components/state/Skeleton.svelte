<script>
  let { rows = 3, class: className = "" } = $props();

  const WIDTHS = ["100%", "85%", "65%", "92%", "72%", "80%"];
</script>

<div class="ui-skeleton-reveal space-y-3 {className}" aria-hidden="true">
  {#each Array.from({ length: rows }, (_, i) => i) as i}
    <div
      class="h-4 animate-pulse rounded bg-panel-hover"
      style="width: {WIDTHS[i % WIDTHS.length]}"
    ></div>
  {/each}
</div>
