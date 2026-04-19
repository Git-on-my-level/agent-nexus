<script>
  let { count = 3, class: className = "" } = $props();
</script>

<div
  class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 {className}"
  aria-hidden="true"
>
  {#each Array.from({ length: count }, (_, i) => i) as i (i)}
    <div class="rounded-md border border-line bg-bg-soft px-4 py-3">
      <div class="flex items-center gap-2.5">
        <div class="h-8 w-8 animate-pulse rounded-full bg-panel-hover"></div>
        <div class="flex-1 space-y-2">
          <div
            class="h-4 animate-pulse rounded bg-panel-hover"
            style="width: 60%"
          ></div>
          <div
            class="h-3 animate-pulse rounded bg-panel-hover"
            style="width: 40%"
          ></div>
        </div>
      </div>
      <div class="mt-4 h-7 w-16 animate-pulse rounded bg-panel-hover"></div>
    </div>
  {/each}
</div>
