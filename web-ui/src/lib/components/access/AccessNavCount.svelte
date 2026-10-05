<script>
  import {
    pendingAccessCount,
    startPendingAccessCount,
  } from "$lib/pendingAccessCount.js";

  /**
   * How many access requests wait for a decision, beside the Access item and
   * on the account menu trigger that hides it. One number; the hover title
   * says what the number is. Zero renders nothing: a workspace with nobody
   * waiting has no badge, not a grey "0". Neither does a reader who may not
   * decide access — they would never be able to act on it.
   */
  let { workspace = "", enabled = false, variant = "sidebar" } = $props();

  $effect(() => {
    if (!enabled || !workspace) return;
    return startPendingAccessCount(workspace);
  });

  let count = $derived(
    $pendingAccessCount.workspace === workspace
      ? ($pendingAccessCount.count ?? 0)
      : 0,
  );
  let text = $derived(count > 0 ? (count > 99 ? "99+" : String(count)) : "");
  let label = $derived(
    count === 1
      ? "1 access request waiting"
      : `${count} access requests waiting`,
  );
</script>

{#if text}
  {#if variant === "trigger"}
    <!-- On the account menu trigger, where the Access item is out of sight
         until the menu opens. -->
    <span
      class="shrink-0 rounded-full bg-warn px-1.5 text-[10px] font-semibold leading-4 text-bg tabular-nums"
      aria-label={label}
      title={label}
      data-access-trigger-count>{text}</span
    >
  {:else}
    <span
      class="ml-auto shrink-0 rounded-full bg-warn-soft px-1.5 text-micro font-medium tabular-nums text-warn-text"
      aria-label={label}
      title={label}
      data-access-nav-count>{text}</span
    >
  {/if}
{/if}
