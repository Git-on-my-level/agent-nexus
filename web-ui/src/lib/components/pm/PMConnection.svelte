<script>
  import { onMount } from "svelte";
  import { coreClient } from "$lib/coreClient";
  let presence = $state(null);
  let now = $state(Date.now());
  onMount(() => {
    let alive = true,
      busy = false;
    async function refresh() {
      if (busy) return;
      busy = true;
      try {
        const result = await coreClient.getPmPresence();
        if (alive) presence = result;
      } catch {
        /* Older cores and failed reads must not imply disconnected. */
      } finally {
        busy = false;
      }
    }
    void refresh();
    const timer = setInterval(() => {
      now = Date.now();
      void refresh();
    }, 15000);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  });
  const disconnected = $derived(
    presence &&
      (!presence.connected ||
        (presence.last_seen_at &&
          now - Date.parse(presence.last_seen_at) > 90000)),
  );
</script>

{#if disconnected}
  <aside
    class="rounded-lg border border-border bg-bg-soft p-3 text-body"
    aria-label="PM connection"
  >
    <p class="font-medium">No PM connected</p>
    <p class="text-fg-muted">
      Your PM runs on your computer. Install it there to answer questions and
      handle PM-routed asks.
    </p>
    <code class="text-meta">anx pm install</code>
  </aside>
{/if}
