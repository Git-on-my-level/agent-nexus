<script>
  import { onMount } from "svelte";

  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";

  let artifacts = $state([]);
  let loading = $state(false);
  let error = $state("");
  let filtersOpen = $state(false);
  let filters = $state({
    kind: "",
    thread_id: "",
    created_after: "",
    created_before: "",
  });

  onMount(async () => {
    await loadArtifacts();
  });

  function toIsoOrEmpty(value) {
    if (!value) return "";
    const parsed = Date.parse(String(value));
    if (Number.isNaN(parsed)) return "";
    return new Date(parsed).toISOString();
  }

  function buildArtifactQuery() {
    return {
      kind: filters.kind.trim(),
      thread_id: filters.thread_id.trim(),
      created_after: toIsoOrEmpty(filters.created_after),
      created_before: toIsoOrEmpty(filters.created_before),
    };
  }

  async function loadArtifacts() {
    loading = true;
    error = "";
    try {
      artifacts =
        (await coreClient.listArtifacts(buildArtifactQuery())).artifacts ?? [];
    } catch (e) {
      error = `Failed to load artifacts: ${e instanceof Error ? e.message : String(e)}`;
      artifacts = [];
    } finally {
      loading = false;
    }
  }

  async function applyFilters() {
    await loadArtifacts();
  }
  async function clearFilters() {
    filters = {
      kind: "",
      thread_id: "",
      created_after: "",
      created_before: "",
    };
    await loadArtifacts();
  }

  function kindColor(kind) {
    const colors = {
      work_order: "bg-blue-50 text-blue-700",
      receipt: "bg-emerald-50 text-emerald-700",
      review: "bg-purple-50 text-purple-700",
      doc: "bg-amber-50 text-amber-700",
    };
    return colors[kind] ?? "bg-gray-100 text-gray-600";
  }
</script>

<div class="flex items-center justify-between">
  <h1 class="text-lg font-semibold text-gray-900">Artifacts</h1>
  <button
    class="rounded-md px-3 py-1.5 text-xs font-medium text-gray-600 transition-colors hover:bg-gray-100"
    onclick={() => (filtersOpen = !filtersOpen)}
    type="button"
  >
    {filtersOpen ? "Hide filters" : "Filters"}
  </button>
</div>

{#if filtersOpen}
  <form
    class="mt-3 rounded-lg border border-gray-200 bg-white p-3"
    onsubmit={(event) => {
      event.preventDefault();
      applyFilters();
    }}
  >
    <div class="grid gap-2 sm:grid-cols-2">
      <label class="text-xs text-gray-500"
        >Kind <input
          bind:value={filters.kind}
          class="mt-1 w-full rounded border border-gray-200 px-2 py-1 text-sm"
          placeholder="work_order, receipt, review, doc..."
        /></label
      >
      <label class="text-xs text-gray-500"
        >Thread ID <input
          bind:value={filters.thread_id}
          class="mt-1 w-full rounded border border-gray-200 px-2 py-1 text-sm"
          placeholder="thread-onboarding"
        /></label
      >
      <label class="text-xs text-gray-500"
        >Created after <input
          bind:value={filters.created_after}
          class="mt-1 w-full rounded border border-gray-200 px-2 py-1 text-sm"
          type="datetime-local"
        /></label
      >
      <label class="text-xs text-gray-500"
        >Created before <input
          bind:value={filters.created_before}
          class="mt-1 w-full rounded border border-gray-200 px-2 py-1 text-sm"
          type="datetime-local"
        /></label
      >
    </div>
    <div class="mt-2 flex gap-2">
      <button
        class="rounded bg-gray-900 px-3 py-1 text-xs font-medium text-white hover:bg-gray-700"
        type="submit">Apply</button
      >
      <button
        class="rounded px-3 py-1 text-xs text-gray-500 hover:bg-gray-100"
        onclick={clearFilters}
        type="button">Clear</button
      >
    </div>
  </form>
{/if}

{#if error}
  <p class="mt-3 rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">
    {error}
  </p>
{:else if !loading && artifacts.length === 0}
  <p class="mt-6 text-sm text-gray-400">
    No artifacts match the current filters.
  </p>
{/if}

{#if artifacts.length > 0}
  <div class="mt-4 overflow-hidden rounded-lg border border-gray-200 bg-white">
    {#each artifacts as artifact, i}
      <a
        class="flex items-center gap-3 border-b border-gray-100 px-4 py-3 transition-colors hover:bg-gray-50 {i ===
        artifacts.length - 1
          ? 'border-b-0'
          : ''}"
        href={`/artifacts/${artifact.id}`}
      >
        <span
          class={`shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium ${kindColor(artifact.kind)}`}
          >{artifact.kind}</span
        >
        <div class="min-w-0 flex-1">
          <p class="truncate text-sm font-medium text-gray-900">
            {artifact.summary || artifact.id}
          </p>
          <div class="mt-0.5 flex items-center gap-2 text-xs text-gray-400">
            <span>{artifact.created_by || "unknown"}</span>
            <span>·</span>
            <span>{formatTimestamp(artifact.created_at) || "—"}</span>
            {#if artifact.thread_id}
              <span>·</span>
              <span class="truncate">{artifact.thread_id}</span>
            {/if}
          </div>
        </div>
        {#if (artifact.refs ?? []).length > 0}
          <span class="shrink-0 text-xs text-gray-400"
            >{artifact.refs.length} ref{artifact.refs.length === 1
              ? ""
              : "s"}</span
          >
        {/if}
      </a>
    {/each}
  </div>
{/if}
