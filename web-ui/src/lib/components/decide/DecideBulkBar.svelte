<script>
  import { onMount } from "svelte";
  import { mutationOutbox, retryMutation } from "$lib/mutationOutbox.js";
  import {
    canSkip,
    flushSkip,
    pendingSkip,
    skipDecisions,
    skipSaveFailures,
    stuckSkips,
    undoSkip,
  } from "$lib/decideSkip.js";

  /**
   * Select several decisions, or none, and skip them without deciding. When
   * the backlog builds up, Skip all clears it in one press: each requester is
   * told nothing was decided, and may ask again later. Skips wait behind an
   * Undo first, then go out through the durable outbox.
   *
   * `selected` is a Set of inbox item ids the page owns, so row checkboxes
   * and this bar agree.
   */
  let {
    decisions = [],
    selected = $bindable(new Set()),
    href = "",
    onskip = () => {},
  } = $props();

  let skippable = $derived(decisions.filter(canSkip));
  let picked = $derived(skippable.filter((d) => selected.has(d.inboxItemId)));
  let held = $derived(decisions.length - skippable.length);
  let all = $derived(
    skippable.length > 0 && picked.length === skippable.length,
  );
  let some = $derived(picked.length > 0 && !all);
  let stuck = $derived(stuckSkips($mutationOutbox));
  let now = $state(Date.now());
  // The clock restarts with each skip, so the countdown starts at 5.
  $effect(() => {
    if ($pendingSkip) now = Date.now();
  });
  let left = $derived(
    $pendingSkip
      ? Math.max(0, Math.ceil(($pendingSkip.deadline - now) / 1000))
      : 0,
  );

  function toggleAll() {
    selected = all
      ? new Set()
      : new Set(skippable.map((decision) => decision.inboxItemId));
  }

  function skip(list) {
    const count = skipDecisions(list, { href });
    if (!count) return;
    selected = new Set();
    onskip(count);
  }

  function retryStuck() {
    for (const entry of stuck) void retryMutation(entry.key).catch(() => {});
  }

  onMount(() => {
    const clock = setInterval(() => (now = Date.now()), 500);
    // A skip waiting behind Undo is saved when the reader leaves, not lost.
    const leave = () => void flushSkip();
    window.addEventListener("pagehide", leave);
    return () => {
      clearInterval(clock);
      window.removeEventListener("pagehide", leave);
      leave();
    };
  });
</script>

{#if skippable.length || $pendingSkip || stuck.length || $skipSaveFailures}
  <div
    class="flex min-h-11 flex-wrap items-center gap-x-3 gap-y-2 rounded-md border border-line-subtle bg-panel px-3 py-2 text-meta"
    data-decide-bulk
  >
    {#if skippable.length}
      <label class="inline-flex min-h-8 items-center gap-2 text-fg-muted">
        <input
          type="checkbox"
          checked={all}
          indeterminate={some}
          onchange={toggleAll}
          data-decide-select-all
          aria-label="Select all decisions"
        />
        {picked.length ? `${picked.length} selected` : "Select"}
      </label>
      {#if picked.length}
        <button
          type="button"
          class="ui-btn-secondary"
          data-decide-skip-selected
          onclick={() => skip(picked)}>Skip {picked.length}</button
        >
        <button
          type="button"
          class="text-micro text-fg-muted hover:text-fg"
          onclick={() => (selected = new Set())}>Clear</button
        >
      {:else}
        <button
          type="button"
          class="ui-btn-secondary"
          data-decide-skip-all
          onclick={() => skip(skippable)}
          >Skip all{skippable.length > 1 ? ` ${skippable.length}` : ""}</button
        >
      {/if}
      <span class="text-micro text-fg-subtle">
        Skipping decides nothing. Agents are told, and can ask again later.
        {#if held}{held} {held === 1 ? "needs" : "need"} an answer here.{/if}
      </span>
    {/if}
    {#if stuck.length}
      <span
        class="text-micro text-warn-text"
        role="status"
        data-decide-skip-stuck
      >
        {stuck.length}
        {stuck.length === 1 ? "skip" : "skips"} didn’t go through.
        <button type="button" class="ui-prose-link" onclick={retryStuck}
          >Retry</button
        >
      </span>
    {/if}
    {#if $skipSaveFailures}
      <span class="text-micro text-danger-text" role="alert">
        Couldn’t save {$skipSaveFailures}
        {$skipSaveFailures === 1 ? "skip" : "skips"} on this device, so they were
        not sent.
      </span>
    {/if}
  </div>
{/if}

{#if $pendingSkip}
  <div
    class="pointer-events-none fixed inset-x-0 bottom-[calc(4.75rem+env(safe-area-inset-bottom,0px))] z-40 flex justify-center px-4 lg:bottom-6"
  >
    <div
      class="pointer-events-auto flex max-w-full items-center gap-3 rounded-md border border-line-strong bg-panel px-3 py-2 text-meta text-fg shadow-[var(--shadow-menu)]"
      role="status"
      aria-live="polite"
      data-decide-skip-toast
    >
      <span class="min-w-0 truncate"
        >Skipped {$pendingSkip.ids.length}
        {$pendingSkip.ids.length === 1 ? "decision" : "decisions"} without deciding.</span
      >
      <button
        type="button"
        class="shrink-0 text-micro font-medium text-accent-text hover:underline"
        data-decide-skip-undo
        onclick={undoSkip}>Undo{left ? ` (${left})` : ""}</button
      >
    </div>
  </div>
{/if}
