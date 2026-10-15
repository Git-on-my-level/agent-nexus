<script>
  import {
    inboxResponseToast,
    inboxUnsentResponses,
  } from "$lib/inboxResponseQueue.js";
  import { mutationOutbox } from "$lib/mutationOutbox.js";
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
  <details
    class="m-3 self-end shrink-0 max-h-[70vh] w-fit open:w-80 max-w-[calc(100vw-2rem)] overflow-auto rounded-md border border-line bg-panel p-3 shadow-[var(--shadow-menu)]"
    bind:open
    data-unsent-indicator
  >
    <summary class="cursor-pointer text-meta font-medium text-fg"
      >{count} unsent</summary
    >
    {#if open}
      <div class="mt-3 space-y-3">
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
