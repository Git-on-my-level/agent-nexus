<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import SearchableEntityPicker from "$lib/components/SearchableEntityPicker.svelte";
  import { coreClient } from "$lib/coreClient";
  import { filterTopLevelDocuments } from "$lib/documentVisibility";
  import { formatTimestamp } from "$lib/formatDate";
  import {
    searchTopics as searchTopicRecords,
    topicSearchResultToPickerOption,
  } from "$lib/searchHelpers";
  import { workspacePath } from "$lib/workspacePaths";
  import {
    lookupActorDisplayName,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";

  const DOC_STATUS_LABELS = { draft: "Draft", active: "Active" };

  let documents = $state([]);
  let loading = $state(false);
  let error = $state("");
  let showArchived = $state(false);
  let archiveBusyId = $state("");
  let confirmModal = $state({ open: false, action: "", entityId: "" });
  let trashBusyId = $state("");
  let workspaceSlug = $derived($page.params.workspace);
  let scopedThreadId = $derived(
    String($page.url.searchParams.get("thread_id") ?? "").trim(),
  );
  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );

  let groupByLabel = $state(
    browser && localStorage.getItem("oar-docs-group-by-label") === "true",
  );
  let collapsedGroups = $state(new Set());

  let groupedDocs = $derived.by(() => {
    if (!groupByLabel) return null;
    /** @type {Record<string, typeof documents>} */
    const groups = {};
    for (const doc of documents) {
      const label = (doc.labels ?? [])[0] || "__ungrouped__";
      if (!groups[label]) groups[label] = [];
      groups[label].push(doc);
    }
    return Object.entries(groups).sort(([a], [b]) => {
      if (a === "__ungrouped__") return 1;
      if (b === "__ungrouped__") return -1;
      return a.localeCompare(b);
    });
  });

  function toggleGrouping() {
    groupByLabel = !groupByLabel;
    collapsedGroups = new Set();
    if (browser)
      localStorage.setItem("oar-docs-group-by-label", String(groupByLabel));
  }

  function toggleGroup(label) {
    const next = new Set(collapsedGroups);
    if (next.has(label)) next.delete(label);
    else next.add(label);
    collapsedGroups = next;
  }

  let createOpen = $state(false);
  let creating = $state(false);
  let createError = $state("");

  let draft = $state({
    id: "",
    title: "",
    status: "draft",
    labels: "",
    thread_id: "",
    content: "",
  });

  function workspaceHref(pathname = "/") {
    return workspacePath(workspaceSlug, pathname);
  }

  async function searchThreadOptions(query) {
    const threads = await searchTopicRecords(query);
    return threads.map(topicSearchResultToPickerOption);
  }

  $effect(() => {
    showArchived;
    const threadId = scopedThreadId;
    if (workspaceSlug) {
      void loadDocuments(threadId);
    }
  });

  async function loadDocuments(threadId = "") {
    loading = true;
    error = "";
    try {
      const filters = {};
      if (threadId) filters.thread_id = threadId;
      if (showArchived) filters.include_archived = "true";
      const data = await coreClient.listDocuments(filters);
      documents = filterTopLevelDocuments(data.documents);
    } catch (e) {
      error = `Failed to load documents: ${e instanceof Error ? e.message : String(e)}`;
      documents = [];
    } finally {
      loading = false;
    }
  }

  function resetDraft() {
    draft = {
      id: "",
      title: "",
      status: "draft",
      labels: "",
      thread_id: scopedThreadId,
      content: "",
    };
  }

  function toggleCreate() {
    createOpen = !createOpen;
    if (!createOpen) {
      createError = "";
      resetDraft();
    }
  }

  async function handleCreate() {
    if (!draft.title.trim()) {
      createError = "Title is required.";
      return;
    }
    if (!draft.content.trim()) {
      createError = "Content is required.";
      return;
    }

    creating = true;
    createError = "";

    try {
      const labels = draft.labels
        .split(",")
        .map((l) => l.trim())
        .filter(Boolean);

      const docPayload = {
        title: draft.title.trim(),
        status: draft.status,
        labels,
      };
      if (draft.id.trim()) docPayload.id = draft.id.trim();
      if (draft.thread_id.trim()) docPayload.thread_id = draft.thread_id.trim();

      const result = await coreClient.createDocument({
        document: docPayload,
        content: draft.content.trim(),
        content_type: "text",
      });

      const newDocId = result.document?.id;
      createOpen = false;
      resetDraft();

      if (newDocId) {
        await goto(workspaceHref(`/docs/${newDocId}`));
      } else {
        await loadDocuments(scopedThreadId);
      }
    } catch (e) {
      createError = `Failed to create document: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      creating = false;
    }
  }

  function statusColor(status) {
    if (status === "active") return "text-emerald-400 bg-emerald-500/10";
    if (status === "draft") return "text-amber-400 bg-amber-500/10";
    return "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
  }

  function isDocArchived(doc) {
    const at = doc?.archived_at;
    return typeof at === "string" ? at.trim() !== "" : Boolean(at);
  }

  async function archiveDocument(docId) {
    const id = String(docId ?? "").trim();
    if (!id || archiveBusyId) return;
    archiveBusyId = id;
    error = "";
    try {
      await coreClient.archiveDocument(id, {});
      await loadDocuments(scopedThreadId);
    } catch (e) {
      error = `Archive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      archiveBusyId = "";
    }
  }

  async function unarchiveDocument(docId) {
    const id = String(docId ?? "").trim();
    if (!id || archiveBusyId) return;
    archiveBusyId = id;
    error = "";
    try {
      await coreClient.unarchiveDocument(id, {});
      await loadDocuments(scopedThreadId);
    } catch (e) {
      error = `Unarchive failed: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      archiveBusyId = "";
    }
  }

  async function trashDocument(docId) {
    const id = String(docId ?? "").trim();
    if (!id || trashBusyId) return;
    trashBusyId = id;
    error = "";
    try {
      await coreClient.tombstoneDocument(id, {});
      confirmModal = { open: false, action: "", entityId: "" };
      await loadDocuments(scopedThreadId);
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
    if (action === "archive") void archiveDocument(id);
    else if (action === "trash") void trashDocument(id);
  }
</script>

<div class="flex items-center justify-between mb-4">
  <div>
    <h1 class="text-lg font-semibold text-[var(--ui-text)]">Docs</h1>
    <p class="mt-1 text-[12px] text-[var(--ui-text-muted)]">
      Canonical document lineages with a mutable head revision and auditable
      history.
    </p>
    {#if scopedThreadId}
      <p class="mt-1 text-[12px] text-[var(--ui-text-muted)]">
        Scoped to topic
        <a
          class="text-indigo-300 transition-colors hover:text-indigo-200"
          href={workspaceHref(`/topics/${encodeURIComponent(scopedThreadId)}`)}
        >
          {scopedThreadId}
        </a>
      </p>
    {/if}
  </div>
  <div class="flex flex-wrap items-center justify-end gap-2 sm:gap-1.5">
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
      class="cursor-pointer inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-[12px] font-medium transition-colors {groupByLabel
        ? 'bg-[var(--ui-accent-strong)] text-white'
        : 'bg-[var(--ui-panel)] text-[var(--ui-text-muted)] hover:bg-[var(--ui-border)]'}"
      onclick={toggleGrouping}
      type="button"
      title="Group by label"
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
          d="M3 7h4m0 0V3m0 4L3 3m18 4h-4m0 0V3m0 4l4-4M3 17h4m0 0v4m0-4L3 21m18-4h-4m0 0v4m0-4l4 4"
        />
      </svg>
    </button>
    <button
      class="cursor-pointer inline-flex items-center gap-1.5 rounded-md bg-[var(--ui-panel)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text)] transition-colors hover:bg-[var(--ui-border)]"
      onclick={toggleCreate}
      type="button"
    >
      {#if !createOpen}
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
            d="M12 4v16m8-8H4"
          />
        </svg>
      {/if}
      {createOpen ? "Cancel" : "New doc"}
    </button>
  </div>
</div>

{#if scopedThreadId}
  <div
    class="mb-4 flex items-center justify-between rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2"
  >
    <p class="text-[12px] text-[var(--ui-text-muted)]">
      Showing only documents linked to this topic.
    </p>
    <a
      class="text-[12px] font-medium text-indigo-300 transition-colors hover:text-indigo-200"
      href={workspaceHref("/docs")}
    >
      Clear scope
    </a>
  </div>
{/if}

{#if createOpen}
  <div
    class="mb-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] p-4"
  >
    <h2 class="mb-3 text-[13px] font-semibold text-[var(--ui-text)]">
      New doc lineage
    </h2>
    <p class="mb-3 text-[12px] text-[var(--ui-text-muted)]">
      Create the lineage metadata and its first head revision together.
    </p>
    <div class="grid gap-3 sm:grid-cols-2">
      <label class="sm:col-span-2">
        <span class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >Title <span class="text-red-400">*</span></span
        >
        <input
          bind:value={draft.title}
          class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-3 py-1.5 text-[13px] text-[var(--ui-text)] placeholder:text-[var(--ui-text-subtle)]"
          placeholder="Document title"
          type="text"
        />
      </label>
      <label>
        <span class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >ID (optional)</span
        >
        <input
          bind:value={draft.id}
          class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-3 py-1.5 text-[13px] text-[var(--ui-text)] placeholder:text-[var(--ui-text-subtle)]"
          placeholder="auto-generated if empty"
          type="text"
        />
      </label>
      <label>
        <span class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >Status</span
        >
        <select
          bind:value={draft.status}
          class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
        >
          <option value="draft">draft</option>
          <option value="active">active</option>
        </select>
      </label>
      <label>
        <span class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >Labels (comma-separated)</span
        >
        <input
          bind:value={draft.labels}
          class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-3 py-1.5 text-[13px] text-[var(--ui-text)] placeholder:text-[var(--ui-text-subtle)]"
          placeholder="e.g. ops, runbook"
          type="text"
        />
      </label>
      <SearchableEntityPicker
        bind:value={draft.thread_id}
        advancedLabel="Use a manual topic ID"
        helperText="Optional: link the doc lineage to its primary topic."
        label="Topic linkage"
        manualLabel="Topic ID"
        manualPlaceholder="thread-..."
        placeholder="Search topics by title, ID, or tags"
        searchFn={searchThreadOptions}
      />
      <label class="sm:col-span-2">
        <span class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >Head content (Markdown) <span class="text-red-400">*</span></span
        >
        <textarea
          bind:value={draft.content}
          class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-3 py-2 text-[13px] text-[var(--ui-text)] placeholder:text-[var(--ui-text-subtle)] font-mono leading-relaxed resize-y"
          placeholder="# Document title&#10;&#10;Write your content here..."
          rows="10"
        ></textarea>
      </label>
    </div>

    {#if createError}
      <div
        class="mt-3 rounded-md bg-red-500/10 px-3 py-2 text-[12px] text-red-400"
        role="alert"
      >
        {createError}
      </div>
    {/if}
    <div class="mt-3 flex items-center gap-2">
      <button
        class="cursor-pointer rounded-md bg-indigo-600 px-3 py-1.5 text-[12px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
        disabled={creating}
        onclick={handleCreate}
        type="button"
      >
        {creating ? "Creating…" : "Create doc"}
      </button>
      <button
        class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] hover:bg-[var(--ui-border-subtle)]"
        onclick={toggleCreate}
        type="button"
      >
        Cancel
      </button>
    </div>
  </div>
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
    Loading documents...
  </div>
{:else if error}
  <div class="mb-4 rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
    {error}
  </div>
{:else if documents.length === 0}
  <div class="mt-8 text-center">
    <p class="text-[13px] font-medium text-[var(--ui-text-muted)]">
      No docs yet
    </p>
    <p class="mt-1 text-[13px] text-[var(--ui-text-muted)]">
      No doc lineages yet. Create one to start a head revision and revision
      history.
    </p>
  </div>
{/if}

{#snippet docRow(doc, showBorderTop)}
  <div
    class="flex items-stretch transition-colors hover:bg-[var(--ui-border-subtle)] {showBorderTop
      ? 'border-t border-[var(--ui-border)]'
      : ''}"
  >
    <a class="min-w-0 flex-1 px-4 py-3" href={workspaceHref(`/docs/${doc.id}`)}>
      <div class="flex items-start justify-between gap-3">
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-2">
            {#if doc.status}
              <span
                class="inline-flex rounded px-1.5 py-0.5 text-[11px] font-semibold {statusColor(
                  doc.status,
                )}">{DOC_STATUS_LABELS[doc.status] ?? doc.status}</span
              >
            {/if}
            {#if isDocArchived(doc)}
              <span
                class="rounded bg-amber-500/15 px-1.5 py-0.5 text-[11px] font-medium text-amber-400"
                >Archived</span
              >
            {/if}
            {#each (doc.labels ?? []).slice(0, 3) as label}
              <span
                class="rounded bg-[var(--ui-border)] px-1.5 py-0.5 text-[10px] text-[var(--ui-text-muted)]"
                >{label}</span
              >
            {/each}
          </div>
          <p
            class="mt-1 truncate text-[13px] font-medium text-[var(--ui-text)]"
          >
            {doc.title || doc.id}
          </p>
          <p class="text-[11px] text-[var(--ui-text-muted)]">
            Head v{doc.head_revision_number} · Updated {formatTimestamp(
              doc.updated_at,
            ) || "—"} by {actorName(doc.updated_by)}
          </p>
          {#if doc.thread_id && !scopedThreadId}
            <p class="mt-0.5 text-[11px] text-[var(--ui-text-subtle)]">
              Thread (timeline): {doc.thread_id}
            </p>
          {/if}
        </div>
        <span class="shrink-0 text-[11px] text-[var(--ui-text-subtle)]">
          {doc.head_revision_number} revision{doc.head_revision_number === 1
            ? ""
            : "s"}
        </span>
      </div>
    </a>
    <div
      class="flex shrink-0 items-center gap-1 border-l border-[var(--ui-border)] px-2"
      role="presentation"
      onclick={(e) => e.stopPropagation()}
    >
      {#if isDocArchived(doc)}
        <button
          class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-2 py-1 text-[11px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)] disabled:cursor-not-allowed disabled:opacity-50"
          disabled={Boolean(archiveBusyId) || Boolean(trashBusyId)}
          onclick={() => void unarchiveDocument(doc.id)}
          type="button"
        >
          Unarchive
        </button>
      {:else}
        <button
          class="cursor-pointer rounded-md p-1 text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border)] hover:text-[var(--ui-accent)] disabled:cursor-not-allowed disabled:opacity-50"
          disabled={Boolean(archiveBusyId) || Boolean(trashBusyId)}
          onclick={() =>
            (confirmModal = {
              open: true,
              action: "archive",
              entityId: doc.id,
            })}
          title="Archive"
          type="button"
        >
          <svg
            class="h-3.5 w-3.5"
            fill="currentColor"
            viewBox="0 0 24 24"
            aria-hidden="true"
          >
            <path
              d="M20.25 7.5l-.625 10.632a2.25 2.25 0 01-2.247 2.118H6.622a2.25 2.25 0 01-2.247-2.118L3.75 7.5m8.25 3v6.75m0 0l-3-3m3 3l3-3M3.375 7.5h17.25c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125H3.375c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125z"
            />
          </svg>
        </button>
      {/if}
      <button
        class="cursor-pointer rounded-md p-1 text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border)] hover:text-red-400 disabled:cursor-not-allowed disabled:opacity-50"
        disabled={Boolean(trashBusyId) || Boolean(archiveBusyId)}
        onclick={() =>
          (confirmModal = {
            open: true,
            action: "trash",
            entityId: doc.id,
          })}
        title="Move to trash"
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
            d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0"
          />
        </svg>
      </button>
    </div>
  </div>
{/snippet}

{#if !loading && documents.length > 0}
  {#if groupByLabel && groupedDocs}
    <div class="space-y-2">
      {#each groupedDocs as [label, docs]}
        {@const collapsed = collapsedGroups.has(label)}
        {@const displayLabel =
          label === "__ungrouped__"
            ? "Ungrouped"
            : label.charAt(0).toUpperCase() + label.slice(1)}
        <div
          class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] overflow-hidden"
        >
          <button
            class="cursor-pointer flex w-full items-center gap-2 px-4 py-2 text-left transition-colors hover:bg-[var(--ui-border-subtle)]"
            onclick={() => toggleGroup(label)}
            type="button"
          >
            <svg
              class="h-3 w-3 text-[var(--ui-text-subtle)] transition-transform {collapsed
                ? ''
                : 'rotate-90'}"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M9 5l7 7-7 7"
              />
            </svg>
            <span class="text-[12px] font-medium text-[var(--ui-text)]"
              >{displayLabel}</span
            >
            <span
              class="rounded bg-[var(--ui-border)] px-1.5 py-0.5 text-[10px] text-[var(--ui-text-muted)]"
              >{docs.length}</span
            >
          </button>
          {#if !collapsed}
            {#each docs as doc, i}
              {@render docRow(doc, i > 0)}
            {/each}
          {/if}
        </div>
      {/each}
    </div>
  {:else}
    <div
      class="space-y-px rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] overflow-hidden"
    >
      {#each documents as doc, i}
        {@render docRow(doc, i > 0)}
      {/each}
    </div>
  {/if}
{/if}

<ConfirmModal
  open={confirmModal.open}
  title={confirmModal.action === "trash" ? "Move to trash" : "Archive document"}
  message={confirmModal.action === "trash"
    ? "This document will be tombstoned. You can restore it from trash later."
    : "This document will be hidden from default views. You can unarchive it later."}
  confirmLabel={confirmModal.action === "trash" ? "Trash" : "Archive"}
  variant={confirmModal.action === "trash" ? "danger" : "warning"}
  busy={confirmModal.action === "trash"
    ? Boolean(trashBusyId)
    : Boolean(archiveBusyId)}
  onconfirm={handleConfirm}
  oncancel={() => (confirmModal = { open: false, action: "", entityId: "" })}
/>
