<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import CompactFilterBar from "$lib/components/CompactFilterBar.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import {
    ARTIFACT_STATE_VALUES,
    ARTIFACT_BACKING_SCOPE_VALUES,
    DEFAULT_ARTIFACT_LIST_FILTERS,
    buildArtifactListQuery,
    buildArtifactListSearchString,
    formatArtifactTimestampInputValue,
    hasArtifactListFilters,
    parseArtifactListSearchParams,
  } from "$lib/artifactFilters";
  import { coreClient } from "$lib/coreClient";
  import { KIND_LABELS, kindLabel } from "$lib/artifactKinds";
  import { formatTimestamp } from "$lib/formatDate";
  import { workspacePath } from "$lib/workspacePaths";
  import {
    lookupActorDisplayName,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import LeadingSelectionGlyph from "$lib/components/LeadingSelectionGlyph.svelte";
  import WorkspaceListBulkToolbar from "$lib/components/WorkspaceListBulkToolbar.svelte";
  import AttachmentChip from "$lib/components/AttachmentChip.svelte";
  import { createWorkspaceListSelection } from "$lib/workspaceListSelection.svelte.js";
  import { buildPrimitiveRefRoutes, resolveRefLink } from "$lib/refLinkModel";

  let artifacts = $state([]);
  let loading = $state(false);
  let error = $state("");
  let retrying = $state(false);
  let confirmModal = $state({
    open: false,
    action: "",
    entityId: "",
    bulkIds: /** @type {string[] | null} */ (null),
  });
  let bulkBusy = $state(false);

  const artifactSel = createWorkspaceListSelection({
    bulkBusy: () => bulkBusy,
  });
  let filtersOpen = $state(false);

  const ARTIFACT_LIFECYCLE_LABELS = {
    active: "Active",
    archived: "Archived",
    trashed: "Trashed",
  };
  const ARTIFACT_BACKING_SCOPE_LABELS = {
    all: "All",
    standalone: "Standalone",
    backing_only: "Backing only",
  };
  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );
  let filters = $state({ ...DEFAULT_ARTIFACT_LIST_FILTERS });
  let dateInputs = $state({
    created_after: "",
    created_before: "",
  });
  let hasActiveFilters = $derived(hasArtifactListFilters(filters));

  function workspaceHref(pathname = "/") {
    return workspacePath(organizationSlug, workspaceSlug, pathname);
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

  async function loadArtifactsFromState(state, isRetry = false) {
    loading = true;
    error = "";
    retrying = isRetry;
    try {
      const query = { ...buildArtifactListQuery(state) };
      artifacts = (await coreClient.listArtifacts(query)).artifacts ?? [];
    } catch (e) {
      error = `Failed to load artifacts: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      loading = false;
      retrying = false;
    }
  }

  $effect(() => {
    artifacts;
    artifactSel.reconcileSelectionWithIds(
      artifacts.map((a) => String(a?.id ?? "").trim()).filter(Boolean),
    );
  });

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

  let artifactListRoutesById = $derived.by(
    () =>
      buildPrimitiveRefRoutes({
        artifacts,
        events: [],
        cards: [],
        documents: [],
        threadId: "",
      }).artifactRoutesById,
  );

  function isAttachmentListRow(artifact) {
    return String(artifact?.kind ?? "").toLowerCase() === "attachment";
  }

  function listAttachmentResolved(artifact) {
    const id = String(artifact?.id ?? "").trim();
    return resolveRefLink(`artifact:${id}`, {
      threadId: String(artifact?.thread_id ?? "").trim(),
      boardId: "",
      humanize: true,
      artifactRoutesById: artifactListRoutesById,
      eventRoutesById: {},
      workspaceSlug,
      organizationSlug,
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

  /** @param {string} value */
  function toggleArtifactLifecycleState(value) {
    const cur = [...(filters.states ?? ["active"])];
    const set = new Set(cur);
    if (set.has(value)) {
      if (set.size <= 1) return;
      set.delete(value);
    } else {
      set.add(value);
    }
    const order = /** @type {const} */ ([...ARTIFACT_STATE_VALUES]);
    filters = {
      ...filters,
      states: order.filter((s) => set.has(s)),
    };
  }

  function isArtifactArchived(artifact) {
    const at = artifact?.archived_at;
    return typeof at === "string" ? at.trim() !== "" : Boolean(at);
  }

  function isArtifactTrashed(artifact) {
    if (artifact?.state === "trashed") return true;
    const t = artifact?.trashed_at;
    return typeof t === "string" ? t.trim() !== "" : Boolean(t);
  }

  let selectedArtifacts = $derived(
    artifacts.filter((a) => artifactSel.selectedIds.has(a.id)),
  );
  let allArtifactsVisibleSelected = $derived(
    artifacts.length > 0 &&
      artifacts.every((a) => artifactSel.selectedIds.has(a.id)),
  );
  let bulkArtifactsCanArchive = $derived(
    selectedArtifacts.some(
      (a) => !isArtifactArchived(a) && !isArtifactTrashed(a),
    ),
  );
  let bulkArtifactsCanUnarchive = $derived(
    selectedArtifacts.some(
      (a) => isArtifactArchived(a) && !isArtifactTrashed(a),
    ),
  );
  let bulkArtifactsCanTrash = $derived(
    selectedArtifacts.some((a) => !isArtifactTrashed(a)),
  );

  function selectAllVisibleArtifacts() {
    artifactSel.selectAllFromVisibleIds(
      artifacts.map((a) => a.id).filter(Boolean),
    );
  }

  function clearArtifactSelection() {
    artifactSel.clearSelection();
  }

  function toggleArtifactSelectMode() {
    artifactSel.toggleSelectMode();
  }

  /** @param {number} i */
  function artifactIdAtVisibleIndex(i) {
    const a = artifacts[i];
    return String(a?.id ?? "").trim();
  }

  /** @param {number} i */
  function artifactHrefAtVisibleIndex(i) {
    return workspaceHref(`/artifacts/${artifactIdAtVisibleIndex(i)}`);
  }

  async function bulkArchiveArtifacts(ids) {
    const list = ids.filter(Boolean);
    if (!list.length || bulkBusy) return;
    bulkBusy = true;
    error = "";
    try {
      for (const id of list) {
        await coreClient.archiveArtifact(id, {});
      }
      clearArtifactSelection();
      confirmModal = { open: false, action: "", entityId: "", bulkIds: null };
      const parsed = parseArtifactListSearchParams($page.url.searchParams);
      await loadArtifactsFromState(parsed);
    } catch (e) {
      error = `Archive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      bulkBusy = false;
    }
  }

  async function bulkUnarchiveArtifacts(ids) {
    const list = ids.filter(Boolean);
    if (!list.length || bulkBusy) return;
    bulkBusy = true;
    error = "";
    try {
      for (const id of list) {
        await coreClient.unarchiveArtifact(id, {});
      }
      clearArtifactSelection();
      const parsed = parseArtifactListSearchParams($page.url.searchParams);
      await loadArtifactsFromState(parsed);
    } catch (e) {
      error = `Unarchive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      bulkBusy = false;
    }
  }

  async function bulkTrashArtifacts(ids) {
    const list = ids.filter(Boolean);
    if (!list.length || bulkBusy) return;
    bulkBusy = true;
    error = "";
    try {
      for (const id of list) {
        await coreClient.trashArtifact(id, {});
      }
      clearArtifactSelection();
      confirmModal = { open: false, action: "", entityId: "", bulkIds: null };
      const parsed = parseArtifactListSearchParams($page.url.searchParams);
      await loadArtifactsFromState(parsed);
    } catch (e) {
      error = `Trash failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      bulkBusy = false;
    }
  }

  function idsForBulkArtifactArchive() {
    return selectedArtifacts
      .filter((a) => !isArtifactArchived(a) && !isArtifactTrashed(a))
      .map((a) => a.id);
  }

  function idsForBulkArtifactUnarchive() {
    return selectedArtifacts
      .filter((a) => isArtifactArchived(a) && !isArtifactTrashed(a))
      .map((a) => a.id);
  }

  function idsForBulkArtifactTrash() {
    return selectedArtifacts
      .filter((a) => !isArtifactTrashed(a))
      .map((a) => a.id);
  }

  function handleConfirm() {
    const bulkIds = confirmModal.bulkIds;
    const action = confirmModal.action;
    confirmModal = { open: false, action: "", entityId: "", bulkIds: null };
    if (bulkIds && bulkIds.length > 0) {
      if (action === "archive") void bulkArchiveArtifacts(bulkIds);
      else if (action === "trash") void bulkTrashArtifacts(bulkIds);
    }
  }

  let artifactConfirmBulkCount = $derived(confirmModal.bulkIds?.length ?? 0);
  let artifactConfirmIsBulk = $derived(artifactConfirmBulkCount > 0);

  let artifactConfirmModalTitle = $derived.by(() => {
    if (confirmModal.action === "trash") {
      return artifactConfirmIsBulk
        ? `Move ${artifactConfirmBulkCount} artifacts to trash`
        : "Move to trash";
    }
    return artifactConfirmIsBulk
      ? `Archive ${artifactConfirmBulkCount} artifacts`
      : "Archive artifact";
  });

  let artifactConfirmModalMessage = $derived.by(() => {
    if (confirmModal.action === "trash") {
      return artifactConfirmIsBulk
        ? `These artifacts (${artifactConfirmBulkCount}) will be moved to trash. You can restore them later.`
        : "This artifact will be moved to trash. You can restore it later.";
    }
    return artifactConfirmIsBulk
      ? `These artifacts (${artifactConfirmBulkCount}) will be hidden from default views. You can unarchive them later.`
      : "This artifact will be hidden from default views. You can unarchive it later.";
  });

  let artifactConfirmModalBusy = $derived(artifactConfirmIsBulk && bulkBusy);

  function updateDateFilter(field, value) {
    dateInputs = { ...dateInputs, [field]: value };
    filters = { ...filters, [field]: value };
  }
</script>

<div class="mb-3 flex max-md:mb-2 flex-wrap items-center justify-between gap-2">
  <h1 class="text-subtitle font-semibold text-[var(--fg)]">Artifacts</h1>
  <div class="flex flex-wrap items-center justify-end gap-1.5">
    <button
      class="cursor-pointer inline-flex items-center gap-1.5 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-micro font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] {!artifacts.length &&
      !loading
        ? 'opacity-50 pointer-events-none'
        : ''}"
      onclick={toggleArtifactSelectMode}
      disabled={!artifacts.length && !loading}
      type="button"
      aria-pressed={artifactSel.selectMode}
    >
      {artifactSel.selectMode ? "Done" : "Select"}
    </button>
    <button
      class="cursor-pointer inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-micro font-medium transition-colors {hasActiveFilters
        ? 'border-[var(--accent)]/40 bg-[var(--accent)]/10 text-[var(--accent)] hover:bg-[var(--accent)]/15'
        : 'border-[var(--line)] bg-[var(--bg-soft)] text-[var(--fg-muted)] hover:bg-[var(--line-subtle)]'}"
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
      {hasActiveFilters ? "Filtered" : "Filter"}
    </button>
  </div>
