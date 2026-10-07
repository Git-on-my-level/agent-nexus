<script>
  import {
    clearPendingAccessCount,
    pendingAccessCount,
    pendingAccessLabel,
    startPendingAccessCount,
  } from "$lib/pendingAccessCount.js";

  /**
   * How many access requests wait for a decision, beside the Access item and
   * on the account menu trigger that hides it. One number; the hover title
   * says what the number is. Zero renders nothing: a workspace with nobody
   * waiting has no badge, not a grey "0". Neither does a reader who may not
   * decide access — they could never act on it.
   *
   * `enabled` gates the rendering, not just the polling. It goes false when
   * the session stops being a person's, and the number behind it was read
   * from an inventory that reader may no longer see; left on screen it would
   * never be refreshed or corrected, so it is cleared rather than frozen.
   *
   * On the trigger the number is decorative: the button carries an explicit
   * `aria-label`, which suppresses any name a descendant would contribute, so
   * the layout folds the count into that label instead (`pendingAccessLabel`).
   */
  let { workspace = "", enabled = false, variant = "sidebar" } = $props();

  $effect(() => {
    if (!enabled || !workspace) {
      clearPendingAccessCount();
      return;
    }
    const key = workspace;
    let stop = () => {};
    // Access owns its primary reads; this secondary badge follows first paint.
    const timer = setTimeout(() => {
      stop = startPendingAccessCount(key);
    }, 1500);
    return () => {
      clearTimeout(timer);
      stop();
    };
  });

  let snapshot = $derived(
    enabled && $pendingAccessCount.workspace === workspace
      ? $pendingAccessCount
      : { count: null, forbidden: false },
  );
  let count = $derived(snapshot.forbidden ? 0 : (snapshot.count ?? 0));
  let text = $derived(count > 0 ? (count > 99 ? "99+" : String(count)) : "");
  let label = $derived(pendingAccessLabel(count));
</script>

{#if text}
  {#if variant === "trigger"}
    <span
      class="shrink-0 rounded-full bg-warn px-1.5 text-[10px] font-semibold leading-4 text-bg tabular-nums"
      aria-hidden="true"
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
