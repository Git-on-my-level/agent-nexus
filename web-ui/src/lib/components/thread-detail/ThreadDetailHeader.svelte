<script>
  import { page } from "$app/stores";

  import { workspacePath } from "$lib/workspacePaths";
  import { threadDetailStore } from "$lib/threadDetailStore";
  import { getPriorityLabel } from "$lib/threadFilters";

  let snapshot = $derived($threadDetailStore.snapshot);
  let staleness = $derived(threadDetailStore.getStaleness(snapshot));
  let workspaceSlug = $derived($page.params.workspace);
</script>

<nav
  class="mb-3 flex items-center gap-1.5 text-[13px] text-[var(--ui-text-muted)]"
  aria-label="Breadcrumb"
>
  <a
    class="hover:text-[var(--ui-text)]"
    href={workspacePath(workspaceSlug, "/threads")}>Threads</a
  >
  <span class="text-[var(--ui-text-subtle)]">/</span>
  <span class="truncate text-[var(--ui-text)]">{snapshot?.title || ""}</span>
</nav>

{#if snapshot}
  <div class="flex items-start justify-between gap-4">
    <h1 class="text-lg font-semibold text-[var(--ui-text)]">
      {snapshot.title}
    </h1>
    <div class="flex shrink-0 items-center gap-2 text-[12px]">
      {#if staleness}
        <span
          class="rounded px-2 py-0.5 {staleness.stale
            ? 'bg-rose-500/10 text-rose-400'
            : 'bg-emerald-500/10 text-emerald-400'}"
        >
          {staleness.label}
        </span>
      {/if}
      <span
        class="rounded bg-[var(--ui-border)] px-2 py-0.5 capitalize text-[var(--ui-text-muted)]"
        >{snapshot.status}</span
      >
      <span
        class="rounded bg-[var(--ui-border)] px-2 py-0.5 text-[var(--ui-text-muted)]"
        >{getPriorityLabel(snapshot.priority)}</span
      >
    </div>
  </div>
{/if}