</div>

{#if filtersOpen}
  <CompactFilterBar testId="artifacts-filter-panel">
    {#snippet children()}
      <form
        class="contents"
        onsubmit={(event) => {
          event.preventDefault();
          void applyFilters();
        }}
      >
        <div class="grid gap-3 sm:grid-cols-2">
          <label class="text-micro font-medium text-[var(--fg-muted)]">
            Kind
            <select
              bind:value={filters.kind}
              class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
            >
              <option value="">All</option>
              {#each Object.entries(KIND_LABELS) as [value, label]}
                <option {value}>{label}</option>
              {/each}
            </select>
          </label>
          <label class="text-micro font-medium text-[var(--fg-muted)]">
            Backing
            <select
              bind:value={filters.backing_scope}
              class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
            >
              {#each ARTIFACT_BACKING_SCOPE_VALUES as value}
                <option {value}>{ARTIFACT_BACKING_SCOPE_LABELS[value]}</option>
              {/each}
            </select>
          </label>
          <label class="text-micro font-medium text-[var(--fg-muted)]">
            Topic ID
            <input
              bind:value={filters.thread_id}
              class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
              placeholder="thread-onboarding"
            />
          </label>
          <label class="text-micro font-medium text-[var(--fg-muted)]">
            Created after
            <input
              value={dateInputs.created_after}
              class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
              type="datetime-local"
              oninput={(event) =>
                updateDateFilter("created_after", event.currentTarget.value)}
            />
          </label>
          <label class="text-micro font-medium text-[var(--fg-muted)]">
            Created before
            <input
              value={dateInputs.created_before}
              class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
              type="datetime-local"
              oninput={(event) =>
                updateDateFilter("created_before", event.currentTarget.value)}
            />
          </label>
        </div>
        <div class="mt-3 text-micro">
          <span class="font-medium text-[var(--fg-muted)]">Lifecycle</span>
          <fieldset
            class="mt-1 space-y-1 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-2"
          >
            {#each Object.entries(ARTIFACT_LIFECYCLE_LABELS) as [value, label] (value)}
              <label
                class="flex cursor-pointer items-center gap-2 text-meta text-[var(--fg)]"
              >
                <input
                  checked={(filters.states ?? ["active"]).includes(value)}
                  class="h-3.5 w-3.5 cursor-pointer rounded border-[var(--line)] bg-[var(--bg)] text-[var(--accent-hover)] focus:ring-2 focus:ring-[var(--accent)] focus:ring-offset-0"
                  type="checkbox"
                  onchange={() => toggleArtifactLifecycleState(value)}
                />
                {label}
              </label>
            {/each}
          </fieldset>
        </div>
        <div class="mt-3 flex gap-1.5">
          <button
            class="cursor-pointer rounded-md bg-[var(--panel)] px-3 py-1.5 text-micro font-medium text-[var(--fg)] hover:bg-[var(--line)]"
            type="submit">Apply</button
          >
          <button
            class="cursor-pointer rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-1.5 text-micro font-medium text-[var(--fg-muted)] hover:bg-[var(--line-subtle)]"
            onclick={clearFilters}
            type="button">Clear filters</button
          >
        </div>
      </form>
    {/snippet}
  </CompactFilterBar>
{/if}

{#if loading && artifacts.length === 0}
  <Skeleton rows={6} />
{:else if error}
  <StateError
    message={error}
    onretry={() => void loadArtifactsFromState(filters, true)}
    {retrying}
    class="mb-4"
  />
{:else if artifacts.length === 0}
  <StateEmpty
    title="No matching artifacts"
    helper="Try adjusting filters or clearing the current view."
  />
{/if}

{#if !loading && artifacts.length > 0}
  {#if artifactSel.selectMode}
    <WorkspaceListBulkToolbar
      allVisibleSelected={allArtifactsVisibleSelected}
      busy={bulkBusy}
      canArchive={bulkArtifactsCanArchive}
      canTrash={bulkArtifactsCanTrash}
      canUnarchive={bulkArtifactsCanUnarchive}
      onArchive={() => {
        const ids = idsForBulkArtifactArchive();
        if (!ids.length) return;
        confirmModal = {
          open: true,
          action: "archive",
          entityId: "",
          bulkIds: ids,
        };
      }}
      onClear={clearArtifactSelection}
      onDeselectAll={clearArtifactSelection}
      onSelectAll={selectAllVisibleArtifacts}
      onTrash={() => {
        const ids = idsForBulkArtifactTrash();
        if (!ids.length) return;
        confirmModal = {
          open: true,
          action: "trash",
          entityId: "",
          bulkIds: ids,
        };
      }}
      onUnarchive={() =>
        void bulkUnarchiveArtifacts(idsForBulkArtifactUnarchive())}
      selectionChromeActive={true}
      selectedCount={artifactSel.selectedIds.size}
    />
  {/if}
  <div
    class="space-y-px rounded-md border border-[var(--line)] bg-[var(--bg-soft)] overflow-hidden"
  >
    {#each artifacts as artifact, i}
      {@const selected = artifactSel.selectedIds.has(artifact.id)}
      {@const borderTop = i > 0 ? "border-t border-[var(--line)]" : ""}
      {@const artifactRefCount = (artifact.refs ?? []).length}
      {#if artifactSel.selectMode}
        <div
          class="transition-colors hover:bg-[var(--line-subtle)] {borderTop}"
        >
          <div
            aria-label={`${selected ? "Deselect" : "Select"} ${rowHeading(artifact)}`}
            aria-pressed={selected}
            class="flex cursor-pointer items-stretch outline-none transition-colors focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--bg-soft)] {selected
              ? 'border-l-[3px] border-l-[var(--accent)] bg-[var(--accent)]/10'
              : 'border-l-[3px] border-l-transparent'}"
            onclick={(e) =>
              artifactSel.handleRowMouseEvent(
                e,
                i,
                artifacts.length,
                artifactIdAtVisibleIndex,
                artifactHrefAtVisibleIndex,
              )}
            onkeydown={(e) =>
              artifactSel.handleRowKeyboardEvent(
                e,
                i,
                artifactIdAtVisibleIndex,
              )}
            role="button"
            tabindex="0"
          >
            <div class="flex shrink-0 items-center self-stretch pl-2 sm:pl-3">
              <LeadingSelectionGlyph {selected} />
            </div>
            <div
              class="pointer-events-none flex min-w-0 flex-1 flex-col gap-2 px-3 py-2 sm:pr-4 sm:pl-2"
            >
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0 flex-1">
                  <div class="flex flex-wrap items-center gap-2">
                    {#if isAttachmentListRow(artifact)}
                      <div
                        class="pointer-events-auto max-w-full"
                        onclick={(e) => e.stopPropagation()}
                        role="presentation"
                      >
                        <AttachmentChip
                          resolved={listAttachmentResolved(artifact)}
                          size="compact"
                        />
                      </div>
                    {:else}
                      <p
                        class="truncate text-meta font-medium text-[var(--fg)]"
                      >
                        {rowHeading(artifact)}
                      </p>
                    {/if}
                    {#if isArtifactArchived(artifact)}
                      <span
                        class="rounded bg-warn-soft px-1.5 py-0.5 text-micro font-medium text-warn-text"
                        >Archived</span
                      >
                    {/if}
                  </div>
                  <p class="text-micro text-[var(--fg-muted)]">
                    {#if !isAttachmentListRow(artifact)}
                      <span class="font-medium">{kindLabel(artifact.kind)}</span
                      >
                      ·
                    {/if}
                    Created {formatTimestamp(artifact.created_at) || "—"} by {actorName(
                      artifact.created_by,
                    )}
                  </p>
                </div>
                <div class="flex shrink-0 items-center gap-1">
                  <span class="mr-1 text-micro text-[var(--fg-muted)]">
                    {artifactRefCount}
                    {artifactRefCount === 1 ? "ref" : "refs"}
                  </span>
                </div>
              </div>
              {#if refPreview(artifact).length > 0 || artifact.thread_id}
                <div
                  class="hidden flex-wrap items-center gap-1.5 text-micro sm:flex"
                >
                  {#if artifact.thread_id}
                    <RefLink
                      humanize
                      labelHints={{
                        [`thread:${artifact.thread_id}`]: "Thread (timeline)",
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
                  {#if artifactRefCount > 3}
                    <span class="text-micro text-[var(--fg-muted)]"
                      >+{artifact.refs.length - 3} more</span
                    >
                  {/if}
                </div>
              {/if}
            </div>
          </div>
        </div>
      {:else}
        <div
          class="px-3 py-2 transition-colors hover:bg-[var(--line-subtle)] sm:px-4 {borderTop}"
        >
          <div class="flex items-start justify-between gap-3">
            {#if isAttachmentListRow(artifact)}
              <div class="min-w-0 flex-1">
                <AttachmentChip
                  resolved={listAttachmentResolved(artifact)}
                  size="compact"
                />
                {#if isArtifactArchived(artifact)}
                  <span
                    class="mt-1 inline-flex rounded bg-warn-soft px-1.5 py-0.5 text-micro font-medium text-warn-text"
                    >Archived</span
                  >
                {/if}
                <p class="mt-1 text-micro text-[var(--fg-muted)]">
                  Created {formatTimestamp(artifact.created_at) || "—"} by {actorName(
                    artifact.created_by,
                  )}
                </p>
              </div>
            {:else}
              <a
                class="min-w-0 flex-1 outline-none rounded-sm focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--bg-soft)]"
                href={workspaceHref(`/artifacts/${artifact.id}`)}
              >
                <div class="flex flex-wrap items-center gap-2">
                  <p class="truncate text-meta font-medium text-[var(--fg)]">
                    {rowHeading(artifact)}
                  </p>
                  {#if isArtifactArchived(artifact)}
                    <span
                      class="rounded bg-warn-soft px-1.5 py-0.5 text-micro font-medium text-warn-text"
                      >Archived</span
                    >
                  {/if}
                </div>
                <p class="text-micro text-[var(--fg-muted)]">
                  <span class="font-medium">{kindLabel(artifact.kind)}</span>
                  · Created {formatTimestamp(artifact.created_at) || "—"} by {actorName(
                    artifact.created_by,
                  )}
                </p>
              </a>
            {/if}
            <span
              class="shrink-0 tabular-nums text-micro text-[var(--fg-muted)]"
              aria-hidden="true"
            >
              {artifactRefCount}
              {artifactRefCount === 1 ? "ref" : "refs"}
            </span>
          </div>

          {#if refPreview(artifact).length > 0 || artifact.thread_id}
            <div
              class="mt-1.5 hidden flex-wrap items-center gap-1.5 text-micro sm:flex"
            >
              {#if artifact.thread_id}
                <RefLink
                  humanize
                  labelHints={{
                    [`thread:${artifact.thread_id}`]: "Thread (timeline)",
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
              {#if artifactRefCount > 3}
                <span class="text-micro text-[var(--fg-muted)]"
                  >+{artifact.refs.length - 3} more</span
                >
              {/if}
            </div>
          {/if}
        </div>
      {/if}
    {/each}
  </div>
{/if}

<ConfirmModal
  open={confirmModal.open}
  title={artifactConfirmModalTitle}
  message={artifactConfirmModalMessage}
  confirmLabel={confirmModal.action === "trash" ? "Trash" : "Archive"}
  variant={confirmModal.action === "trash" ? "danger" : "warning"}
  busy={artifactConfirmModalBusy}
  onconfirm={handleConfirm}
  oncancel={() =>
    (confirmModal = {
      open: false,
      action: "",
      entityId: "",
      bulkIds: null,
    })}
/>
