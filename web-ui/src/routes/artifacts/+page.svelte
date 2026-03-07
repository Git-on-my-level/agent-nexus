<script>
  import { onMount } from "svelte";

  import RefLink from "$lib/components/RefLink.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";

  const KIND_LABELS = {
    work_order: "Work Order",
    receipt: "Receipt",
    review: "Review",
    doc: "Document",
    evidence: "Evidence",
    log: "Log",
  };

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

  function kindLabel(kind) {
    return KIND_LABELS[String(kind ?? "").trim()] ?? String(kind ?? "Artifact");
  }

  function kindDescription(kind) {
    if (kind === "work_order") return "Execution plan and acceptance criteria";
    if (kind === "receipt") return "Work completion evidence and verification";
    if (kind === "review") return "Human decision on receipt quality";
    if (kind === "doc") return "Readable document artifact";
    if (kind === "evidence") return "Supporting evidence and logs";
    if (kind === "log") return "Operational activity record";
    return "Artifact payload";
  }

  function kindColor(kind) {
    const styles = {
      work_order: "text-blue-400 bg-blue-500/10",
      receipt: "text-emerald-400 bg-emerald-500/10",
      review: "text-amber-400 bg-amber-500/10",
      doc: "text-fuchsia-400 bg-fuchsia-500/10",
      evidence: "text-gray-600 bg-gray-200",
      log: "text-teal-400 bg-teal-500/10",
    };
    return styles[kind] ?? "text-gray-600 bg-gray-200";
  }

  function rowHeading(artifact) {
    const summary = String(artifact?.summary ?? "").trim();
    if (summary) return summary;
    return `${kindLabel(artifact?.kind)} artifact`;
  }

  function refPreview(artifact) {
    const refs = Array.isArray(artifact?.refs) ? artifact.refs : [];
    return refs.slice(0, 3);
  }
</script>

<div class="flex items-center justify-between mb-4">
  <h1 class="text-lg font-semibold text-gray-900">Artifacts</h1>
  <button
    class="inline-flex items-center gap-1.5 rounded-md border border-gray-200 bg-gray-100 px-2.5 py-1.5 text-[12px] font-medium text-gray-600 transition-colors hover:bg-gray-200"
    onclick={() => (filtersOpen = !filtersOpen)}
    type="button"
  >
    <svg
      class="h-3.5 w-3.5"
      fill="none"
      viewBox="0 0 24 24"
      stroke="currentColor"
      stroke-width="2"
    >
      <path
        stroke-linecap="round"
        stroke-linejoin="round"
        d="M3 4a1 1 0 011-1h16a1 1 0 011 1v2.586a1 1 0 01-.293.707l-6.414 6.414a1 1 0 00-.293.707V17l-4 4v-6.586a1 1 0 00-.293-.707L3.293 7.293A1 1 0 013 6.586V4z"
      />
    </svg>
    Filter
  </button>
</div>

{#if filtersOpen}
  <form
    class="mb-4 rounded-md border border-gray-200 bg-gray-100 p-3"
    onsubmit={(event) => {
      event.preventDefault();
      void applyFilters();
    }}
  >
    <div class="grid gap-3 sm:grid-cols-2">
      <label class="text-[12px] font-medium text-gray-500">
        Kind
        <input
          bind:value={filters.kind}
          class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-1.5 text-[13px] transition-colors focus:bg-gray-100"
          placeholder="work_order, receipt, review..."
        />
      </label>
      <label class="text-[12px] font-medium text-gray-500">
        Thread ID
        <input
          bind:value={filters.thread_id}
          class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-1.5 text-[13px] transition-colors focus:bg-gray-100"
          placeholder="thread-onboarding"
        />
      </label>
      <label class="text-[12px] font-medium text-gray-500">
        Created after
        <input
          bind:value={filters.created_after}
          class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-1.5 text-[13px] transition-colors focus:bg-gray-100"
          type="datetime-local"
        />
      </label>
      <label class="text-[12px] font-medium text-gray-500">
        Created before
        <input
          bind:value={filters.created_before}
          class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-1.5 text-[13px] transition-colors focus:bg-gray-100"
          type="datetime-local"
        />
      </label>
    </div>
    <div class="mt-3 flex gap-1.5">
      <button
        class="rounded-md bg-gray-200 px-3 py-1.5 text-[12px] font-medium text-gray-900 hover:bg-gray-300"
        type="submit">Apply</button
      >
      <button
        class="rounded-md border border-gray-200 bg-gray-100 px-3 py-1.5 text-[12px] font-medium text-gray-600 hover:bg-gray-200"
        onclick={clearFilters}
        type="button">Clear</button
      >
    </div>
  </form>
{/if}

{#if error}
  <div class="mb-4 rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
    {error}
  </div>
{:else if !loading && artifacts.length === 0}
  <div class="mt-8 text-center">
    <p class="text-[13px] font-medium text-gray-500">No matching artifacts</p>
    <p class="mt-1 text-[13px] text-gray-400">
      Try adjusting filters or clearing the current view.
    </p>
  </div>
{/if}

{#if artifacts.length > 0}
  <div
    class="space-y-px rounded-md border border-gray-200 bg-gray-100 overflow-hidden"
  >
    {#each artifacts as artifact, i}
      <a
        class="block px-4 py-3 transition-colors hover:bg-gray-200 {i > 0
          ? 'border-t border-gray-200'
          : ''}"
        href={`/artifacts/${artifact.id}`}
      >
        <div class="flex items-start justify-between gap-3">
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              <span
                class="inline-flex rounded px-1.5 py-0.5 text-[11px] font-semibold {kindColor(
                  artifact.kind,
                )}"
              >
                {kindLabel(artifact.kind)}
              </span>
              <span class="text-[11px] text-gray-400"
                >{kindDescription(artifact.kind)}</span
              >
            </div>
            <p class="mt-1 truncate text-[13px] font-medium text-gray-900">
              {rowHeading(artifact)}
            </p>
            <p class="text-[11px] text-gray-400">
              Created {formatTimestamp(artifact.created_at) || "—"} by {artifact.created_by ||
                "unknown"}
            </p>
          </div>
          <span class="shrink-0 text-[11px] text-gray-300">
            {(artifact.refs ?? []).length} ref{(artifact.refs ?? []).length ===
            1
              ? ""
              : "s"}
          </span>
        </div>

        {#if refPreview(artifact).length > 0 || artifact.thread_id}
          <div class="mt-1.5 flex flex-wrap items-center gap-1.5 text-[11px]">
            {#if artifact.thread_id}
              <RefLink
                humanize
                labelHints={{
                  [`thread:${artifact.thread_id}`]: "Related thread",
                }}
                refValue={`thread:${artifact.thread_id}`}
                showRaw
                threadId={artifact.thread_id}
              />
            {/if}
            {#each refPreview(artifact) as refValue}
              <RefLink
                humanize
                {refValue}
                showRaw
                threadId={artifact.thread_id}
              />
            {/each}
            {#if (artifact.refs ?? []).length > 3}
              <span class="text-[11px] text-gray-300"
                >+{artifact.refs.length - 3} more</span
              >
            {/if}
          </div>
        {/if}
      </a>
    {/each}
  </div>
{/if}
