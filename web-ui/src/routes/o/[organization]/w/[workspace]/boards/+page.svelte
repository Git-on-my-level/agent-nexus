<script>
  import { page } from "$app/stores";
  import ArchiveButton from "$lib/components/ArchiveButton.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import TrashButton from "$lib/components/TrashButton.svelte";
  import CompactFilterBar from "$lib/components/CompactFilterBar.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { workspacePath } from "$lib/workspacePaths";
  import {
    lookupActorDisplayName,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import {
    BOARD_STATUS_LABELS,
    CANONICAL_BOARD_COLUMNS,
    boardSummaryCounts,
    freshnessStatusLabel,
    freshnessStatusTone,
    isFreshnessCurrent,
    parseDelimitedValues,
  } from "$lib/boardUtils";
  import { boardRowInspectNav } from "$lib/topicRouteUtils";

  const defaultBoardListFilters = {
    showArchived: false,
    state: "",
    owners: "",
    q: "",
  };

  let boards = $state([]);
  let loading = $state(false);
  let error = $state("");
  let retrying = $state(false);
  let filtersOpen = $state(false);
  let boardFiltersDraft = $state({ ...defaultBoardListFilters });
  let boardFiltersApplied = $state({ ...defaultBoardListFilters });
  let boardListGeneration = $state(0);
  let hasActiveFilters = $derived.by(() => {
    const f = boardFiltersApplied;
    return (
      f.showArchived ||
      Boolean(f.state) ||
      Boolean(f.owners.trim()) ||
      Boolean(f.q.trim())
    );
  });
  let archiveBusyId = $state("");
  let confirmModal = $state({ open: false, action: "", entityId: "" });
  let trashBusyId = $state("");

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );

  function workspaceHref(pathname = "/") {
    return workspacePath(organizationSlug, workspaceSlug, pathname);
  }

  function sumCardsByColumn(cols) {
    if (!cols || typeof cols !== "object") return 0;
    return Object.values(cols).reduce((acc, n) => acc + Number(n ?? 0), 0);
  }

  /** Normalize boards.list rows: API uses `{ board, summary }`; some mocks send a flat board + board_summary. */
  function normalizeBoardListRow(row) {
    if (!row || typeof row !== "object") {
      return { board: {}, summary: {} };
    }
    if (row.board) {
      return row;
    }
    const { board_summary, projection_freshness, ...boardRest } = row;
    const cols = board_summary?.cards_by_column ?? {};
    const cardTotal = sumCardsByColumn(cols);
    const docCount = Array.isArray(boardRest.document_refs)
      ? boardRest.document_refs.length
      : 0;
    return {
      board: { ...boardRest, projection_freshness },
      summary: {
        card_count: cardTotal,
        cards_by_column: cols,
        unresolved_card_count: cardTotal,
        resolved_card_count: 0,
        document_count: docCount,
        latest_activity_at: board_summary?.latest_activity_at ?? null,
        has_document_refs: docCount > 0,
      },
    };
  }

  async function loadBoards(isRetry = false) {
    loading = true;
    error = "";
    retrying = isRetry;
    try {
      const f = boardFiltersApplied;
      const filters = {};
      if (f.showArchived) filters.include_archived = "true";
      if (f.state) filters.state = f.state;
      const owners = parseDelimitedValues(f.owners);
      if (owners.length > 0) filters.owner = owners;
      const q = f.q.trim();
      if (q) filters.q = q;
      const data = await coreClient.listBoards(filters);
      boards = (data.boards ?? []).map(normalizeBoardListRow);
    } catch (e) {
      error = `Failed to load boards: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      loading = false;
      retrying = false;
    }
  }

  function lifecycleStateColor(state) {
    if (state === "active") return "text-ok-text bg-ok-soft";
    if (state === "archived") return "text-warn-text bg-warn-soft";
    if (state === "trashed") return "text-slate-300 bg-slate-500/10";
    return "text-[var(--fg-muted)] bg-[var(--line)]";
  }

  $effect(() => {
    boardListGeneration;
    if (workspaceSlug) {
      void loadBoards();
    }
  });

  function isBoardArchived(board) {
    const at = board?.archived_at;
    return typeof at === "string" ? at.trim() !== "" : Boolean(at);
  }

  async function archiveBoard(boardId) {
    const id = String(boardId ?? "").trim();
    if (!id || archiveBusyId) return;
    archiveBusyId = id;
    error = "";
    try {
      await coreClient.archiveBoard(id, {});
      await loadBoards();
    } catch (e) {
      error = `Archive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      archiveBusyId = "";
    }
  }

  async function unarchiveBoard(boardId) {
    const id = String(boardId ?? "").trim();
    if (!id || archiveBusyId) return;
    archiveBusyId = id;
    error = "";
    try {
      await coreClient.unarchiveBoard(id, {});
      await loadBoards();
    } catch (e) {
      error = `Unarchive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      archiveBusyId = "";
    }
  }

  async function trashBoard(boardId) {
    const id = String(boardId ?? "").trim();
    if (!id || trashBusyId) return;
    trashBusyId = id;
    error = "";
    try {
      await coreClient.trashBoard(id, {});
      confirmModal = { open: false, action: "", entityId: "" };
      await loadBoards();
    } catch (e) {
      error = `Trash failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      trashBusyId = "";
    }
  }

  function handleConfirm() {
    const id = confirmModal.entityId;
    const action = confirmModal.action;
    confirmModal = { open: false, action: "", entityId: "" };
    if (action === "archive") void archiveBoard(id);
    else if (action === "trash") void trashBoard(id);
  }

  function applyBoardFilters() {
    boardFiltersApplied = { ...boardFiltersDraft };
    boardListGeneration++;
  }

  function resetBoardFilters() {
    boardFiltersDraft = { ...defaultBoardListFilters };
    boardFiltersApplied = { ...defaultBoardListFilters };
    boardListGeneration++;
    filtersOpen = false;
  }
