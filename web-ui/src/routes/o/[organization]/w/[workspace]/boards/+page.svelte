<script>
  import { page } from "$app/stores";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import CompactFilterBar from "$lib/components/CompactFilterBar.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import { coreClient } from "$lib/coreClient";
  import { createWorkspaceResourceLifecycleController } from "$lib/workspaceResourceLifecycle.svelte.js";
  import { formatTimestamp } from "$lib/formatDate";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { BOARD_STATUS_LABELS, parseDelimitedValues } from "$lib/boardUtils";
  import InlineWorkspaceMetricStrip from "$lib/components/InlineWorkspaceMetricStrip.svelte";
  import { boardListColumnMetricItems } from "$lib/workspaceRowMetrics.js";
  import CopyButton from "$lib/components/CopyButton.svelte";
  import WorkspaceListRowShell from "$lib/components/WorkspaceListRowShell.svelte";
  import { copyText } from "$lib/clipboard.js";
  import WorkspaceResourceListRow from "$lib/components/WorkspaceResourceListRow.svelte";
  import WorkspaceListBulkToolbar from "$lib/components/WorkspaceListBulkToolbar.svelte";
  import LeadingSelectionGlyph from "$lib/components/LeadingSelectionGlyph.svelte";
  import LifecycleBadge from "$lib/components/LifecycleBadge.svelte";
  import Button from "$lib/components/Button.svelte";
  import { createWorkspaceListSelection } from "$lib/workspaceListSelection.svelte.js";
  import { absoluteUrl } from "$lib/absoluteUrl.js";
  import {
    resourceDisplayLabel,
    resourceRouteSegment,
  } from "$lib/resourceIdentity.js";

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
  let activeBoardListLoadToken = 0;
  let hasActiveFilters = $derived.by(() => {
    const f = boardFiltersApplied;
    const st = f.states ?? ["active"];
    const defaultLifecycle = st.length === 1 && String(st[0]) === "active";
    return !defaultLifecycle || Boolean(f.owners.trim()) || Boolean(f.q.trim());
  });
  let lifecycle = $state();

  const boardSel = createWorkspaceListSelection({
    bulkBusy: () => lifecycle?.bulkBusy ?? false,
  });

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let workspaceHref = $derived(
    bindWorkspaceHref(organizationSlug, workspaceSlug),
  );

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
    const loadToken = ++activeBoardListLoadToken;
    const loadWorkspaceSlug = workspaceSlug;
    const loadGeneration = boardListGeneration;
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
      if (
        loadToken !== activeBoardListLoadToken ||
        loadWorkspaceSlug !== workspaceSlug ||
        loadGeneration !== boardListGeneration
      ) {
        return;
      }
      boards = (data.boards ?? []).map(normalizeBoardListRow);
    } catch (e) {
      if (
        loadToken !== activeBoardListLoadToken ||
        loadWorkspaceSlug !== workspaceSlug ||
        loadGeneration !== boardListGeneration
      ) {
        return;
      }
      error = `Failed to load boards: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      if (
        loadToken === activeBoardListLoadToken &&
        loadWorkspaceSlug === workspaceSlug &&
        loadGeneration === boardListGeneration
      ) {
        loading = false;
        retrying = false;
      }
    }
  }

  let boardsHaveMixedLifecycle = $derived(
    boards.some((row) => {
      const s = String(row?.board?.state ?? "")
        .trim()
        .toLowerCase();
      return s && s !== "active";
    }),
  );

  $effect(() => {
    boardListGeneration;
    if (workspaceSlug) {
      void loadBoards();
    }
  });

  $effect(() => {
    boards;
    boardSel.reconcileSelectionWithIds(
      boards.map((row) => String(row?.board?.id ?? "").trim()).filter(Boolean),
    );
  });

  let allBoardsVisibleSelected = $derived(
    boards.length > 0 &&
      boards.every((row) => boardSel.selectedIds.has(row.board.id)),
  );
  let selectedBoardRows = $derived(
    boards.filter((row) => boardSel.selectedIds.has(row.board.id)),
  );

  lifecycle = createWorkspaceResourceLifecycleController({
    resourceSingular: "board",
    resourcePlural: "boards",
    selectedItems: () => selectedBoardRows,
    idFor: (row) => row?.board?.id,
    isArchived: (row) => isBoardArchived(row?.board),
    isTrashed: (row) => isBoardTrashed(row?.board),
    actions: {
      archive: (id) => coreClient.archiveBoard(id, {}),
      unarchive: (id) => coreClient.unarchiveBoard(id, {}),
      trash: (id) => coreClient.trashBoard(id, {}),
    },
    reload: () => loadBoards(),
    clearSelection: () => clearBoardSelection(),
    setError: (message) => {
      error = message;
    },
  });

  let bulkBoardsCanArchive = $derived(lifecycle.canArchive());
  let bulkBoardsCanUnarchive = $derived(lifecycle.canUnarchive());
  let bulkBoardsCanTrash = $derived(lifecycle.canTrash());

  function selectAllVisibleBoards() {
    boardSel.selectAllFromVisibleIds(
      boards.map((row) => row.board.id).filter(Boolean),
    );
  }

  function clearBoardSelection() {
    boardSel.clearSelection();
  }

  function toggleBoardSelectMode() {
    boardSel.toggleSelectMode();
  }

  /** @param {number} i */
  function boardIdAtVisibleIndex(i) {
    const b = boards[i]?.board;
    return String(b?.id ?? "").trim();
  }

  /** @param {number} i */
  function boardHrefAtVisibleIndex(i) {
    const segment = resourceRouteSegment(boards[i]?.board, "board");
    return workspaceHref(`/boards/${encodeURIComponent(segment)}`);
  }

  function isBoardArchived(board) {
    const at = board?.archived_at;
    return typeof at === "string" ? at.trim() !== "" : Boolean(at);
  }

  function isBoardTrashed(board) {
    return board?.state === "trashed";
  }

  let boardConfirmModalTitle = $derived(lifecycle.confirmTitle());
  let boardConfirmModalMessage = $derived(lifecycle.confirmMessage());
  let boardConfirmModalBusy = $derived(lifecycle.confirmBusy());

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

<WorkspacePageShell>
  <WorkspacePageHeader title="Boards">
    {#snippet actions()}
      <button
        class="cursor-pointer inline-flex h-7 items-center gap-1.5 rounded-md border border-line bg-bg-soft px-2.5 text-micro font-medium text-fg-muted transition-colors hover:bg-line-subtle {boards.length ===
          0 && !loading
          ? 'pointer-events-none opacity-50'
          : ''}"
        onclick={toggleBoardSelectMode}
        disabled={boards.length === 0 && !loading}
        type="button"
        aria-pressed={boardSel.selectMode}
      >
        {boardSel.selectMode ? "Done" : "Select"}
      </button>
      <button
        class="cursor-pointer inline-flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-micro font-medium transition-colors {hasActiveFilters
          ? 'border-accent bg-accent-soft text-accent hover:bg-accent-soft'
          : 'border-line bg-bg-soft text-fg-muted hover:bg-line-subtle'}"
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
    {/snippet}
  </WorkspacePageHeader>

  {#if filtersOpen}
    <CompactFilterBar testId="boards-filter-panel">
      {#snippet children()}
        <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          <div class="text-micro">
            <span class="font-medium text-fg-muted">Lifecycle</span>
            <fieldset
              class="mt-1 space-y-1 rounded-md border border-line bg-bg-soft px-2.5 py-2"
            >
              {#each Object.entries(BOARD_STATUS_LABELS) as [value, label] (value)}
                <label
                  class="flex cursor-pointer items-center gap-2 text-meta text-fg"
                >
                  <input
                    checked={(boardFiltersDraft.states ?? ["active"]).includes(
                      value,
                    )}
                    class="h-3.5 w-3.5 cursor-pointer rounded border-line bg-bg text-accent-hover focus:ring-2 focus:ring-accent focus:ring-offset-0"
                    type="checkbox"
                    onchange={() => toggleBoardLifecycleState(value)}
                  />
                  {label}
                </label>
              {/each}
            </fieldset>
          </div>
          <label class="text-micro sm:col-span-2 lg:col-span-2">
            <span class="font-medium text-fg-muted">Search</span>
            <input
              bind:value={boardFiltersDraft.q}
              class="mt-1 w-full rounded-md border border-line bg-bg-soft px-2.5 py-1.5 text-meta transition-colors focus:bg-panel"
              placeholder="Title or board id"
              type="text"
            />
          </label>
          <label class="text-micro sm:col-span-2 lg:col-span-2">
            <span class="font-medium text-fg-muted"
              >Owners (comma-separated ids)</span
            >
            <input
              bind:value={boardFiltersDraft.owners}
              class="mt-1 w-full rounded-md border border-line bg-bg-soft px-2.5 py-1.5 text-meta transition-colors focus:bg-panel"
              placeholder="actor-ops-ai"
              type="text"
            />
          </label>
        </div>
        <div class="mt-3 flex flex-wrap gap-1.5">
          <button
            class="cursor-pointer rounded-md bg-panel px-3 py-1.5 text-micro font-medium text-fg hover:bg-line"
            onclick={applyBoardFilters}
            type="button"
          >
            Apply
          </button>
          <button
            class="cursor-pointer rounded-md border border-line bg-bg-soft px-3 py-1.5 text-micro font-medium text-fg-muted hover:bg-line-subtle"
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
      {@const selected = boardSel.selectedIds.has(board.id)}
      {#if boardSel.selectMode}
        <div
          aria-label={`${selected ? "Deselect" : "Select"} ${resourceDisplayLabel(board)}`}
          aria-pressed={selected}
          class="flex cursor-pointer items-stretch outline-none transition-colors focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-panel {showBorderTop
            ? 'border-t border-line'
            : ''} {selected
            ? 'border-l-[3px] border-l-accent bg-accent-soft'
            : 'border-l-[3px] border-l-transparent hover:bg-line-subtle'}"
          onclick={(e) =>
            boardSel.handleRowMouseEvent(
              e,
              index,
              boards.length,
              boardIdAtVisibleIndex,
              boardHrefAtVisibleIndex,
            )}
          onkeydown={(e) =>
            boardSel.handleRowKeyboardEvent(e, index, boardIdAtVisibleIndex)}
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
              title={resourceDisplayLabel(board)}
              description={board.summary ?? ""}
            >
              {#snippet badges()}
                <LifecycleBadge
                  state={board.state}
                  label={BOARD_STATUS_LABELS[board.state]}
                />
                {#if isBoardArchived(board) && board.state !== "archived"}
                  <LifecycleBadge state="archived" forceShow />
                {/if}
              {/snippet}
            </WorkspaceResourceListRow>
            <div
              class="flex shrink-0 items-center gap-1.5 self-start pt-0.5 text-micro"
            >
              <span class="w-14 text-right text-fg-muted"
                >{formatTimestamp(board.updated_at) || "—"}</span
              >
            </div>
          </div>
        </div>
      {:else}
        {@const columnMetrics = boardListColumnMetricItems(
          board,
          item.listStats,
        )}
        {@const totalCards = Number(item.listStats?.card_count ?? 0)}
        {@const boardsMetricFooter = `${totalCards} card${totalCards === 1 ? "" : "s"}`}
        {@const boardLink = absoluteUrl(
          workspaceHref(
            `/boards/${encodeURIComponent(resourceRouteSegment(board, "board"))}`,
          ),
        )}
        <WorkspaceListRowShell
          class={showBorderTop ? "border-t border-line" : ""}
          contextMenuItems={[
            {
              key: "copy-link",
              label: "Copy link",
              onSelect: () => void copyText(boardLink),
            },
          ]}
        >
          {#snippet row()}
            <a
              aria-label={`Open board ${resourceDisplayLabel(board)}`}
              class="flex min-w-0 flex-1 items-start gap-3 px-3 py-2.5 text-left transition-colors hover:bg-panel-hover sm:px-4"
              href={workspaceHref(
                `/boards/${encodeURIComponent(resourceRouteSegment(board, "board"))}`,
              )}
            >
              <div class="min-w-0 flex-1">
                <WorkspaceResourceListRow
                  title={resourceDisplayLabel(board)}
                  description={board.summary ?? ""}
                  titleClass="group-hover/row:text-accent-text transition-colors"
                >
                  {#snippet badges()}
                    <LifecycleBadge
                      state={board.state}
                      label={BOARD_STATUS_LABELS[board.state]}
                      forceShow={boardsHaveMixedLifecycle}
                    />
                    {#if isBoardArchived(board) && board.state !== "archived"}
                      <LifecycleBadge state="archived" forceShow />
                    {/if}
                  {/snippet}
                </WorkspaceResourceListRow>
                <InlineWorkspaceMetricStrip
                  items={columnMetrics}
                  footer={boardsMetricFooter}
                />
              </div>
            </a>
          {/snippet}
          {#snippet meta()}
            <span class="w-14 text-right text-fg-muted"
              >{formatTimestamp(board.updated_at) || "—"}</span
            >
          {/snippet}
          {#snippet actions()}
            <CopyButton
              value={boardLink}
              iconOnly
              icon="link"
              label="Copy board link"
              size="sm"
            />
          {/snippet}
        </WorkspaceListRowShell>
      {/if}
    {/snippet}
    {#if boardSel.selectMode}
      <WorkspaceListBulkToolbar
        allVisibleSelected={allBoardsVisibleSelected}
        busy={lifecycle.bulkBusy}
        canArchive={bulkBoardsCanArchive}
        canTrash={bulkBoardsCanTrash}
        canUnarchive={bulkBoardsCanUnarchive}
        onArchive={() => {
          const ids = lifecycle.idsForArchive();
          if (!ids.length) return;
          lifecycle.openBulkConfirm("archive", ids);
        }}
        onClear={clearBoardSelection}
        onDeselectAll={clearBoardSelection}
        onSelectAll={selectAllVisibleBoards}
        onTrash={() => {
          const ids = lifecycle.idsForTrash();
          if (!ids.length) return;
          lifecycle.openBulkConfirm("trash", ids);
        }}
        onUnarchive={() =>
          void lifecycle.runBulk("unarchive", lifecycle.idsForUnarchive())}
        selectionChromeActive={true}
        selectedCount={boardSel.selectedIds.size}
      />
    {/if}
    <div
      class="space-y-px overflow-hidden rounded-md border border-line bg-panel"
    >
      {#each boards as item, i}
        {@render boardRow(item, i, i > 0)}
      {/each}
    </div>
  {/if}

  <ConfirmModal
    open={lifecycle.confirmModal.open}
    title={boardConfirmModalTitle}
    message={boardConfirmModalMessage}
    confirmLabel={lifecycle.confirmModal.action === "trash"
      ? "Trash"
      : "Archive"}
    variant={lifecycle.confirmModal.action === "trash" ? "danger" : "warning"}
    busy={boardConfirmModalBusy}
    onconfirm={() => lifecycle.handleConfirm()}
    oncancel={() => lifecycle.closeConfirm()}
  />
</WorkspacePageShell>
