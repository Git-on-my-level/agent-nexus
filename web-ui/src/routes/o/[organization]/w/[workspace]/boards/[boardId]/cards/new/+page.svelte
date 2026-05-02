<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import GuidedTypedRefsInput from "$lib/components/GuidedTypedRefsInput.svelte";
  import SearchableEntityPicker from "$lib/components/SearchableEntityPicker.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import { actorRegistry } from "$lib/actorSession";
  import { coreClient } from "$lib/coreClient";
  import { boardBackingThreadId, boardColumnTitle } from "$lib/boardUtils";
  import {
    backingThreadIdFromTopicRecord,
    searchDocuments as searchDocumentRecords,
    searchTopics as searchTopicRecords,
    topicSearchResultToPickerOption,
  } from "$lib/searchHelpers";
  import {
    orderPickerOptionsByRecent,
    readRecentAssigneeIds,
    touchRecentAssigneeIds,
  } from "$lib/recentAssignees.js";
  import { toActorPickerOptions } from "$lib/systemActor.js";
  import { workspacePath } from "$lib/workspacePaths";

  let loading = $state(true);
  let loadError = $state("");
  let saving = $state(false);
  let saveError = $state("");
  let conflictWarning = $state("");

  let board = $state(null);
  let showMoreOptions = $state(false);
  /** Last workspace/board id pair used to reset the form; plain object to avoid re-running effects. */
  const formNavKey = { last: "" };
  /** @type {HTMLInputElement | null} */
  let titleInputEl = $state(null);
  let shouldFocusTitle = $state(false);

  let titleValue = $state("");
  let summary = $state("");
  let columnKey = $state("backlog");
  let threadId = $state("");
  let documentId = $state("");
  let risk = $state("medium");
  let resolutionRefs = $state("");
  let relatedRefs = $state("");
  let assignees = $state([]);
  let dueAt = $state("");
  let definitionOfDone = $state("");

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let boardId = $derived($page.params.boardId);
  let actorOptions = $derived(
    orderPickerOptionsByRecent(
      toActorPickerOptions($actorRegistry),
      readRecentAssigneeIds(),
    ),
  );
  let backingThreadId = $derived(board ? boardBackingThreadId(board) : "");
  let attachContextRefs = $derived(
    [
      threadId.trim() ? `thread:${threadId.trim()}` : "",
      boardId ? `board:${boardId}` : "",
    ].filter(Boolean),
  );

  function boardHref() {
    return workspacePath(organizationSlug, workspaceSlug, `/boards/${boardId}`);
  }

  function toggleMoreOptions() {
    showMoreOptions = !showMoreOptions;
  }

  async function searchThreadOptions(query) {
    const threads = await searchTopicRecords(query);
    return threads.map(topicSearchResultToPickerOption);
  }

  async function searchDocumentOptions(query) {
    const documents = await searchDocumentRecords(query);
    return documents.map((document) => ({
      id: document.id,
      title: document.title || document.id,
      subtitle: [
        document.state,
        document.thread_id && `Timeline ${document.thread_id}`,
      ]
        .filter(Boolean)
        .join(" · "),
      keywords: [],
    }));
  }

  function syncFromBoard(b) {
    if (!b?.column_schema?.length) return;
    const keys = b.column_schema.map((c) => c.key);
    if (!keys.includes(columnKey)) {
      columnKey = keys.includes("backlog") ? "backlog" : (keys[0] ?? "backlog");
    }
  }

  function resetForm() {
    shouldFocusTitle = true;
    titleValue = "";
    summary = "";
    threadId = "";
    documentId = "";
    risk = "medium";
    resolutionRefs = "";
    relatedRefs = "";
    assignees = [];
    dueAt = "";
    definitionOfDone = "";
    showMoreOptions = false;
    saveError = "";
    conflictWarning = "";
    columnKey = "backlog";
  }

  /** @param {{ quiet?: boolean }} [opts] */
  async function loadBoard(opts = {}) {
    const quiet = opts.quiet === true;
    if (!quiet) {
      loading = true;
    }
    loadError = "";
    try {
      const workspace = await coreClient.getBoardWorkspace(boardId);
      board = workspace?.board ?? null;
      syncFromBoard(board);
    } catch (e) {
      loadError = `Failed to load board: ${e instanceof Error ? e.message : String(e)}`;
      board = null;
    } finally {
      if (!quiet) {
        loading = false;
      }
    }
  }

  async function submit() {
    saveError = "";
    conflictWarning = "";
    if (!board) return;

    let resolvedTitle = titleValue.trim();
    const summaryTrim = summary.trim();
    const threadTrim = threadId.trim();
    if (!resolvedTitle && threadTrim) {
      try {
        const topics = await searchTopicRecords(threadTrim);
        const match =
          topics.find(
            (t) => backingThreadIdFromTopicRecord(t) === threadTrim,
          ) ?? topics[0];
        resolvedTitle = String(match?.title ?? "").trim() || threadTrim;
      } catch {
        resolvedTitle = threadTrim;
      }
    }
    if (!resolvedTitle && !threadTrim) {
      saveError =
        "Enter a card title, or link a topic or backing thread (timeline ID) under More options.";
      return;
    }
    if (!resolvedTitle) {
      saveError = "Card title is required.";
      return;
    }

    const related_refs = String(relatedRefs ?? "")
      .split(/\r?\n|,/)
      .map((item) => item.trim())
      .filter(Boolean);
    if (threadTrim) {
      const token = `thread:${threadTrim}`;
      if (!related_refs.includes(token)) {
        related_refs.push(token);
      }
    }

    const summaryOut = summaryTrim || resolvedTitle;
    saving = true;
    try {
      await coreClient.addBoardCard(boardId, {
        if_board_updated_at: board.updated_at,
        title: resolvedTitle,
        summary: summaryOut,
        column_key: columnKey,
        document_ref: documentId.trim()
          ? `document:${documentId.trim()}`
          : null,
        assignee_refs: [...assignees],
        risk,
        resolution: null,
        resolution_refs: String(resolutionRefs ?? "")
          .split(/\r?\n|,/)
          .map((item) => item.trim())
          .filter(Boolean),
        related_refs,
        due_at: dueAt.trim() || null,
        definition_of_done: String(definitionOfDone ?? "")
          .split(/\r?\n|,/)
          .map((item) => item.trim())
          .filter(Boolean),
      });
      touchRecentAssigneeIds(assignees);
      await goto(boardHref());
    } catch (e) {
      if (e?.status === 409) {
        conflictWarning =
          "Board was updated elsewhere. Reloading latest board. Reapply your change.";
        await loadBoard({ quiet: true });
      } else {
        saveError = `Failed to add card: ${e instanceof Error ? e.message : String(e)}`;
      }
    } finally {
      saving = false;
    }
  }

  $effect(() => {
    if (!workspaceSlug || !boardId) {
      return;
    }
    const key = `${workspaceSlug}/${boardId}`;
    if (key !== formNavKey.last) {
      formNavKey.last = key;
      resetForm();
    }
    void loadBoard();
  });

  $effect(() => {
    if (!shouldFocusTitle || loading || !board || !titleInputEl) {
      return;
    }
    shouldFocusTitle = false;
    titleInputEl.focus();
  });
