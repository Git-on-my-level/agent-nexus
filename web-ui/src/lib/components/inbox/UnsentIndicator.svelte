<script>
  import {
    inboxResponseToast,
    inboxUnsentResponses,
  } from "$lib/inboxResponseQueue.js";
  import { mutationOutbox, retryMutation } from "$lib/mutationOutbox.js";
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  import InboxSendStatus from "./InboxSendStatus.svelte";
  import { workspacePath } from "$lib/workspacePaths.js";

  let open = $state(false);

  /*
   * An answer still inside its undo window is already on screen in the undo
   * toast; listing it here too says the same thing twice. Everything else
   * that goes out within a moment (a move, a PM answer) never needs this
   * chip, so it appears only once something has waited a little while, and
   * the page does not jump on every answer.
   */
  const SETTLE_MS = 2_000;
  let responses = $derived(
    $inboxUnsentResponses.filter(
      (entry) =>
        !(
          $inboxResponseToast?.state === "pending" &&
          $inboxResponseToast.itemId === entry.itemId
        ),
    ),
  );
  let count = $derived(responses.length + $mutationOutbox.length);
  /** Something needs the reader now: no waiting. */
  let urgent = $derived(
    responses.some((entry) => entry.state === "attention") ||
      $mutationOutbox.some((entry) => entry.state === "attention"),
  );
  let settled = $state(false);
  $effect(() => {
    if (!count) {
      settled = false;
      open = false;
      return;
    }
    if (settled) return;
    const timer = setTimeout(() => (settled = true), SETTLE_MS);
    return () => clearTimeout(timer);
  });

  /*
   * Saved work that needs the reader. Many at once (a bulk skip an older tab
   * stranded, an outage) must not mean one click per row.
   */
  let attention = $derived(
    $mutationOutbox.filter((entry) => entry.state === "attention"),
  );
  let retryingAll = $state(false);
  async function retryAll() {
    retryingAll = true;
    try {
      for (const entry of attention)
        await retryMutation(entry.key, { asNew: !!entry.failure?.expired });
    } finally {
      retryingAll = false;
    }
  }

  /** A saved response belongs to the workspace it was written in. */
  function decideHref(entry) {
    const path = `/decide/${encodeURIComponent(entry.itemId)}`;
    try {
      const [organization, workspace] = JSON.parse(entry.identity);
      return workspacePath(organization, workspace, path);
    } catch {
      return path;
    }
  }
</script>

{#if count && (settled || urgent)}
  <!--
    The chip sits in the page flow; the open list floats over the page
    instead of pushing it down. A long list used to take most of the screen
    above Decide.
  -->
  <details
    class="relative z-30 m-3 self-end shrink-0 rounded-md border border-line bg-panel px-3 py-2 shadow-[var(--shadow-menu)]"
    bind:open
    data-unsent-indicator
  >
    <summary class="cursor-pointer text-meta font-medium text-fg"
      >{count} unsent</summary
    >
    {#if open}
      <div
        class="absolute right-0 top-full mt-1 max-h-[70vh] w-80 max-w-[calc(100vw-2rem)] space-y-3 overflow-auto rounded-md border border-line bg-panel p-3 shadow-[var(--shadow-menu)]"
        data-unsent-list
      >
        {#if attention.length > 1}
          <div class="flex items-center justify-between gap-2 text-meta">
            <span class="text-fg-muted">{attention.length} need you</span>
            <button
              type="button"
              class="rounded border border-line px-2 py-1 text-fg"
              disabled={retryingAll}
              onclick={retryAll}
              data-unsent-retry-all>Retry all</button
            >
          </div>
        {/if}
        {#each responses as entry (entry.request.idempotency_key)}
          <div>
            <a
              class="text-meta text-accent-text hover:underline"
              href={decideHref(entry)}
              >{entry.item?.title || "Open saved response"}</a
            >
            <InboxSendStatus itemId={entry.itemId} />
          </div>
        {/each}
        {#each $mutationOutbox as entry (entry.key)}
          <div>
            <a
              class="text-meta text-accent-text hover:underline"
              href={entry.href}>{entry.title}</a
            >
            <MutationSendStatus {entry} />
          </div>
        {/each}
      </div>
    {/if}
  </details>
{/if}