</script>

<div class="mb-4 flex flex-wrap items-start justify-between gap-4">
  <div>
    <h1 class="text-subtitle font-semibold text-[var(--fg)]">Boards</h1>
  </div>

  <div class="flex flex-wrap items-center gap-3">
    <button
      class="cursor-pointer inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-micro font-medium transition-colors {hasActiveFilters
        ? 'border-[var(--accent)]/40 bg-[var(--accent)]/10 text-[var(--accent)] hover:bg-[var(--accent)]/15'
        : 'border-[var(--line)] bg-[var(--bg-soft)] text-[var(--fg-muted)] hover:bg-[var(--line-subtle)]'}"
      onclick={() => {
        if (!filtersOpen) {
          boardFiltersDraft = { ...boardFiltersApplied };
        }
        filtersOpen = !filtersOpen;
      }}
      type="button"
      data-testid="boards-filters-toggle"
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
      {hasActiveFilters ? "Filtered" : "Filters"}
    </button>
    <a
      class="rounded-md bg-accent-solid px-3 py-1.5 text-micro font-medium text-white transition-colors hover:bg-accent"
      href={workspaceHref("/boards/new")}
    >
      New board
    </a>
  </div>
</div>

{#if filtersOpen}
  <CompactFilterBar testId="boards-filter-panel">
    {#snippet children()}
      <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <label class="text-micro">
          <span class="font-medium text-[var(--fg-muted)]">State</span>
          <select
            bind:value={boardFiltersDraft.state}
            class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
          >
            <option value="">All</option>
            {#each Object.entries(BOARD_STATUS_LABELS) as [value, label]}
              <option {value}>{label}</option>
            {/each}
          </select>
        </label>
        <label class="text-micro sm:col-span-2 lg:col-span-2">
          <span class="font-medium text-[var(--fg-muted)]">Search</span>
          <input
            bind:value={boardFiltersDraft.q}
            class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
            placeholder="Title or board id"
            type="text"
          />
        </label>
        <label class="text-micro sm:col-span-2 lg:col-span-2">
          <span class="font-medium text-[var(--fg-muted)]"
            >Owners (comma-separated ids)</span
          >
          <input
            bind:value={boardFiltersDraft.owners}
            class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
            placeholder="actor-ops-ai"
            type="text"
          />
        </label>
        <label
          class="flex items-end gap-1.5 pb-0.5 text-micro text-[var(--fg-muted)] sm:col-span-2 lg:col-span-3"
        >
          <input
            bind:checked={boardFiltersDraft.showArchived}
            class="h-3.5 w-3.5 cursor-pointer rounded border-[var(--line)] bg-[var(--bg)] text-[var(--accent-hover)] focus:ring-2 focus:ring-[var(--accent)] focus:ring-offset-0"
            type="checkbox"
          />
          Show archived
        </label>
      </div>
      <div class="mt-3 flex flex-wrap gap-1.5">
        <button
          class="cursor-pointer rounded-md bg-[var(--panel)] px-3 py-1.5 text-micro font-medium text-[var(--fg)] hover:bg-[var(--line)]"
          onclick={applyBoardFilters}
          type="button"
        >
          Apply
        </button>
        <button
          class="cursor-pointer rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-1.5 text-micro font-medium text-[var(--fg-muted)] hover:bg-[var(--line-subtle)]"
          onclick={resetBoardFilters}
          type="button"
        >
          Clear filters
        </button>
      </div>
    {/snippet}
  </CompactFilterBar>
{/if}

{#if error}
  <StateError
    message={error}
    onretry={() => void loadBoards(true)}
    {retrying}
    class="mb-4"
  />
{/if}

{#if loading && boards.length === 0}
  <Skeleton rows={8} />
{:else if boards.length === 0 && !error}
  <StateEmpty
    title="No boards yet"
    helper="Boards group cards into columns so the team can see what's planned, in flight, and done at a glance."
    actionLabel="New board"
    actionHref={workspaceHref("/boards/new")}
  />
{:else}
  <div
    class="space-y-px overflow-hidden rounded-md border border-[var(--line)] bg-[var(--panel)]"
  >
    {#each boards as item, i}
      {@const board = item.board}
      {@const summary = item.summary}
      {@const counts = boardSummaryCounts(summary)}
      {@const projectionFreshness =
        item.projection_freshness ?? board?.projection_freshness ?? null}
      {@const rowNav = boardRowInspectNav(board)}
      <div
        class="flex items-stretch {i > 0
          ? 'border-t border-[var(--line)]'
          : ''}"
      >
        <div
          class="group relative min-w-0 flex-1 px-4 py-3 text-left transition-colors hover:bg-[var(--line-subtle)]"
        >
          <a
            aria-label={`Open board ${board.title || board.id}`}
            class="absolute inset-0 z-0"
            href={workspaceHref(`/boards/${board.id}`)}
          ></a>
          <div
            class="pointer-events-none relative z-10 flex items-start justify-between gap-3"
          >
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2">
                {#if board.state}
                  <span
                    class="inline-flex rounded px-1.5 py-0.5 text-micro font-semibold {lifecycleStateColor(
                      board.state,
                    )}"
                  >
                    {BOARD_STATUS_LABELS[board.state] ?? board.state}
                  </span>
                {/if}
                {#if isBoardArchived(board)}
                  <span
                    class="rounded bg-warn-soft px-1.5 py-0.5 text-micro font-medium text-warn-text"
                    >Archived</span
                  >
                {/if}
                {#if projectionFreshness}
                  <span
                    class="inline-flex rounded px-1.5 py-0.5 text-micro font-medium {freshnessStatusTone(
                      projectionFreshness.status,
                    )}"
                  >
                    {freshnessStatusLabel(projectionFreshness.status)}
                  </span>
                {/if}
                {#if summary?.has_document_refs || summary?.has_document_ref}
                  <span
                    class="rounded bg-accent-soft px-1.5 py-0.5 text-micro text-accent-text"
                  >
                    Has doc
                  </span>
                {/if}
              </div>

              <span
                class="mt-1 block truncate text-meta font-medium text-[var(--fg)] group-hover:text-accent-text"
              >
                {board.title || board.id}
              </span>

              <div
                class="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-micro text-[var(--fg-muted)]"
              >
                {#if board.owners?.length > 0}
                  <span>
                    Owned by {board.owners
                      .map((owner) => actorName(owner))
                      .join(", ")}
                  </span>
                {/if}
                {#if rowNav}
                  <span>
                    <span class="text-[var(--fg-muted)]"
                      >{rowNav.kind === "topic"
                        ? "Topic"
                        : "Backing thread"}:</span
                    >
                    <a
                      class="pointer-events-auto relative z-20 text-accent-text transition-colors hover:text-accent-text"
                      href={workspaceHref(
                        rowNav.kind === "topic"
                          ? `/topics/${encodeURIComponent(rowNav.segment)}`
                          : `/threads/${encodeURIComponent(rowNav.segment)}`,
                      )}
                    >
                      {rowNav.display}
                    </a>
                  </span>
                {:else}
                  <span>
                    <span class="text-[var(--fg-muted)]">Context:</span>
                    <span class="text-[var(--fg-muted)]">—</span>
                  </span>
                {/if}
                <span>
                  Visual scan updated {formatTimestamp(board.updated_at) || "—"}
                </span>
                {#if isFreshnessCurrent(projectionFreshness)}
                  <span>
                    Latest derived activity {formatTimestamp(
                      summary?.latest_activity_at,
                    ) || "—"}
                  </span>
                {:else if projectionFreshness}
                  <span>Derived scan details are still catching up</span>
                {/if}
              </div>

              {#if isFreshnessCurrent(projectionFreshness)}
                <div
                  class="mt-1.5 flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-micro"
                >
                  {#each CANONICAL_BOARD_COLUMNS as column, ci}
                    {@const count = counts[column.key]}
                    <span
                      class={column.key === "blocked" && count > 0
                        ? "text-warn-text"
                        : "text-[var(--fg-muted)]"}
                    >
                      <span class="font-medium uppercase">{column.title}</span>
                      {count}
                    </span>
                    {#if ci < CANONICAL_BOARD_COLUMNS.length - 1}
                      <span class="text-[var(--line)]">·</span>
                    {/if}
                  {/each}
                </div>
              {/if}
            </div>
          </div>
        </div>
        <div class="hidden shrink-0 items-center gap-1 px-2 sm:flex">
          <ArchiveButton
            archived={isBoardArchived(board)}
            busy={Boolean(archiveBusyId) || Boolean(trashBusyId)}
            onarchive={(event) => {
              event.stopPropagation();
              confirmModal = {
                open: true,
                action: "archive",
                entityId: board.id,
              };
            }}
            onunarchive={(event) => {
              event.stopPropagation();
              void unarchiveBoard(board.id);
            }}
          />
          <TrashButton
            busy={Boolean(trashBusyId) || Boolean(archiveBusyId)}
            ontrash={(event) => {
              event.stopPropagation();
              confirmModal = {
                open: true,
                action: "trash",
                entityId: board.id,
              };
            }}
          />
        </div>
      </div>
    {/each}
  </div>
{/if}

<ConfirmModal
  open={confirmModal.open}
  title={confirmModal.action === "trash" ? "Move to trash" : "Archive board"}
  message={confirmModal.action === "trash"
    ? "This board will be moved to trash. You can restore it later."
    : "This board will be hidden from default views. You can unarchive it later."}
  confirmLabel={confirmModal.action === "trash" ? "Trash" : "Archive"}
  variant={confirmModal.action === "trash" ? "danger" : "warning"}
  busy={confirmModal.action === "trash"
    ? Boolean(trashBusyId)
    : Boolean(archiveBusyId)}
  onconfirm={handleConfirm}
  oncancel={() => (confirmModal = { open: false, action: "", entityId: "" })}
/>
