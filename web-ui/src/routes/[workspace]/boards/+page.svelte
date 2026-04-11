<script>
  import { page } from "$app/stores";
  import { goto } from "$app/navigation";
  import GuidedTypedRefsInput from "$lib/components/GuidedTypedRefsInput.svelte";
  import SearchableEntityPicker from "$lib/components/SearchableEntityPicker.svelte";
  import SearchableMultiEntityPicker from "$lib/components/SearchableMultiEntityPicker.svelte";
  import ArchiveButton from "$lib/components/ArchiveButton.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import TrashButton from "$lib/components/TrashButton.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import {
    searchDocuments as searchDocumentRecords,
    searchTopics as searchTopicRecords,
    topicSearchResultToPickerOption,
  } from "$lib/searchHelpers";
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

  let boards = $state([]);
  let loading = $state(false);
  let error = $state("");
  let showArchived = $state(false);
  let archiveBusyId = $state("");
  let confirmModal = $state({ open: false, action: "", entityId: "" });
  let trashBusyId = $state("");
  let creating = $state(false);
  let createError = $state("");
  let showCreateForm = $state(false);

  let createTitle = $state("");
  let createStatus = $state("active");
  let createBackingThreadId = $state("");
  let createBoardDocumentId = $state("");
  let createLabels = $state("");
  let createOwnerIds = $state([]);
  let createPinnedRefs = $state("");

  let workspaceSlug = $derived($page.params.workspace);
  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );
  let actorOptions = $derived(
    $actorRegistry.map((actor) => ({
      id: actor.id,
      title: actor.display_name || actor.id,
      subtitle: actor.id,
      keywords: actor.tags ?? [],
    })),
  );

  function workspaceHref(pathname = "/") {
    return workspacePath(workspaceSlug, pathname);
  }

  function toDocumentOption(document) {
    return {
      id: document.id,
      title: document.title || document.id,
      subtitle: [
        document.state,
        document.thread_id && `Timeline ${document.thread_id}`,
      ]
        .filter(Boolean)
        .join(" · "),
      keywords: document.labels ?? [],
    };
  }

  async function searchThreadOptions(query) {
    const threads = await searchTopicRecords(query);
    return threads.map(topicSearchResultToPickerOption);
  }

  async function searchDocumentOptions(query) {
    const documents = await searchDocumentRecords(query);
    return documents.map(toDocumentOption);
  }

  function resetCreateForm() {
    createTitle = "";
    createStatus = "active";
    createBackingThreadId = "";
    createBoardDocumentId = "";
    createLabels = "";
    createOwnerIds = [];
    createPinnedRefs = "";
  }

  async function loadBoards() {
    loading = true;
    error = "";
    try {
      const filters = {};
      if (showArchived) filters.include_archived = "true";
      const data = await coreClient.listBoards(filters);
      boards = data.boards ?? [];
    } catch (e) {
      error = `Failed to load boards: ${e instanceof Error ? e.message : String(e)}`;
      boards = [];
    } finally {
      loading = false;
    }
  }

  async function submitCreateBoard() {
    createError = "";

    const title = createTitle.trim();
    const backingThreadId = createBackingThreadId.trim();

    if (!title || !backingThreadId) {
      createError =
        "Title and board timeline ID (backing thread) are required.";
      return;
    }

    const board = {
      title,
      status: createStatus,
      thread_id: backingThreadId,
    };
    const labels = parseDelimitedValues(createLabels);
    const owners = [...createOwnerIds];
    const pinnedRefs = parseDelimitedValues(createPinnedRefs);

    if (labels.length > 0) board.labels = labels;
    if (owners.length > 0) board.owners = owners;
    if (createBoardDocumentId.trim()) {
      board.document_refs = [`document:${createBoardDocumentId.trim()}`];
    }
    if (pinnedRefs.length > 0) board.pinned_refs = pinnedRefs;

    creating = true;
    try {
      const created = await coreClient.createBoard({ board });
      await loadBoards();
      resetCreateForm();
      showCreateForm = false;
      await goto(workspaceHref(`/boards/${created.board.id}`));
    } catch (e) {
      createError = `Failed to create board: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      creating = false;
    }
  }

  function statusColor(status) {
    if (status === "active") return "text-emerald-400 bg-emerald-500/10";
    if (status === "paused") return "text-amber-400 bg-amber-500/10";
    if (status === "closed") return "text-slate-300 bg-slate-500/10";
    return "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
  }

  $effect(() => {
    showArchived;
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
</script>

<div class="mb-4 flex flex-wrap items-start justify-between gap-4">
  <div>
    <h1 class="text-lg font-semibold text-[var(--ui-text)]">Boards</h1>
    <p class="mt-1 hidden text-[12px] text-[var(--ui-text-muted)] sm:block">
      Canonical visual progress maps over live work. Use them as a trusted scan
      surface, not a disposable kanban layer.
    </p>
  </div>

  <div class="flex flex-wrap items-center gap-3">
    <label
      class="inline-flex cursor-pointer items-center gap-1.5 text-[12px] text-[var(--ui-text-muted)]"
    >
      <input
        bind:checked={showArchived}
        class="h-3.5 w-3.5 cursor-pointer rounded border-[var(--ui-border)] bg-[var(--ui-bg)] text-[var(--ui-accent-strong)] focus:ring-2 focus:ring-[var(--ui-accent)] focus:ring-offset-0"
        type="checkbox"
      />
      Show archived
    </label>
    <button
      class="rounded-md bg-indigo-600 px-3 py-1.5 text-[12px] font-medium text-white transition-colors hover:bg-indigo-500"
      onclick={() => {
        createError = "";
        showCreateForm = !showCreateForm;
      }}
      type="button"
    >
      {showCreateForm ? "Hide create form" : "Create board"}
    </button>
  </div>
</div>

{#if error}
  <div class="mb-4 rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
    {error}
  </div>
{/if}

{#if showCreateForm}
  <section
    class="mb-5 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]"
  >
    <div class="border-b border-[var(--ui-border)] px-4 py-2.5">
      <h2 class="text-[13px] font-medium text-[var(--ui-text)]">
        Create board
      </h2>
    </div>

    <div class="space-y-3 px-4 py-3">
      {#if createError}
        <div
          class="rounded-md bg-red-500/10 px-3 py-2 text-[12px] text-red-400"
        >
          {createError}
        </div>
      {/if}

      <div class="grid gap-3 md:grid-cols-2">
        <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
          Board title
          <input
            bind:value={createTitle}
            class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
            placeholder="Q3 launch board"
            type="text"
          />
        </label>

        <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
          Status
          <select
            bind:value={createStatus}
            class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
          >
            {#each Object.entries(BOARD_STATUS_LABELS) as [value, label]}
              <option {value}>{label}</option>
            {/each}
          </select>
        </label>

        <SearchableEntityPicker
          bind:value={createBackingThreadId}
          advancedLabel="Use a manual thread ID"
          helperText="Pick a topic or enter this board's backing thread ID (append-only event timeline for the board)."
          label="Board timeline"
          manualLabel="Thread ID"
          manualPlaceholder="thread-q2-initiative"
          placeholder="Search topics by title, ID, or tags"
          searchFn={searchThreadOptions}
        />

        <SearchableEntityPicker
          bind:value={createBoardDocumentId}
          advancedLabel="Use a manual document ID"
          helperText="Optional: add a document ref to the board (included in refs)."
          label="Board document"
          manualLabel="Document ID"
          manualPlaceholder="product-constitution"
          placeholder="Search documents by title, ID, or timeline ID"
          searchFn={searchDocumentOptions}
        />
      </div>

      <div class="grid gap-3 md:grid-cols-2">
        <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
          Labels
          <textarea
            bind:value={createLabels}
            class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
            placeholder="product, launch"
            rows="3"
          ></textarea>
        </label>

        <SearchableMultiEntityPicker
          bind:values={createOwnerIds}
          advancedLabel="Add a manual owner ID"
          helperText="Owners stay visible on the board list and detail scan surfaces."
          items={actorOptions}
          label="Owners"
          manualLabel="Owner ID"
          manualPlaceholder="actor-ops-ai"
          placeholder="Search actors by name, ID, or tags"
        />
      </div>

      <div>
        <p class="text-[12px] font-medium text-[var(--ui-text-muted)]">
          Pinned refs
        </p>
        <GuidedTypedRefsInput
          bind:value={createPinnedRefs}
          addInputLabel="Add board pinned ref"
          addInputPlaceholder="thread:thread-q2-initiative"
          addButtonLabel="Add ref"
          emptyText="No pinned refs yet."
          helperText="Pinned refs appear in the board header."
          textareaAriaLabel="Board pinned refs"
        />
      </div>

      <div class="flex flex-wrap gap-2">
        <button
          class="rounded-md bg-indigo-600 px-3 py-1.5 text-[12px] font-medium text-white transition-colors hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-60"
          disabled={creating}
          onclick={submitCreateBoard}
          type="button"
        >
          {creating ? "Creating..." : "Create board"}
        </button>
        <button
          class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)] hover:text-[var(--ui-text)]"
          onclick={() => {
            showCreateForm = false;
            createError = "";
          }}
          type="button"
        >
          Cancel
        </button>
      </div>
    </div>
  </section>
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
    Loading boards...
  </div>
{:else if boards.length === 0}
  <div class="mt-8 text-center">
    <p class="text-[13px] font-medium text-[var(--ui-text-muted)]">
      No boards yet
    </p>
    <p class="mt-1 text-[13px] text-[var(--ui-text-muted)]">
      No boards yet. Create one to give operators a trustworthy visual map of
      active work.
    </p>
  </div>
{:else}
  <div
    class="space-y-px overflow-hidden rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]"
  >
    {#each boards as item, i}
      {@const board = item.board}
      {@const summary = item.summary}
      {@const counts = boardSummaryCounts(summary)}
      {@const projectionFreshness = item.projection_freshness ?? null}
      {@const rowNav = boardRowInspectNav(board)}
      <div
        class="flex items-stretch {i > 0
          ? 'border-t border-[var(--ui-border)]'
          : ''}"
      >
        <div
          class="group relative min-w-0 flex-1 px-4 py-3 text-left transition-colors hover:bg-[var(--ui-border-subtle)]"
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
                {#if board.status}
                  <span
                    class="inline-flex rounded px-1.5 py-0.5 text-[11px] font-semibold {statusColor(
                      board.status,
                    )}"
                  >
                    {BOARD_STATUS_LABELS[board.status] ?? board.status}
                  </span>
                {/if}
                {#if isBoardArchived(board)}
                  <span
                    class="rounded bg-amber-500/15 px-1.5 py-0.5 text-[11px] font-medium text-amber-400"
                    >Archived</span
                  >
                {/if}
                {#if projectionFreshness}
                  <span
                    class="inline-flex rounded px-1.5 py-0.5 text-[10px] font-medium {freshnessStatusTone(
                      projectionFreshness.status,
                    )}"
                  >
                    {freshnessStatusLabel(projectionFreshness.status)}
                  </span>
                {/if}
                {#if summary?.has_document_ref}
                  <span
                    class="rounded bg-indigo-500/10 px-1.5 py-0.5 text-[10px] text-indigo-300"
                  >
                    Has doc
                  </span>
                {/if}
                {#each (board.labels ?? []).slice(0, 3) as label}
                  <span
                    class="rounded bg-[var(--ui-border)] px-1.5 py-0.5 text-[10px] text-[var(--ui-text-muted)]"
                  >
                    {label}
                  </span>
                {/each}
              </div>

              <span
                class="mt-1 block truncate text-[13px] font-medium text-[var(--ui-text)] group-hover:text-indigo-300"
              >
                {board.title || board.id}
              </span>

              <div
                class="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-[var(--ui-text-muted)]"
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
                    <span class="text-[var(--ui-text-muted)]"
                      >{rowNav.kind === "topic"
                        ? "Topic"
                        : "Backing thread"}:</span
                    >
                    <a
                      class="pointer-events-auto relative z-20 text-indigo-400 transition-colors hover:text-indigo-300"
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
                    <span class="text-[var(--ui-text-muted)]">Context:</span>
                    <span class="text-[var(--ui-text-muted)]">—</span>
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
                  class="mt-1.5 flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-[10px]"
                >
                  {#each CANONICAL_BOARD_COLUMNS as column, ci}
                    {@const count = counts[column.key]}
                    <span
                      class={column.key === "blocked" && count > 0
                        ? "text-amber-400"
                        : "text-[var(--ui-text-muted)]"}
                    >
                      <span class="font-medium uppercase">{column.title}</span>
                      {count}
                    </span>
                    {#if ci < CANONICAL_BOARD_COLUMNS.length - 1}
                      <span class="text-[var(--ui-border)]">·</span>
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
