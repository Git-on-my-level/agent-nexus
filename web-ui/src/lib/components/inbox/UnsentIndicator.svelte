<script>
  import { inboxUnsentResponses } from "$lib/inboxResponseQueue.js";
  import InboxSendStatus from "./InboxSendStatus.svelte";
  import { appPath } from "$lib/workspacePaths.js";

  let open = $state(false);
  $effect(() => {
    if (!$inboxUnsentResponses.length) open = false;
  });
</script>

{#if $inboxUnsentResponses.length}
  <details
    class="fixed right-4 top-16 z-40 max-h-[70vh] w-fit open:w-80 max-w-[calc(100vw-2rem)] overflow-auto rounded-md border border-line bg-panel p-3 shadow-[var(--shadow-menu)]"
    bind:open
    data-unsent-indicator
  >
    <summary class="cursor-pointer text-meta font-medium text-fg"
      >{$inboxUnsentResponses.length} unsent</summary
    >
    <!--
      Only while it is open. A closed `<details>` still lays its children
      out, and layout audit measures those nodes — the collapsed chip would
      fail geometry checks from off-screen status chrome.
    -->
    {#if open}
      <div class="mt-3 space-y-3">
        {#each $inboxUnsentResponses as entry (entry.request.idempotency_key)}
          <div>
            <a
              class="text-meta text-accent-text hover:underline"
              href={appPath(`/inbox/${encodeURIComponent(entry.itemId)}`)}
              >{entry.item?.title || "Open saved response"}</a
            >
            <InboxSendStatus itemId={entry.itemId} />
          </div>
        {/each}
      </div>
    {/if}
  </details>
{/if}
