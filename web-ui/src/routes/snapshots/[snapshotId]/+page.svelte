<script>
  import { page } from "$app/stores";
  import { coreClient } from "$lib/coreClient";

  let snapshotId = $derived($page.params.snapshotId);
  let snapshot = $state(null);
  let loading = $state(false);
  let loadError = $state("");
  let lastLoadedSnapshotId = $state("");

  $effect(() => {
    if (!snapshotId || snapshotId === lastLoadedSnapshotId) {
      return;
    }

    lastLoadedSnapshotId = snapshotId;
    void loadSnapshot(snapshotId);
  });

  async function loadSnapshot(id) {
    loading = true;
    loadError = "";
    snapshot = null;

    try {
      const response = await coreClient.getSnapshot(id);
      snapshot = response?.snapshot ?? null;

      if (!snapshot) {
        loadError = `Snapshot ${id} was not returned by oar-core.`;
      }
    } catch (error) {
      loadError = `Failed to load snapshot: ${error instanceof Error ? error.message : String(error)}`;
    } finally {
      loading = false;
    }
  }
</script>

<h1 class="text-lg font-semibold text-gray-900">
  Snapshot Detail: {snapshotId}
</h1>

{#if loading}
  <p class="mt-3 text-sm text-gray-500">Loading snapshot...</p>
{:else if loadError}
  <p class="mt-3 rounded bg-red-50 px-3 py-2 text-sm text-red-700">
    {loadError}
  </p>
{:else if snapshot}
  <div class="mt-3 rounded-lg border border-gray-200 bg-white">
    <div class="border-b border-gray-100 px-4 py-2.5">
      <h2 class="text-xs font-semibold uppercase tracking-wider text-gray-400">
        Raw Snapshot JSON
      </h2>
    </div>
    <pre
      class="overflow-auto px-4 py-3 text-[11px] text-gray-700">{JSON.stringify(
        snapshot,
        null,
        2,
      )}</pre>
  </div>
{/if}
