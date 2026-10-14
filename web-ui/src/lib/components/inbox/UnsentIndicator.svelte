<script>
  import { inboxUnsentResponses } from "$lib/inboxResponseQueue.js";
  import { mutationOutbox } from "$lib/mutationOutbox.js";
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  import InboxSendStatus from "./InboxSendStatus.svelte";
  import { appPath } from "$lib/workspacePaths.js";

  let open = $state(false);
  $effect(() => {
    if (!$inboxUnsentResponses.length && !$mutationOutbox.length) open = false;
  });
</script>

{#if $inboxUnsentResponses.length + $mutationOutbox.length}
  <details
    class="m-3 self-end shrink-0 max-h-[70vh] w-fit open:w-80 max-w-[calc(100vw-2rem)] overflow-auto rounded-md border border-line bg-panel p-3 shadow-[var(--shadow-menu)]"
    bind:open
    data-unsent-indicator
  >
    <summary class="cursor-pointer text-meta font-medium text-fg"
      >{$inboxUnsentResponses.length + $mutationOutbox.length} unsent</summary
    >
    {#if open}
      <div class="mt-3 space-y-3">
        {#each $inboxUnsentResponses as entry (entry.request.idempotency_key)}
          <div>
            <a
              class="text-meta text-accent-text hover:underline"
              href={appPath(`/decide/${encodeURIComponent(entry.itemId)}`)}
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
