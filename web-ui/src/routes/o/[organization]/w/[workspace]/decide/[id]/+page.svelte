<script>
  import { onMount } from "svelte";
  import { page } from "$app/stores";
  import { briefStore, mountBrief } from "$lib/briefStore.js";
  import { findDecision } from "$lib/brief.js";
  import { undoInboxResponse } from "$lib/inboxResponseQueue.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { usePageTitle } from "$lib/pageTitle.svelte.js";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import SkeletonList from "$lib/components/state/SkeletonList.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import DecisionCard from "$lib/components/decide/DecisionCard.svelte";
  import InboxUndoToast from "$lib/components/inbox/InboxUndoToast.svelte";

  /**
   * A decision's permalink: the same DecisionCard the list renders, full
   * width with its body. Its reply draft shares the list's durable key, so
   * text typed in either place is here in the other.
   */
  const brief = briefStore();
  let now = $state(Date.now());
  let id = $derived(decodeURIComponent($page.params.id ?? ""));
  let decision = $derived(findDecision($brief.data, id));
  let href = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  usePageTitle(() => decision?.title || "Decision");

  onMount(() => {
    const stop = mountBrief();
    const clock = setInterval(() => (now = Date.now()), 30_000);
    return () => {
      stop();
      clearInterval(clock);
    };
  });
</script>

<WorkspacePageShell>
  <a class="text-meta text-fg-muted hover:text-fg" href={href("/decide")}
    >← Decide</a
  >
  {#if decision}
    <DecisionCard {decision} variant="full" {now} />
    {#if decision.subject}
      <p class="text-meta text-fg-muted">
        About
        {#if decision.subject.ref.startsWith("card:")}
          <a
            class="ui-prose-link"
            href={href(
              `/initiatives/${encodeURIComponent(decision.subject.ref.slice(5))}`,
            )}>{decision.subject.title || "this initiative"}</a
          >
        {:else}
          <span class="text-fg"
            >{decision.subject.title || decision.subject.ref}</span
          >
        {/if}
      </p>
    {/if}
  {:else if !$brief.data && $brief.error}
    <StateError
      title="This decision didn’t load"
      message="Check your connection and try again."
      retrying={$brief.refreshing}
      onretry={() => brief.refresh({ force: true })}
    />
  {:else if !$brief.data}
    <SkeletonList rows={1} />
  {:else}
    <p class="py-10 text-center text-meta text-fg-muted" data-decide-gone>
      This ask is no longer open.
      <a class="ui-prose-link" href={href("/decide")}>Back to Decide</a>
    </p>
  {/if}
</WorkspacePageShell>

<InboxUndoToast onUndo={() => undoInboxResponse()} />
