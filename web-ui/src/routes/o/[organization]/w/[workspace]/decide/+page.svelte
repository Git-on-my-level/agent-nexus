<script>
  import { onMount, tick } from "svelte";
  import { page } from "$app/stores";
  import { briefStore, mountBrief } from "$lib/briefStore.js";
  import { freshnessLabel, sortDecisions } from "$lib/brief.js";
  import { undoInboxResponse } from "$lib/inboxResponseQueue.js";
  import { usePageTitle } from "$lib/pageTitle.svelte.js";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import SkeletonList from "$lib/components/state/SkeletonList.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import DecisionCard from "$lib/components/decide/DecisionCard.svelte";
  import InboxUndoToast from "$lib/components/inbox/InboxUndoToast.svelte";

  /**
   * Decide: every decision waiting on the reader, from `brief.v1`.
   * Order is core's: decide_by, then blocked count, then priority, then age.
   * `?initiative=<ref>` narrows to one initiative (Watch's badges link here).
   * j/k move focus between rows; `c` on a focused row copies its ref.
   */
  usePageTitle(() => "Decide");

  const brief = briefStore();
  let now = $state(Date.now());
  let focusIndex = $state(-1);
  let listEl = $state(null);

  let initiative = $derived($page.url.searchParams.get("initiative") ?? "");
  let decisions = $derived(
    sortDecisions(
      ($brief.data?.decisions ?? []).filter(
        (decision) =>
          !initiative ||
          decision.subject?.ref === initiative ||
          decision.blocks.some((block) => block.ref === initiative),
      ),
    ),
  );
  let freshness = $derived(freshnessLabel($brief.data?.asOf, now));
  let filterTitle = $derived(
    initiative
      ? ($brief.data?.initiatives ?? []).find((row) => row.ref === initiative)
          ?.title || ""
      : "",
  );

  onMount(() => {
    const stop = mountBrief();
    const clock = setInterval(() => (now = Date.now()), 30_000);
    return () => {
      stop();
      clearInterval(clock);
    };
  });

  async function focusRow(index) {
    const rows = listEl?.querySelectorAll("[data-decision-ref]") ?? [];
    if (!rows.length) return;
    focusIndex = Math.max(0, Math.min(rows.length - 1, index));
    await tick();
    rows[focusIndex]?.focus();
  }

  function onKeydown(event) {
    if (event.metaKey || event.ctrlKey || event.altKey) return;
    const target = event.target;
    if (
      target instanceof HTMLElement &&
      (target.isContentEditable ||
        ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName))
    )
      return;
    if (event.key === "j") {
      event.preventDefault();
      void focusRow(focusIndex + 1);
    } else if (event.key === "k") {
      event.preventDefault();
      void focusRow(focusIndex - 1);
    }
  }

  function onUndo() {
    undoInboxResponse();
  }
</script>

<svelte:window onkeydown={onKeydown} />

<WorkspacePageShell>
  <WorkspacePageHeader title="Decide">
    {#snippet meta()}
      <span class="text-meta text-fg-muted">
        {#if $brief.data}
          <span data-decide-count>{decisions.length}</span>
          {decisions.length === 1 ? "decision" : "decisions"}
          {#if freshness}
            · <span data-source="computed:generated_at">{freshness}</span>
          {/if}
        {/if}
      </span>
    {/snippet}
  </WorkspacePageHeader>

  {#if initiative}
    <p class="text-meta text-fg-muted" data-decide-filter>
      Only decisions for <span class="text-fg">{filterTitle || "this initiative"}</span>.
      <a class="ui-prose-link" href="?">Show all</a>
    </p>
  {/if}

  {#if $brief.error && $brief.data}
    <div
      class="flex flex-wrap items-center gap-2 rounded-md border border-warn bg-warn-soft px-3 py-2 text-meta text-fg"
      role="status"
      data-decide-stale
    >
      <span>Couldn’t refresh. Showing what loaded {freshness.replace(/^as of /, "")}.</span>
      <button
        type="button"
        class="ui-prose-link"
        onclick={() => brief.refresh({ force: true })}>Retry</button
      >
    </div>
  {/if}

  {#if !$brief.data && $brief.error}
    <StateError
      title="Decisions didn’t load"
      message="Check your connection and try again."
      retrying={$brief.refreshing}
      onretry={() => brief.refresh({ force: true })}
    />
  {:else if !$brief.data}
    <SkeletonList rows={4} />
  {:else if !decisions.length}
    <p class="py-10 text-center text-meta text-fg-muted" data-decide-empty>
      Nothing is waiting on you.
    </p>
  {:else}
    <div class="space-y-2" bind:this={listEl} data-decide-list>
      {#each decisions as decision, index (decision.ref)}
        <DecisionCard
          {decision}
          variant="row"
          {now}
          focused={index === focusIndex}
        />
      {/each}
    </div>
    {#if $brief.data.truncated?.decisions}
      <p class="text-meta text-fg-muted">
        Showing the first {decisions.length}. Answer some to see the rest.
      </p>
    {/if}
  {/if}
</WorkspacePageShell>

<InboxUndoToast {onUndo} />