</script>

<div class="mx-auto max-w-2xl">
  <div class="mb-6">
    <a
      class="text-micro text-fg-muted transition-colors hover:text-fg"
      href={boardHref()}
    >
      ← {board?.title || "Board"}
    </a>
    <h1 class="mt-2 text-subtitle font-semibold text-fg">Add card</h1>
  </div>

  {#if loading}
    <Skeleton rows={8} />
  {:else if loadError}
    <StateError message={loadError} onretry={loadBoard} />
  {:else if !board}
    <p class="text-meta text-fg-muted">Board not found.</p>
  {:else}
    {#if saveError}
      <div
        class="mb-4 rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
      >
        {saveError}
      </div>
    {/if}
    {#if conflictWarning}
      <div
        class="mb-4 rounded-md bg-warn-soft px-3 py-2 text-meta text-warn-text"
      >
        {conflictWarning}
      </div>
    {/if}

    <div class="space-y-5 rounded-md border border-line bg-panel p-5">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-start">
        <label class="min-w-0 flex-1 text-micro font-medium text-fg-muted">
          Card title
          <input
            autocomplete="off"
            class="mt-1.5 w-full rounded-md border border-line bg-bg-soft px-3 py-2 text-meta text-fg focus:border-accent focus:outline-none"
            placeholder="Short label for the card"
            type="text"
            bind:value={titleValue}
            bind:this={titleInputEl}
            onkeydown={(e) => e.key === "Enter" && e.preventDefault()}
          />
        </label>
        <label class="shrink-0 text-micro font-medium text-fg-muted sm:pt-0.5">
          Column
          <select
            class="mt-1.5 block w-full min-w-[9rem] rounded-md border border-line bg-bg-soft px-3 py-2 text-meta text-fg focus:border-accent focus:outline-none"
            bind:value={columnKey}
          >
            {#each board.column_schema as column (column.key)}
              <option value={column.key}>
                {column.title ||
                  boardColumnTitle(column.key, board.column_schema)}
              </option>
            {/each}
          </select>
        </label>
      </div>

      <label class="block text-micro font-medium text-fg-muted">
        Summary
        <textarea
          class="mt-1.5 w-full rounded-md border border-line bg-bg-soft px-3 py-2 text-meta text-fg focus:border-accent focus:outline-none"
          placeholder="What this card is about (shown on the board)"
          rows="4"
          bind:value={summary}
        ></textarea>
      </label>

      <div>
        <button
          class="flex items-center gap-1.5 text-micro text-fg-muted transition-colors hover:text-fg"
          type="button"
          aria-expanded={showMoreOptions}
          onclick={toggleMoreOptions}
        >
          <svg
            class="h-3.5 w-3.5 transition-transform {showMoreOptions
              ? 'rotate-90'
              : ''}"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="2"
            aria-hidden="true"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M9 5l7 7-7 7"
            />
          </svg>
          {showMoreOptions ? "Fewer options" : "More options"}
        </button>

        {#if showMoreOptions}
          <div
            class="mt-3 space-y-3 rounded-md border border-line bg-bg-soft p-3"
          >
            <div class="grid gap-3 md:grid-cols-2">
              <SearchableEntityPicker
                bind:value={threadId}
                advancedLabel="Use a manual thread ID"
                disabledIds={[backingThreadId].filter(Boolean)}
                helperText="Optional: pick a topic or paste a thread ID."
                label="Topic or thread"
                manualLabel="Thread ID"
                manualPlaceholder="thread-onboarding"
                placeholder="Search topics by title or ID"
                searchFn={searchThreadOptions}
              />

              <SearchableEntityPicker
                bind:value={documentId}
                helperText="Optional: surface a doc lineage on the card."
                label="Document"
                placeholder="Search documents by title, ID, or timeline ID"
                searchFn={searchDocumentOptions}
                showManualEntry={false}
              />

              <SearchableEntityPicker
                mode="multi"
                bind:values={assignees}
                helperText="Optional assignees for the card."
                items={actorOptions}
                label="Assignees"
                placeholder="Search people and agents"
                showManualEntry={false}
              />

              <label class="text-micro font-medium text-fg-muted">
                Risk
                <select
                  class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-1.5 text-meta text-fg"
                  bind:value={risk}
                >
                  <option value="low">Low</option>
                  <option value="medium">Medium</option>
                  <option value="high">High</option>
                  <option value="critical">Critical</option>
                </select>
              </label>

              <label class="text-micro font-medium text-fg-muted">
                Due date
                <input
                  class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-1.5 text-meta text-fg"
                  type="datetime-local"
                  bind:value={dueAt}
                />
              </label>

              <label class="text-micro font-medium text-fg-muted md:col-span-2">
                Definition of done
                <textarea
                  class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-1.5 text-meta text-fg"
                  rows="3"
                  bind:value={definitionOfDone}
                ></textarea>
              </label>

              <div class="md:col-span-2">
                <p class="text-micro font-medium text-fg-muted">Related refs</p>
                <GuidedTypedRefsInput
                  bind:value={relatedRefs}
                  {boardId}
                  threadId={threadId.trim()}
                  {attachContextRefs}
                  addInputLabel="Add related ref"
                  addInputPlaceholder="topic:summer-menu-rollout"
                  addButtonLabel="Add ref"
                  emptyText="No related refs yet."
                  helperText="Optional typed refs (topic:, document:, board:, thread:, …)."
                  textareaAriaLabel="Card related refs"
                />
              </div>

              <div class="md:col-span-2">
                <p class="text-micro font-medium text-fg-muted">
                  Resolution evidence
                </p>
                <GuidedTypedRefsInput
                  bind:value={resolutionRefs}
                  {boardId}
                  threadId={threadId.trim()}
                  {attachContextRefs}
                  addInputLabel="Add resolution ref"
                  addInputPlaceholder="artifact:supporting-context"
                  addButtonLabel="Add ref"
                  emptyText="No resolution evidence yet."
                  helperText="Optional typed refs that evidence the card's resolution."
                  textareaAriaLabel="Card resolution refs"
                />
              </div>
            </div>
          </div>
        {/if}
      </div>

      <div class="flex flex-wrap gap-2 border-t border-line pt-4">
        <button
          class="rounded-md bg-accent-solid px-4 py-2 text-meta font-medium text-white transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-60"
          type="button"
          disabled={saving}
          onclick={submit}
        >
          {saving ? "Adding…" : "Add card"}
        </button>
        <a
          class="rounded-md border border-line bg-panel px-4 py-2 text-meta font-medium text-fg-muted transition-colors hover:bg-line-subtle hover:text-fg"
          href={boardHref()}
        >
          Cancel
        </a>
      </div>
    </div>
  {/if}
</div>
