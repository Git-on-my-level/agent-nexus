<script>
  import { page } from "$app/stores";
  import { goto } from "$app/navigation";
  import GuidedTypedRefsInput from "$lib/components/GuidedTypedRefsInput.svelte";
  import SearchableEntityPicker from "$lib/components/SearchableEntityPicker.svelte";
  import SearchableMultiEntityPicker from "$lib/components/SearchableMultiEntityPicker.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import { actorRegistry } from "$lib/actorSession";
  import { coreClient } from "$lib/coreClient";
  import {
    searchDocuments as searchDocumentRecords,
    searchTopics as searchTopicRecords,
    topicSearchResultToBoardRefOption,
  } from "$lib/searchHelpers";
  import { toActorPickerOptions } from "$lib/systemActor.js";
  import { workspacePath } from "$lib/workspacePaths";
  import { boardPrimaryTopicRef } from "$lib/topicRouteUtils";
  import {
    firstBoardDocumentId,
    joinDelimitedValues,
    parseDelimitedValues,
  } from "$lib/boardUtils";

  let loading = $state(true);
  let loadError = $state("");
  let saving = $state(false);
  let saveError = $state("");

  let board = $state(null);
  let boardUpdatedAt = $state("");

  let boardTitle = $state("");
  let boardLinkedTopicRef = $state("");
  let boardDocumentId = $state("");
  let boardOwners = $state([]);
  let boardPinnedRefs = $state("");

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let boardId = $derived($page.params.boardId);
  let actorOptions = $derived(toActorPickerOptions($actorRegistry));

  function boardHref() {
    return workspacePath(organizationSlug, workspaceSlug, `/boards/${boardId}`);
  }

  async function searchTopicLinkOptions(query) {
    const topics = await searchTopicRecords(query);
    return topics.map(topicSearchResultToBoardRefOption);
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

  function syncDrafts(b) {
    boardTitle = b?.title ?? "";
    boardLinkedTopicRef = boardPrimaryTopicRef(b);
    boardDocumentId = firstBoardDocumentId(b);
    boardOwners = [...(b?.owners ?? [])];
    boardPinnedRefs = joinDelimitedValues(b?.pinned_refs ?? []);
    boardUpdatedAt = b?.updated_at ?? "";
  }

  async function loadBoard() {
    loading = true;
    loadError = "";
    try {
      const workspace = await coreClient.getBoardWorkspace(boardId);
      board = workspace?.board ?? null;
      syncDrafts(board);
    } catch (e) {
      loadError = `Failed to load board: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      loading = false;
    }
  }

  async function submit() {
    saveError = "";
    const title = boardTitle.trim();
    if (!title) {
      saveError = "Board title is required.";
      return;
    }
    saving = true;
    try {
      const docId = boardDocumentId.trim();
      const linkedTopic = boardLinkedTopicRef.trim();
      const patch = {
        title,
        primary_topic_ref: linkedTopic || null,
        document_refs: docId ? [`document:${docId}`] : [],
        owners: [...boardOwners],
        pinned_refs: parseDelimitedValues(boardPinnedRefs),
      };
      await coreClient.updateBoard(boardId, {
        if_updated_at: boardUpdatedAt,
        patch,
      });
      await goto(boardHref());
    } catch (e) {
      if (e?.status === 409) {
        saveError =
          "Board was updated elsewhere. Reload to get the latest version.";
        await loadBoard();
      } else {
        saveError = `Failed to save: ${e instanceof Error ? e.message : String(e)}`;
      }
    } finally {
      saving = false;
    }
  }

  $effect(() => {
    if (workspaceSlug && boardId) {
      void loadBoard();
    }
  });
</script>

<div class="mx-auto max-w-2xl">
  <div class="mb-6">
    <a
      class="text-micro text-[var(--fg-muted)] transition-colors hover:text-[var(--fg)]"
      href={boardHref()}
    >
      ← {board?.title || "Board"}
    </a>
    <h1 class="mt-2 text-subtitle font-semibold text-[var(--fg)]">
      Board settings
    </h1>
  </div>

  {#if loading}
    <Skeleton rows={6} />
  {:else if loadError}
    <StateError message={loadError} onretry={loadBoard} />
  {:else}
    {#if saveError}
      <div
        class="mb-4 rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
      >
        {saveError}
      </div>
    {/if}

    <div
      class="space-y-5 rounded-md border border-[var(--line)] bg-[var(--panel)] p-5"
    >
      <label class="block text-micro font-medium text-[var(--fg-muted)]">
        Board title
        <input
          bind:value={boardTitle}
          class="mt-1.5 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-meta text-[var(--fg)] focus:border-[var(--accent)] focus:outline-none"
          type="text"
          onkeydown={(e) => e.key === "Enter" && submit()}
        />
      </label>

      <SearchableEntityPicker
        bind:value={boardLinkedTopicRef}
        advancedLabel="Enter a topic ref manually"
        helperText="Links this board to a topic for navigation and scan context."
        label="Linked topic"
        manualLabel="Topic ref"
        manualPlaceholder="topic:…"
        placeholder="Search topics by title or id"
        searchFn={searchTopicLinkOptions}
      />

      <SearchableEntityPicker
        bind:value={boardDocumentId}
        advancedLabel="Use a manual document ID"
        helperText="Optional: add or replace the board document ref surfaced in board refs."
        label="Board document"
        manualLabel="Document ID"
        manualPlaceholder="incident-response-playbook"
        placeholder="Search documents by title, ID, or timeline ID"
        searchFn={searchDocumentOptions}
      />

      <SearchableMultiEntityPicker
        bind:values={boardOwners}
        advancedLabel="Add a manual owner ID"
        helperText="Owners are shown in board list and detail views."
        items={actorOptions}
        label="Owners"
        manualLabel="Owner ID"
        manualPlaceholder="actor-ops-ai"
        placeholder="Search actors by name, ID, or tags"
      />

      <div>
        <p class="text-micro font-medium text-[var(--fg-muted)]">Pinned refs</p>
        <GuidedTypedRefsInput
          bind:value={boardPinnedRefs}
          addInputLabel="Add board pinned ref"
          addInputPlaceholder="thread:board-q2-initiative"
          addButtonLabel="Add ref"
          emptyText="No pinned refs yet."
          helperText="These refs are shown at the top of the board."
          textareaAriaLabel="Board pinned refs"
        />
      </div>

      <div class="flex gap-2 border-t border-[var(--line)] pt-4">
        <button
          class="rounded-md bg-accent-solid px-4 py-2 text-meta font-medium text-white transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-60"
          disabled={saving}
          onclick={submit}
          type="button"
        >
          {saving ? "Saving…" : "Save changes"}
        </button>
        <a
          class="rounded-md border border-[var(--line)] bg-[var(--panel)] px-4 py-2 text-meta font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] hover:text-[var(--fg)]"
          href={boardHref()}
        >
          Cancel
        </a>
      </div>
    </div>
  {/if}
</div>
