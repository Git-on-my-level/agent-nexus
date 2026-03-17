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

<h1 class="text-lg font-semibold text-[var(--ui-text)]">
  Snapshot: <span class="font-mono text-[var(--ui-text-muted)]"
    >{snapshotId}</span
  >
</h1>

{#if loading}
  <div
    class="mt-6 flex items-center gap-2 text-[13px] text-[var(--ui-text-muted)]"
  >
    <svg class="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
      <circle
        class="opacity-25"
        cx="12"
        cy="12"
        r="10"
        stroke="currentColor"
        stroke-width="4"
      ></circle>
      <path
        class="opacity-75"
        fill="currentColor"
        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
      ></path>
    </svg>
    Loading snapshot...
  </div>
{:else if loadError}
  <div
    class="mt-3 flex items-start gap-2 rounded-md bg-red-500/10 px-4 py-3 text-[13px] text-red-400"
  >
    <svg
      class="mt-0.5 h-4 w-4 shrink-0 text-red-400"
      fill="none"
      viewBox="0 0 24 24"
      stroke="currentColor"
      stroke-width="2"
    >
      <path
        stroke-linecap="round"
        stroke-linejoin="round"
        d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L4.082 16.5c-.77.833.192 2.5 1.732 2.5z"
      />
    </svg>
    {loadError}
  </div>
{:else if snapshot}
  <div
    class="mt-4 rounded-xl border border-[var(--ui-border)] bg-[var(--ui-panel)]"
  >
    <div class="border-b border-[var(--ui-border-subtle)] px-5 py-3">
      <h2 class="text-[13px] font-medium text-[var(--ui-text)]">
        Raw Snapshot JSON
      </h2>
    </div>
    <pre
      class="overflow-auto px-5 py-4 text-[11px] text-[var(--ui-text-muted)]">{JSON.stringify(
        snapshot,
        null,
        2,
      )}</pre>
  </div>
{/if}
