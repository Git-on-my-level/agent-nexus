<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import RefLink from "$lib/components/RefLink.svelte";
  import {
    DEFAULT_ARTIFACT_LIST_FILTERS,
    buildArtifactListQuery,
    buildArtifactListSearchString,
    formatArtifactTimestampInputValue,
    hasArtifactListFilters,
    parseArtifactListSearchParams,
  } from "$lib/artifactFilters";
  import { coreClient } from "$lib/coreClient";
  import {
    KIND_LABELS,
    kindLabel,
    kindDescription,
    kindColor,
  } from "$lib/artifactKinds";
  import { formatTimestamp } from "$lib/formatDate";
  import { workspacePath } from "$lib/workspacePaths";
  import { lookupActorDisplayName, actorRegistry } from "$lib/actorSession";

  let artifacts = $state([]);
  let loading = $state(false);
  let error = $state("");
  let filtersOpen = $state(false);
  let workspaceSlug = $derived($page.params.workspace);
  let actorName = $derived((id) => lookupActorDisplayName(id, $actorRegistry));
  let filters = $state({ ...DEFAULT_ARTIFACT_LIST_FILTERS });
  let dateInputs = $state({
    created_after: "",
    created_before: "",
  });

  function workspaceHref(pathname = "/") {
    return workspacePath(workspaceSlug, pathname);
  }

  $effect(() => {
    const parsed = parseArtifactListSearchParams($page.url.searchParams);
    filters = { ...DEFAULT_ARTIFACT_LIST_FILTERS, ...parsed };
    dateInputs = {
      created_after: formatArtifactTimestampInputValue(parsed.created_after),
      created_before: formatArtifactTimestampInputValue(parsed.created_before),
    };
    filtersOpen = hasArtifactListFilters(parsed);
    void loadArtifactsFromState(parsed);
  });

  async function loadArtifactsFromState(state) {
    loading = true;
    error = "";
    try {
      artifacts =
        (await coreClient.listArtifacts(buildArtifactListQuery(state)))
          .artifacts ?? [];
    } catch (e) {
      error = `Failed to load artifacts: ${e instanceof Error ? e.message : String(e)}`;
      artifacts = [];
    } finally {
      loading = false;
    }
  }

  async function applyFilters() {
    const qs = buildArtifactListSearchString(filters);
    const base = workspaceHref("/artifacts");
    await goto(`${base}${qs ? `?${qs}` : ""}`, {
      noScroll: true,
      keepFocus: true,
    });
  }

  async function clearFilters() {
    filters = { ...DEFAULT_ARTIFACT_LIST_FILTERS };
    dateInputs = { created_after: "", created_before: "" };
    filtersOpen = false;

    if ([...$page.url.searchParams.keys()].length === 0) {
      await loadArtifactsFromState(DEFAULT_ARTIFACT_LIST_FILTERS);
      return;
    }

    await goto(workspaceHref("/artifacts"), {
      noScroll: true,
      keepFocus: true,
    });
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

  function updateDateFilter(field, value) {
    dateInputs = { ...dateInputs, [field]: value };
    filters = { ...filters, [field]: value };
  }
</script>

<div class="flex items-center justify-between mb-4">
  <h1 class="text-lg font-semibold text-[var(--ui-text)]">Artifacts</h1>
  <button
    class="cursor-pointer inline-flex items-center gap-1.5 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-2.5 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)]"
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
    class="mb-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] p-3"
    onsubmit={(event) => {
      event.preventDefault();
      void applyFilters();
    }}
  >
    <div class="grid gap-3 sm:grid-cols-2">
      <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
        Kind
        <select
          bind:value={filters.kind}
          class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-2.5 py-1.5 text-[13px] transition-colors focus:bg-[var(--ui-panel)]"
        >
          <option value="">All</option>
          {#each Object.entries(KIND_LABELS) as [value, label]}
            <option {value}>{label}</option>
          {/each}
        </select>
      </label>
      <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
        Thread ID
        <input
          bind:value={filters.thread_id}
          class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-2.5 py-1.5 text-[13px] transition-colors focus:bg-[var(--ui-panel)]"
          placeholder="thread-onboarding"
        />
      </label>
      <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
        Created after
        <input
          value={dateInputs.created_after}
          class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-2.5 py-1.5 text-[13px] transition-colors focus:bg-[var(--ui-panel)]"
          type="datetime-local"
          oninput={(event) =>
            updateDateFilter("created_after", event.currentTarget.value)}
        />
      </label>
      <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
        Created before
        <input
          value={dateInputs.created_before}
          class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-2.5 py-1.5 text-[13px] transition-colors focus:bg-[var(--ui-panel)]"
          type="datetime-local"
          oninput={(event) =>
            updateDateFilter("created_before", event.currentTarget.value)}
        />
      </label>
    </div>
    <div class="mt-3 flex gap-1.5">
      <button
        class="cursor-pointer rounded-md bg-[var(--ui-panel)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text)] hover:bg-[var(--ui-border)]"
        type="submit">Apply</button
      >
      <button
        class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] hover:bg-[var(--ui-border-subtle)]"
        onclick={clearFilters}
        type="button">Clear</button
      >
    </div>
  </form>
{/if}

{#if loading}
  <div
    class="mt-12 flex items-center justify-center gap-2 text-[13px] text-[var(--ui-text-muted)]"
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
    Loading artifacts...
  </div>
{:else if error}
  <div class="mb-4 rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
    {error}
  </div>
{:else if artifacts.length === 0}
  <div class="mt-8 text-center">
    <p class="text-[13px] font-medium text-[var(--ui-text-muted)]">
      No matching artifacts
    </p>
    <p class="mt-1 text-[13px] text-[var(--ui-text-muted)]">
      Try adjusting filters or clearing the current view.
    </p>
  </div>
{/if}

{#if !loading && artifacts.length > 0}
  <div
    class="space-y-px rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] overflow-hidden"
  >
    {#each artifacts as artifact, i}
      <a
        class="block px-4 py-3 transition-colors hover:bg-[var(--ui-border-subtle)] {i >
        0
          ? 'border-t border-[var(--ui-border)]'
          : ''}"
        href={workspaceHref(`/artifacts/${artifact.id}`)}
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
              <span class="text-[11px] text-[var(--ui-text-muted)]"
                >{kindDescription(artifact.kind)}</span
              >
            </div>
            <p
              class="mt-1 truncate text-[13px] font-medium text-[var(--ui-text)]"
            >
              {rowHeading(artifact)}
            </p>
            <p class="text-[11px] text-[var(--ui-text-muted)]">
              Created {formatTimestamp(artifact.created_at) || "—"} by {actorName(
                artifact.created_by,
              )}
            </p>
          </div>
          <span class="shrink-0 text-[11px] text-[var(--ui-text-subtle)]">
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
              <span class="text-[11px] text-[var(--ui-text-subtle)]"
                >+{artifact.refs.length - 3} more</span
              >
            {/if}
          </div>
        {/if}
      </a>
    {/each}
  </div>
{/if}
