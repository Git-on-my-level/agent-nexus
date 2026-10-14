<script>
  import { briefStore, mountBrief } from "$lib/briefStore.js";
  import { readerScope } from "$lib/readerScope.js";
  import { inboxResponseOverlay } from "$lib/inboxResponseQueue.js";

  /**
   * How many decisions wait on the reader, beside Decide. It reads the same
   * `brief.v1` snapshot Decide renders (one bounded read, shared and cached),
   * so the badge and the list cannot disagree. Answers still inside their
   * undo window are already left out. Zero renders nothing.
   */
  let { workspace = "", enabled = false, variant = "sidebar" } = $props();

  // The store belongs to one reader; a switch hands back the new reader's.
  let brief = $derived.by(() => {
    void $readerScope;
    return briefStore();
  });
  $effect(() => {
    void $readerScope;
    if (!enabled || !workspace) return;
    return mountBrief();
  });

  let count = $derived(
    ($brief?.data?.decisions ?? []).filter(
      (decision) => !$inboxResponseOverlay[decision.inboxItemId],
    ).length,
  );
  // The exact number, never "99+": a capped count hides how much waits.
  let text = $derived(enabled && count > 0 ? String(count) : "");
</script>

{#if text}
  {#if variant === "bottom"}
    <span
      class="absolute left-1/2 top-1 ml-1.5 min-w-4 rounded-full bg-warn px-1 text-center text-[10px] font-semibold leading-4 text-bg tabular-nums"
      aria-label={`${count} waiting on you`}
      data-decide-nav-count>{text}</span
    >
  {:else}
    <span
      class="ml-auto shrink-0 rounded-full bg-warn-soft px-1.5 text-micro font-medium tabular-nums text-warn-text"
      aria-label={`${count} waiting on you`}
      data-decide-nav-count>{text}</span
    >
  {/if}
{/if}
