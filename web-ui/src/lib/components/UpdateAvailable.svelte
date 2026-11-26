<script>
  /*
   * A tab left open across a deploy keeps running the build it loaded.
   * Tabs of different builds share one device outbox, so an old tab can meet
   * work it cannot send: on 2026-11-26 a long-open window marked a reader's
   * bulk skips "Unknown saved operation" and stranded them. SvelteKit polls
   * for a new build (`kit.version.pollInterval`); once one is out, the next
   * in-app navigation is a full page load, and the reader can reload now.
   * Saved work survives the reload: the outbox and its journals are durable.
   * A chunk of the loaded build that can no longer be fetched (the same
   * cause) shows its reload prompt here too.
   */
  import { beforeNavigate } from "$app/navigation";
  import { updated } from "$app/stores";
  import { get } from "svelte/store";
  import { staleChunkRecoveryNeeded } from "$lib/chunkRecovery.js";

  beforeNavigate(({ willUnload, to }) => {
    if (get(updated) && !willUnload && to?.url)
      globalThis.location.href = to.url.href;
  });
</script>

{#if $staleChunkRecoveryNeeded}
  <div class="p-6" role="alert" data-workspace-update-error>
    <p>This page could not be loaded. Reload to try again.</p>
    <button
      class="btn-secondary mt-3"
      type="button"
      onclick={() => globalThis.location.reload()}>Retry</button
    >
  </div>
{:else if $updated}
  <div
    class="m-3 mb-0 flex shrink-0 items-center justify-between gap-3 rounded-md border border-line bg-panel px-3 py-2 text-meta text-fg"
    role="status"
    data-update-available
  >
    <span>A newer version of this app is available.</span>
    <button
      type="button"
      class="rounded border border-line px-2 py-1 text-fg"
      onclick={() => globalThis.location.reload()}>Reload</button
    >
  </div>
{/if}
