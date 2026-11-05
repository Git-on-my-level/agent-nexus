<script>
  /**
   * Stands in for the Access page while its reads settle. The page renders
   * every section at once when they do, so this holds roughly the space the
   * first sections take and nothing below it jumps as reads land one by one.
   */
  import SkeletonList from "$lib/components/state/SkeletonList.svelte";

  const SECTIONS = [
    { title: "Hosts", rows: 2, rowClass: "h-[88px]" },
    { title: "Administrators", rows: 2, rowClass: "h-[52px]" },
    { title: "People", rows: 3, rowClass: "h-[52px]" },
  ];
</script>

<div class="space-y-6 sm:space-y-8" data-access-loading>
  <p class="sr-only" role="status">Loading access…</p>
  {#each SECTIONS as section (section.title)}
    <div aria-hidden="true">
      <p class="mb-2 text-meta font-semibold text-fg-subtle">
        {section.title}
      </p>
      <SkeletonList rows={section.rows} rowClass={section.rowClass} />
    </div>
  {/each}
</div>
