<script>
  import { threadDetailStore } from "$lib/threadDetailStore";
  import { getPriorityLabel } from "$lib/threadFilters";

  let snapshot = $derived($threadDetailStore.snapshot);
  let staleness = $derived(threadDetailStore.getStaleness(snapshot));
</script>

<nav
  class="mb-3 flex items-center gap-1.5 text-sm text-gray-400"
  aria-label="Breadcrumb"
>
  <a class="hover:text-gray-600" href="/threads">Threads</a>
  <span class="text-gray-300">/</span>
  <span class="truncate text-gray-700">{snapshot?.title || ""}</span>
</nav>

{#if snapshot}
  <div class="flex items-start justify-between gap-4">
    <h1 class="text-lg font-semibold text-gray-900">{snapshot.title}</h1>
    <div class="flex shrink-0 items-center gap-2 text-xs">
      {#if staleness}
        <span
          class="rounded px-2 py-0.5 {staleness.stale
            ? 'bg-rose-100 text-rose-700'
            : 'bg-emerald-100 text-emerald-700'}"
        >
          {staleness.label}
        </span>
      {/if}
      <span class="rounded bg-gray-100 px-2 py-0.5 capitalize text-gray-600"
        >{snapshot.status}</span
      >
      <span class="rounded bg-gray-100 px-2 py-0.5 text-gray-600"
        >{getPriorityLabel(snapshot.priority)}</span
      >
    </div>
  </div>
{/if}
