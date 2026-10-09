<script>
  import { inboxNeedsYouCount, startInboxCount } from "$lib/inboxCount.js";

  import { readerScope } from "$lib/readerScope.js";

  /**
   * The Needs you count beside the Inbox nav item. Zero renders nothing: an
   * empty inbox has no badge, not a grey "0".
   */
  let { workspace = "", enabled = false, variant = "sidebar" } = $props();

  $effect(() => {
    void $readerScope;
    if (!enabled || !workspace) return;
    return startInboxCount(workspace);
  });

  let count = $derived(
    $inboxNeedsYouCount.workspace === workspace
      ? $inboxNeedsYouCount.count
      : null,
  );
  let text = $derived(
    count && count > 0
      ? `${count > 99 ? "99+" : count}${$inboxNeedsYouCount.truncated ? "+" : ""}`
      : "",
  );
</script>

{#if text}
  {#if variant === "bottom"}
    <span
      class="absolute left-1/2 top-1 ml-1.5 min-w-4 rounded-full bg-warn px-1 text-center text-[10px] font-semibold leading-4 text-bg tabular-nums"
      aria-label={`${count} need you`}
      data-inbox-nav-count>{text}</span
    >
  {:else}
    <span
      class="ml-auto shrink-0 rounded-full bg-warn-soft px-1.5 text-micro font-medium tabular-nums text-warn-text"
      aria-label={`${count} need you`}
      data-inbox-nav-count>{text}</span
    >
  {/if}
{/if}
