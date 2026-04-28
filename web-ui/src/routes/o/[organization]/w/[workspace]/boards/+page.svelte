<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import CompactFilterBar from "$lib/components/CompactFilterBar.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { workspacePath } from "$lib/workspacePaths";
  import { BOARD_STATUS_LABELS, parseDelimitedValues } from "$lib/boardUtils";
  import WorkspaceResourceListRow from "$lib/components/WorkspaceResourceListRow.svelte";
  import WorkspaceListBulkToolbar from "$lib/components/WorkspaceListBulkToolbar.svelte";
  import LeadingSelectionGlyph from "$lib/components/LeadingSelectionGlyph.svelte";
  import Button from "$lib/components/Button.svelte";

  const defaultBoardListFilters = {
    states: ["active"],
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
    const st = f.states ?? ["active"];
    const defaultLifecycle = st.length === 1 && String(st[0]) === "active";
    return !defaultLifecycle || Boolean(f.owners.trim()) || Boolean(f.q.trim());
  });
  let archiveBusyId = $state("");
  /** @type {{ open: boolean, action: string, entityId: string, bulkIds: string[] | null }} */
  let confirmModal = $state({
    open: false,
    action: "",
    entityId: "",
    bulkIds: null,
  });
  let trashBusyId = $state("");
  let bulkBusy = $state(false);
  /** @type {Set<string>} */
  let selectedBoardIds = $state(new Set());
  let boardSelectMode = $state(false);
  /** @type {number | null} */
  let boardSelectionAnchorIndex = $state(null);

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  function workspaceHref(pathname = "/") {
    return workspacePath(organizationSlug, workspaceSlug, pathname);
  }

  function sumCardsByColumn(cols) {
    if (!cols || typeof cols !== "object") return 0;
    return Object.values(cols).reduce((acc, n) => acc + Number(n ?? 0), 0);
  }

  /**
   * Normalize boards.list rows. API returns `{ board, summary }` where `summary` is
   * derived list stats (NOT `board.summary` text). We rename to `listStats` to avoid
   * colliding with the resource's optional `summary` blurb in templates.
   */
  function normalizeBoardListRow(row) {
    if (!row || typeof row !== "object") {
      return { board: {}, listStats: {} };
    }
    if (row.board) {
      const stats = row.summary;
      return {
        board: row.board,
        listStats:
          stats && typeof stats === "object" && !Array.isArray(stats)
            ? stats
            : {},
      };
    }
    const { board_summary, projection_freshness, ...boardRest } = row;
    const cols = board_summary?.cards_by_column ?? {};
    const cardTotal = sumCardsByColumn(cols);
    const docCount = Array.isArray(boardRest.document_refs)
      ? boardRest.document_refs.length
      : 0;
    return {
      board: { ...boardRest, projection_freshness },
      listStats: {
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
      const filters = {
        state: f.states ?? ["active"],
      };
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

  $effect(() => {
    boards;
    const valid = new Set(
      boards.map((row) => String(row?.board?.id ?? "").trim()).filter(Boolean),
    );
    const next = new Set([...selectedBoardIds].filter((id) => valid.has(id)));
    if (next.size !== selectedBoardIds.size) {
      selectedBoardIds = next;
    }
  });

  let allBoardsVisibleSelected = $derived(
    boards.length > 0 &&
      boards.every((row) => selectedBoardIds.has(row.board.id)),
  );
  let selectedBoardRows = $derived(
    boards.filter((row) => selectedBoardIds.has(row.board.id)),
  );

  let bulkBoardsCanArchive = $derived(
    selectedBoardRows.some(
      (row) => !isBoardArchived(row.board) && !isBoardTrashed(row.board),
    ),
  );
  let bulkBoardsCanUnarchive = $derived(
    selectedBoardRows.some(
      (row) => isBoardArchived(row.board) && !isBoardTrashed(row.board),
    ),
  );
  let bulkBoardsCanTrash = $derived(
    selectedBoardRows.some((row) => !isBoardTrashed(row.board)),
  );

  function selectAllVisibleBoards() {
    selectedBoardIds = new Set(
      boards.map((row) => row.board.id).filter(Boolean),
    );
  }

  function clearBoardSelection() {
    selectedBoardIds = new Set();
  }

  function toggleBoardSelectMode() {
    boardSelectMode = !boardSelectMode;
    if (!boardSelectMode) {
      clearBoardSelection();
      boardSelectionAnchorIndex = null;
    }
  }

  function applyBoardRangeFromIndices(fromIndex, toIndex) {
    const lo = Math.min(fromIndex, toIndex);
    const hi = Math.max(fromIndex, toIndex);
    const next = new Set(selectedBoardIds);
    for (let i = lo; i <= hi; i++) {
      const b = boards[i]?.board;
      if (b?.id) next.add(b.id);
    }
    selectedBoardIds = next;
  }

  /** @param {{ board: { id: string } }} item */
  function handleBoardRowClick(item, index, e) {
    if (!boardSelectMode || bulkBusy) return;
    const board = item.board;
    const href = workspaceHref(`/boards/${board.id}`);
    const ce = /** @type {MouseEvent & { detail?: number }} */ (e);
    if ((ce.detail ?? 1) >= 2) {
      void goto(href);
      return;
    }
    if (e.shiftKey && boardSelectionAnchorIndex !== null) {
      applyBoardRangeFromIndices(boardSelectionAnchorIndex, index);
      return;
    }
    const id = board.id;
    const next = new Set(selectedBoardIds);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    selectedBoardIds = next;
    boardSelectionAnchorIndex = index;
  }

  /** @param {{ board: { id: string } }} item */
  function boardRowKeydown(item, index, e) {
    if (!boardSelectMode || bulkBusy) return;
    if (e.key !== " " && e.key !== "Enter") return;
    e.preventDefault();
    const board = item.board;
    if (e.shiftKey && boardSelectionAnchorIndex !== null) {
      applyBoardRangeFromIndices(boardSelectionAnchorIndex, index);
      return;
    }
    const id = board.id;
    const next = new Set(selectedBoardIds);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    selectedBoardIds = next;
    boardSelectionAnchorIndex = index;
  }

  $effect(() => {
    if (!boardSelectMode) return;
    /** @param {KeyboardEvent} ev */
    function onBoardKey(ev) {
      if (ev.key !== "Escape") return;
      boardSelectMode = false;
      clearBoardSelection();
      boardSelectionAnchorIndex = null;
    }
    document.addEventListener("keydown", onBoardKey);
    return () => document.removeEventListener("keydown", onBoardKey);
  });

  function boardIdsForBulkArchive() {
    return selectedBoardRows
      .filter(
        (row) => !isBoardArchived(row.board) && !isBoardTrashed(row.board),
      )
      .map((row) => row.board.id);
  }

  function boardIdsForBulkUnarchive() {
    return selectedBoardRows
      .filter((row) => isBoardArchived(row.board) && !isBoardTrashed(row.board))
      .map((row) => row.board.id);
  }

  function boardIdsForBulkTrash() {
    return selectedBoardRows
      .filter((row) => !isBoardTrashed(row.board))
      .map((row) => row.board.id);
  }

  function isBoardArchived(board) {
    const at = board?.archived_at;
    return typeof at === "string" ? at.trim() !== "" : Boolean(at);
  }

  function isBoardTrashed(board) {
    return board?.state === "trashed";
  }

  async function archiveBoard(boardId) {
    const id = String(boardId ?? "").trim();
    if (!id || archiveBusyId || bulkBusy) return;
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

  async function trashBoard(boardId) {
    const id = String(boardId ?? "").trim();
    if (!id || trashBusyId || bulkBusy) return;
    trashBusyId = id;
    error = "";
    try {
      await coreClient.trashBoard(id, {});
      confirmModal = { open: false, action: "", entityId: "", bulkIds: null };
      await loadBoards();
    } catch (e) {
      error = `Trash failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      trashBusyId = "";
    }
  }

  async function bulkArchiveBoards(ids) {
    const list = ids.filter(Boolean);
    if (!list.length || bulkBusy) return;
    bulkBusy = true;
    error = "";
    try {
      for (const id of list) {
        await coreClient.archiveBoard(id, {});
      }
      clearBoardSelection();
      confirmModal = { open: false, action: "", entityId: "", bulkIds: null };
      await loadBoards();
    } catch (e) {
      error = `Archive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      bulkBusy = false;
    }
  }

  async function bulkUnarchiveBoards(ids) {
    const list = ids.filter(Boolean);
    if (!list.length || bulkBusy) return;
    bulkBusy = true;
    error = "";
    try {
      for (const id of list) {
        await coreClient.unarchiveBoard(id, {});
      }
      clearBoardSelection();
      await loadBoards();
    } catch (e) {
      error = `Unarchive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      bulkBusy = false;
    }
  }

  async function bulkTrashBoards(ids) {
    const list = ids.filter(Boolean);
    if (!list.length || bulkBusy) return;
    bulkBusy = true;
    error = "";
    try {
      for (const id of list) {
        await coreClient.trashBoard(id, {});
      }
      clearBoardSelection();
      confirmModal = { open: false, action: "", entityId: "", bulkIds: null };
      await loadBoards();
    } catch (e) {
      error = `Trash failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      bulkBusy = false;
    }
  }

  function handleConfirm() {
    const bulkIds = confirmModal.bulkIds;
    const id = confirmModal.entityId;
    const action = confirmModal.action;
    confirmModal = { open: false, action: "", entityId: "", bulkIds: null };
    if (bulkIds && bulkIds.length > 0) {
      if (action === "archive") void bulkArchiveBoards(bulkIds);
      else if (action === "trash") void bulkTrashBoards(bulkIds);
      return;
    }
    if (action === "archive") void archiveBoard(id);
    else if (action === "trash") void trashBoard(id);
  }

  let boardConfirmBulkCount = $derived(confirmModal.bulkIds?.length ?? 0);
  let boardConfirmIsBulk = $derived(boardConfirmBulkCount > 0);

  let boardConfirmModalTitle = $derived.by(() => {
    if (confirmModal.action === "trash") {
      return boardConfirmIsBulk
        ? `Move ${boardConfirmBulkCount} boards to trash`
        : "Move to trash";
    }
    return boardConfirmIsBulk
      ? `Archive ${boardConfirmBulkCount} boards`
      : "Archive board";
  });

  let boardConfirmModalMessage = $derived.by(() => {
    if (confirmModal.action === "trash") {
      return boardConfirmIsBulk
        ? `These boards (${boardConfirmBulkCount}) will be moved to trash. You can restore them later.`
        : "This board will be moved to trash. You can restore it later.";
    }
    return boardConfirmIsBulk
      ? `These boards (${boardConfirmBulkCount}) will be hidden from default views. You can unarchive them later.`
      : "This board will be hidden from default views. You can unarchive it later.";
  });

  let boardConfirmModalBusy = $derived(
    confirmModal.action === "trash"
      ? Boolean(trashBusyId) || (boardConfirmIsBulk && bulkBusy)
      : Boolean(archiveBusyId) || (boardConfirmIsBulk && bulkBusy),
  );

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

  /** @param {string} value */
  function toggleBoardLifecycleState(value) {
    const cur = [...(boardFiltersDraft.states ?? ["active"])];
    const set = new Set(cur);
    if (set.has(value)) {
      if (set.size <= 1) return;
      set.delete(value);
    } else {
      set.add(value);
    }
    const order = /** @type {const} */ (["active", "archived", "trashed"]);
    boardFiltersDraft = {
      ...boardFiltersDraft,
      states: order.filter((s) => set.has(s)),
    };
  }
</script>

<div class="mb-3 flex max-md:mb-2 flex-wrap items-start justify-between gap-4">
  <div>
    <h1 class="text-subtitle font-semibold text-[var(--fg)]">Boards</h1>
  </div>

  <div class="flex flex-wrap items-center gap-3">
    <button
      class="cursor-pointer inline-flex h-7 items-center gap-1.5 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 text-micro font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] {boards.length ===
        0 && !loading
        ? 'pointer-events-none opacity-50'
        : ''}"
      onclick={toggleBoardSelectMode}
      disabled={boards.length === 0 && !loading}
      type="button"
      aria-pressed={boardSelectMode}
    >
      {boardSelectMode ? "Done" : "Select"}
    </button>
    <button
      class="cursor-pointer inline-flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-micro font-medium transition-colors {hasActiveFilters
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
    <Button
      variant="primary"
      size="compact"
      class="rounded-md"
      href={workspaceHref("/boards/new")}
    >
      New board
    </Button>
  </div>
</div>

{#if filtersOpen}
  <CompactFilterBar testId="boards-filter-panel">
    {#snippet children()}
      <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <div class="text-micro">
          <span class="font-medium text-[var(--fg-muted)]">Lifecycle</span>
          <fieldset
            class="mt-1 space-y-1 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-2"
          >
            {#each Object.entries(BOARD_STATUS_LABELS) as [value, label] (value)}
              <label
                class="flex cursor-pointer items-center gap-2 text-meta text-[var(--fg)]"
              >
                <input
                  checked={(boardFiltersDraft.states ?? ["active"]).includes(
                    value,
                  )}
                  class="h-3.5 w-3.5 cursor-pointer rounded border-[var(--line)] bg-[var(--bg)] text-[var(--accent-hover)] focus:ring-2 focus:ring-[var(--accent)] focus:ring-offset-0"
                  type="checkbox"
                  onchange={() => toggleBoardLifecycleState(value)}
                />
                {label}
              </label>
            {/each}
          </fieldset>
        </div>
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
  {#snippet boardRow(item, index, showBorderTop)}
    {@const board = item.board}
    {@const selected = selectedBoardIds.has(board.id)}
    {#if boardSelectMode}
      <div
        aria-label={`${selected ? "Deselect" : "Select"} ${board.title || board.id}`}
        aria-pressed={selected}
        class="flex cursor-pointer items-stretch outline-none transition-colors focus-visible:ring-2 focus-visible:ring-[var(--accent)] focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--panel)] {showBorderTop
          ? 'border-t border-[var(--line)]'
          : ''} {selected
          ? 'border-l-[3px] border-l-[var(--accent)] bg-[var(--accent)]/10'
          : 'border-l-[3px] border-l-transparent hover:bg-[var(--line-subtle)]'}"
        onclick={(e) => handleBoardRowClick(item, index, e)}
        onkeydown={(e) => boardRowKeydown(item, index, e)}
        role="button"
        tabindex="0"
      >
        <div class="flex shrink-0 items-center self-stretch pl-2 sm:pl-3">
          <LeadingSelectionGlyph {selected} />
        </div>
        <div
          class="pointer-events-none flex min-w-0 flex-1 items-start justify-between gap-3 px-3 py-2.5 sm:px-4"
        >
          <WorkspaceResourceListRow
            title={board.title || board.id}
            description={board.summary ?? ""}
          >
            {#snippet badges()}
              {#if board.state}
                <span
                  class="inline-flex shrink-0 rounded px-1.5 py-0.5 text-micro font-semibold {lifecycleStateColor(
                    board.state,
                  )}"
                >
                  {BOARD_STATUS_LABELS[board.state] ?? board.state}
                </span>
              {/if}
              {#if isBoardArchived(board) && board.state !== "archived"}
                <span
                  class="shrink-0 rounded bg-warn-soft px-1.5 py-0.5 text-micro font-medium text-warn-text"
                  >Archived</span
                >
              {/if}
            {/snippet}
          </WorkspaceResourceListRow>
          <div
            class="flex shrink-0 items-center gap-1.5 self-start pt-0.5 text-micro"
          >
            <span class="w-14 text-right text-[var(--fg-muted)]"
              >{formatTimestamp(board.updated_at) || "—"}</span
            >
          </div>
        </div>
      </div>
    {:else}
      <div
        class="flex items-stretch {showBorderTop
          ? 'border-t border-[var(--line)]'
          : ''}"
      >
        <div
          class="group relative min-w-0 flex-1 px-3 py-2.5 text-left transition-colors hover:bg-[var(--line-subtle)] sm:px-4"
        >
          <a
            aria-label={`Open board ${board.title || board.id}`}
            class="absolute inset-0 z-0"
            href={workspaceHref(`/boards/${board.id}`)}
          ></a>
          <div
            class="pointer-events-none relative z-10 flex min-w-0 items-start justify-between gap-3"
          >
            <WorkspaceResourceListRow
              title={board.title || board.id}
              description={board.summary ?? ""}
              titleClass="group-hover:text-accent-text"
            >
              {#snippet badges()}
                {#if board.state}
                  <span
                    class="inline-flex shrink-0 rounded px-1.5 py-0.5 text-micro font-semibold {lifecycleStateColor(
                      board.state,
                    )}"
                  >
                    {BOARD_STATUS_LABELS[board.state] ?? board.state}
                  </span>
                {/if}
                {#if isBoardArchived(board) && board.state !== "archived"}
                  <span
                    class="shrink-0 rounded bg-warn-soft px-1.5 py-0.5 text-micro font-medium text-warn-text"
                    >Archived</span
                  >
                {/if}
              {/snippet}
            </WorkspaceResourceListRow>
            <div
              class="flex shrink-0 items-center gap-1.5 self-start pt-0.5 text-micro"
            >
              <span class="w-14 text-right text-[var(--fg-muted)]"
                >{formatTimestamp(board.updated_at) || "—"}</span
              >
            </div>
          </div>
        </div>
      </div>
    {/if}
  {/snippet}
  {#if boardSelectMode}
    <WorkspaceListBulkToolbar
      allVisibleSelected={allBoardsVisibleSelected}
      busy={bulkBusy}
      canArchive={bulkBoardsCanArchive}
      canTrash={bulkBoardsCanTrash}
      canUnarchive={bulkBoardsCanUnarchive}
      onArchive={() => {
        const ids = boardIdsForBulkArchive();
        if (!ids.length) return;
        confirmModal = {
          open: true,
          action: "archive",
          entityId: "",
          bulkIds: ids,
        };
      }}
      onClear={clearBoardSelection}
      onDeselectAll={clearBoardSelection}
      onSelectAll={selectAllVisibleBoards}
      onTrash={() => {
        const ids = boardIdsForBulkTrash();
        if (!ids.length) return;
        confirmModal = {
          open: true,
          action: "trash",
          entityId: "",
          bulkIds: ids,
        };
      }}
      onUnarchive={() => void bulkUnarchiveBoards(boardIdsForBulkUnarchive())}
      selectionChromeActive={true}
      selectedCount={selectedBoardIds.size}
    />
  {/if}
  <div
    class="space-y-px overflow-hidden rounded-md border border-[var(--line)] bg-[var(--panel)]"
  >
    {#each boards as item, i}
      {@render boardRow(item, i, i > 0)}
    {/each}
  </div>
{/if}

<ConfirmModal
  open={confirmModal.open}
  title={boardConfirmModalTitle}
  message={boardConfirmModalMessage}
  confirmLabel={confirmModal.action === "trash" ? "Trash" : "Archive"}
  variant={confirmModal.action === "trash" ? "danger" : "warning"}
  busy={boardConfirmModalBusy}
  onconfirm={handleConfirm}
  oncancel={() =>
    (confirmModal = { open: false, action: "", entityId: "", bulkIds: null })}
/>
