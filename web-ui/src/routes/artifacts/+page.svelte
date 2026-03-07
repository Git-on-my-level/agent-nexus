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

  function kindBadge(kind) {
    const styles = {
      work_order: "bg-blue-50 text-blue-700 border-blue-200",
      receipt: "bg-emerald-50 text-emerald-700 border-emerald-200",
      review: "bg-amber-50 text-amber-700 border-amber-200",
      doc: "bg-fuchsia-50 text-fuchsia-700 border-fuchsia-200",
      evidence: "bg-slate-100 text-slate-700 border-slate-300",
      log: "bg-teal-50 text-teal-700 border-teal-200",
    };
    return styles[kind] ?? "bg-gray-100 text-gray-600 border-gray-300";
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

<div class="flex items-center justify-between">
  <h1 class="text-lg font-semibold text-gray-900">Artifacts</h1>
  <button
    class="inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium text-gray-600 transition-colors hover:bg-gray-100"
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
    {filtersOpen ? "Hide filters" : "Filter"}
  </button>
</div>

{#if filtersOpen}
  <form
    class="mt-3 rounded-xl border border-gray-200/80 bg-white p-4 shadow-sm"
    onsubmit={(event) => {
      event.preventDefault();
      void applyFilters();
    }}
  >
    <div class="grid gap-3 sm:grid-cols-2">
      <label class="text-xs font-medium text-gray-500"
        >Kind <input
          bind:value={filters.kind}
          class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-1.5 text-sm transition-colors focus:bg-white"
          placeholder="work_order, receipt, review, doc..."
        /></label
      >
      <label class="text-xs font-medium text-gray-500"
        >Thread ID <input
          bind:value={filters.thread_id}
          class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-1.5 text-sm transition-colors focus:bg-white"
          placeholder="thread-onboarding"
        /></label
      >
      <label class="text-xs font-medium text-gray-500"
        >Created after <input
          bind:value={filters.created_after}
          class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-1.5 text-sm transition-colors focus:bg-white"
          type="datetime-local"
        /></label
      >
      <label class="text-xs font-medium text-gray-500"
        >Created before <input
          bind:value={filters.created_before}
          class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-1.5 text-sm transition-colors focus:bg-white"
          type="datetime-local"
        /></label
      >
    </div>
    <div class="mt-3 flex gap-2">
      <button
        class="rounded-md bg-gray-900 px-3 py-1.5 text-xs font-medium text-white shadow-sm hover:bg-gray-800"
        type="submit">Apply</button
      >
      <button
        class="rounded-md px-3 py-1.5 text-xs font-medium text-gray-500 hover:bg-gray-100"
        onclick={clearFilters}
        type="button">Clear</button
      >
    </div>
  </form>
{/if}

{#if error}
  <div
    class="mt-3 flex items-start gap-2 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700"
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
    {error}
  </div>
{:else if !loading && artifacts.length === 0}
  <div
    class="mt-8 rounded-xl border border-dashed border-gray-300 bg-white p-8 text-center"
  >
    <p class="text-sm font-semibold text-gray-700">No matching artifacts</p>
    <p class="mt-1.5 text-sm text-gray-500">
      Try adjusting filters or clearing the current view.
    </p>
  </div>
{/if}

{#if artifacts.length > 0}
  <div class="mt-4 space-y-2">
    {#each artifacts as artifact}
      <a
        class="block rounded-xl border border-gray-200/80 bg-white px-4 py-3 shadow-[0_1px_2px_rgba(0,0,0,0.04)] transition-all hover:border-gray-300/80 hover:shadow-[0_1px_4px_rgba(0,0,0,0.08)]"
        href={`/artifacts/${artifact.id}`}
      >
        <div class="flex items-start justify-between gap-3">
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              <span
                class={`inline-flex rounded-full border px-2 py-0.5 text-[11px] font-semibold ${kindBadge(artifact.kind)}`}
              >
                {kindLabel(artifact.kind)}
              </span>
              <span class="text-[11px] text-gray-500">
                {kindDescription(artifact.kind)}
              </span>
            </div>
            <p class="mt-1.5 truncate text-sm font-semibold text-gray-900">
              {rowHeading(artifact)}
            </p>
            <p class="mt-0.5 text-xs text-gray-500">
              Created {formatTimestamp(artifact.created_at) || "—"} by {artifact.created_by ||
                "unknown"}
            </p>
            <p class="mt-1 text-[11px] text-gray-400">ID: {artifact.id}</p>
          </div>
          <span class="shrink-0 text-xs text-gray-400">
            {(artifact.refs ?? []).length} ref{(artifact.refs ?? []).length ===
            1
              ? ""
              : "s"}
          </span>
        </div>

        <div class="mt-2 flex flex-wrap items-center gap-2 text-xs">
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
            <span class="text-[11px] text-gray-400">
              +{artifact.refs.length - 3} more
            </span>
          {/if}
        </div>
      </a>
    {/each}
  </div>
{/if}
