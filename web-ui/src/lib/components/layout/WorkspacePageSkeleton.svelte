<script>
  /**
   * The destination's own shape, painted before its data arrives: the page
   * name in the header position it will keep, and row placeholders in the
   * bordered container the list will fill.
   *
   * The title is real text, not a grey bar — the first paint should answer
   * "where am I?", which is the one question a blank screen with a spinner
   * cannot. Nothing here reads anything; it adds no requests.
   */
  import WorkspacePageHeader from "./WorkspacePageHeader.svelte";
  import WorkspacePageShell from "./WorkspacePageShell.svelte";

  let { title = "", rows = 6 } = $props();
</script>

<WorkspacePageShell>
  <WorkspacePageHeader {title} />
  <div
    class="space-y-px overflow-hidden rounded-md border border-line bg-bg-soft"
    aria-hidden="true"
  >
    {#each Array.from({ length: rows }, (_, index) => index) as index (index)}
      <div
        class="flex flex-col gap-1.5 px-3 py-3 {index > 0
          ? 'border-t border-line'
          : ''}"
      >
        <div
          class="h-3.5 animate-pulse rounded bg-panel-hover"
          style="width: {index % 3 === 0
            ? '62%'
            : index % 3 === 1
              ? '45%'
              : '53%'}"
        ></div>
        <div class="h-2.5 w-28 animate-pulse rounded bg-panel-hover"></div>
      </div>
    {/each}
  </div>
</WorkspacePageShell>
