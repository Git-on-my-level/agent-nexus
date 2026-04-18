<script>
  let {
    count = 5,
    class: className = "",
  } = $props();
</script>

<div class="space-y-1.5 {className}" aria-hidden="true">
  {#each Array(count) as _i}
    <div
      class="rounded-md border border-[var(--line)] border-l-[3px] border-l-transparent bg-[var(--bg-soft)] px-3 py-2.5"
    >
      <div class="flex items-center gap-1.5">
        <div
          class="h-4 w-12 animate-pulse rounded bg-panel-hover"
        ></div>
        <div class="h-1.5 w-1.5 animate-pulse rounded-full bg-panel-hover"></div>
        <div class="h-3 w-16 animate-pulse rounded bg-panel-hover"></div>
        <div class="h-3 w-10 animate-pulse rounded bg-panel-hover"></div>
      </div>
      <div class="mt-1.5 h-4 animate-pulse rounded bg-panel-hover" style="width: 70%"></div>
      <div class="mt-1.5 h-3 animate-pulse rounded bg-panel-hover" style="width: 45%"></div>
      <div class="mt-2 flex gap-1.5">
        <div class="h-6 w-20 animate-pulse rounded bg-panel-hover"></div>
        <div class="h-6 w-16 animate-pulse rounded bg-panel-hover"></div>
      </div>
    </div>
  {/each}
</div>
