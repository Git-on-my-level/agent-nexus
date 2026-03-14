<script>
  import { page } from "$app/stores";
  import { goto } from "$app/navigation";
  import GuidedTypedRefsInput from "$lib/components/GuidedTypedRefsInput.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { projectPath } from "$lib/projectPaths";
  import { lookupActorDisplayName, actorRegistry } from "$lib/actorSession";
  import {
    BOARD_STATUS_LABELS,
    CANONICAL_BOARD_COLUMNS,
    boardSummaryCounts,
    parseDelimitedValues,
  } from "$lib/boardUtils";

  let boards = $state([]);
  let loading = $state(false);
  let error = $state("");
  let creating = $state(false);
  let createError = $state("");
  let supportError = $state("");
  let showCreateForm = $state(false);
  let availableThreads = $state([]);
  let availableDocuments = $state([]);

  let createTitle = $state("");
  let createStatus = $state("active");
  let createPrimaryThreadId = $state("");
  let createPrimaryDocumentId = $state("");
  let createLabels = $state("");
  let createOwners = $state("");
  let createPinnedRefs = $state("");

  let projectSlug = $derived($page.params.project);
  let actorName = $derived((id) => lookupActorDisplayName(id, $actorRegistry));

  function projectHref(pathname = "/") {
    return projectPath(projectSlug, pathname);
  }

  function navigateToBoard(boardId) {
    goto(projectHref(`/boards/${boardId}`));
  }

  function resetCreateForm() {
    createTitle = "";
    createStatus = "active";
    createPrimaryThreadId = "";
    createPrimaryDocumentId = "";
    createLabels = "";
    createOwners = "";
    createPinnedRefs = "";
  }

  function threadHint(threadId) {
    const thread = availableThreads.find((item) => item.id === threadId);
    return thread?.title ?? "";
  }

  async function loadBoards() {
    loading = true;
    error = "";
    try {
      const data = await coreClient.listBoards({});
      boards = data.boards ?? [];
    } catch (e) {
      error = `Failed to load boards: ${e instanceof Error ? e.message : String(e)}`;
      boards = [];
    } finally {
      loading = false;
    }
  }

  async function loadCreateSupport() {
    supportError = "";
    try {
      const [threadData, documentData] = await Promise.all([
        coreClient.listThreads({}),
        coreClient.listDocuments({}),
      ]);
      availableThreads = threadData.threads ?? [];
      availableDocuments = documentData.documents ?? [];
    } catch (e) {
      supportError = `Failed to load board form options: ${e instanceof Error ? e.message : String(e)}`;
      availableThreads = [];
      availableDocuments = [];
    }
  }

  async function submitCreateBoard() {
    createError = "";

    const title = createTitle.trim();
    const primaryThreadId = createPrimaryThreadId.trim();

    if (!title || !primaryThreadId) {
      createError = "Title and primary thread are required.";
      return;
    }

    const board = {
      title,
      status: createStatus,
      primary_thread_id: primaryThreadId,
    };
    const labels = parseDelimitedValues(createLabels);
    const owners = parseDelimitedValues(createOwners);
    const pinnedRefs = parseDelimitedValues(createPinnedRefs);

    if (labels.length > 0) board.labels = labels;
    if (owners.length > 0) board.owners = owners;
    if (createPrimaryDocumentId.trim()) {
      board.primary_document_id = createPrimaryDocumentId.trim();
    }
    if (pinnedRefs.length > 0) board.pinned_refs = pinnedRefs;

    creating = true;
    try {
      const created = await coreClient.createBoard({ board });
      await loadBoards();
      resetCreateForm();
      showCreateForm = false;
      await goto(projectHref(`/boards/${created.board.id}`));
    } catch (e) {
      createError = `Failed to create board: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      creating = false;
    }
  }

  function statusColor(status) {
    if (status === "active") return "text-emerald-400 bg-emerald-500/10";
    if (status === "paused") return "text-amber-300 bg-amber-500/10";
    if (status === "closed") return "text-slate-300 bg-slate-500/10";
    return "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
  }

  $effect(() => {
    if (projectSlug) {
      void loadBoards();
      void loadCreateSupport();
    }
  });
</script>

<datalist id="board-create-thread-options">
  {#each availableThreads as thread}
    <option value={thread.id}>{thread.title}</option>
  {/each}
</datalist>

<datalist id="board-create-document-options">
  {#each availableDocuments as document}
    <option value={document.id}>{document.title}</option>
  {/each}
</datalist>

<div class="mb-4 flex items-start justify-between gap-4">
  <div>
    <h1 class="text-lg font-semibold text-[var(--ui-text)]">Boards</h1>
    <p class="mt-1 text-[12px] text-[var(--ui-text-muted)]">
      Kanban boards for this project
    </p>
  </div>

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

      {#if supportError}
        <div
          class="rounded-md bg-amber-500/10 px-3 py-2 text-[12px] text-amber-400"
        >
          {supportError}
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

        <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
          Primary thread ID
          <input
            bind:value={createPrimaryThreadId}
            class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
            list="board-create-thread-options"
            placeholder="thread-q2-initiative"
            type="text"
          />
          {#if threadHint(createPrimaryThreadId)}
            <span class="mt-1 block text-[11px] text-[var(--ui-text-subtle)]">
              {threadHint(createPrimaryThreadId)}
            </span>
          {/if}
        </label>

        <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
          Primary document ID
          <input
            bind:value={createPrimaryDocumentId}
            class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
            list="board-create-document-options"
            placeholder="product-constitution"
            type="text"
          />
        </label>
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

        <label class="text-[12px] font-medium text-[var(--ui-text-muted)]">
          Owners
          <textarea
            bind:value={createOwners}
            class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-3 py-2 text-[13px] text-[var(--ui-text)]"
            placeholder="actor-ops-ai"
            rows="3"
          ></textarea>
        </label>
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
      Create one to start organizing work into canonical columns.
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
      <div
        class="block cursor-pointer px-4 py-3 transition-colors hover:bg-[var(--ui-border-subtle)] {i >
        0
          ? 'border-t border-[var(--ui-border)]'
          : ''}"
        onclick={() => navigateToBoard(board.id)}
        onkeydown={(event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            navigateToBoard(board.id);
          }
        }}
        role="button"
        tabindex="0"
      >
        <div class="flex items-start justify-between gap-3">
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
              {#if summary?.has_primary_document}
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

            <p
              class="mt-1 truncate text-[13px] font-medium text-[var(--ui-text)]"
            >
              {board.title || board.id}
            </p>

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
              <span>
                <span class="text-[var(--ui-text-subtle)]">Thread:</span>
                <a
                  class="text-indigo-300 transition-colors hover:text-indigo-200"
                  href={projectHref(
                    `/threads/${encodeURIComponent(board.primary_thread_id)}`,
                  )}
                >
                  {threadHint(board.primary_thread_id) || board.primary_thread_id}
                </a>
              </span>
              <span>
                Activity {formatTimestamp(summary?.latest_activity_at) ||
                  formatTimestamp(board.updated_at) ||
                  "—"}
              </span>
              <span>
                Board updated {formatTimestamp(board.updated_at) || "—"} by {actorName(
                  board.updated_by,
                )}
              </span>
            </div>
          </div>

          <div class="shrink-0 text-[11px] text-[var(--ui-text-subtle)]">
            <div class="flex gap-1">
              {#each CANONICAL_BOARD_COLUMNS as column}
                <span
                  class="rounded bg-[var(--ui-border)] px-1.5 py-0.5"
                  title={`${column.title}: ${counts[column.key]} cards`}
                >
                  {counts[column.key]}
                </span>
              {/each}
            </div>
            <p class="mt-1 text-right text-[10px] text-[var(--ui-text-subtle)]">
              {summary?.card_count ?? 0} cards by column
            </p>
          </div>
        </div>
      </div>
    {/each}
  </div>
{/if}
