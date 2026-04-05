<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { topicDetailPathFromSubject } from "$lib/topicRouteUtils";
  import { workspacePath } from "$lib/workspacePaths";
  import {
    lookupActorDisplayName,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";

  let documentId = $derived($page.params.documentId);
  let workspaceSlug = $derived($page.params.workspace);
  let requestedRevisionId = $derived(
    String($page.url.searchParams.get("revision") ?? "").trim(),
  );
  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );

  let document = $state(null);
  let headRevision = $state(null);
  let revisions = $state([]);
  let selectedRevision = $state(null);
  let loading = $state(false);
  let historyLoading = $state(false);
  let loadError = $state("");
  let loadedDocumentId = $state("");
  let historyOpen = $state(false);

  let editOpen = $state(false);
  let editDraft = $state({
    content: "",
    title: "",
    status: "",
    labels: "",
  });
  let saving = $state(false);
  let saveError = $state("");
  let loadingSelectedRevisionKey = $state("");
  let metadataExpanded = $state(false);
  let confirmModal = $state({ open: false, action: "" });
  let docLifecycleBusy = $state(false);
  let documentTopicHref = $derived(
    document
      ? topicDetailPathFromSubject({
          subjectRef: document.subject_ref,
          threadId: document.thread_id,
        })
      : "",
  );

  let displayedContent = $derived(
    selectedRevision?.content ?? headRevision?.content ?? "",
  );
  let displayedRevision = $derived(selectedRevision ?? headRevision);
  let isViewingOldRevision = $derived(
    selectedRevision &&
      selectedRevision.revision_id !== headRevision?.revision_id,
  );

  // Only text documents can be edited in the textarea-based editor.
  // Structured and binary revisions must be updated via CLI/API.
  let headContentType = $derived(headRevision?.content_type ?? "text");
  let isTextEditable = $derived(
    headContentType === "text" || headContentType === "",
  );

  function workspaceHref(pathname = "/") {
    return workspacePath(workspaceSlug, pathname);
  }

  async function setRequestedRevision(revisionId = "") {
    const next = String(revisionId ?? "").trim();
    const url = new URL($page.url);

    if (next) {
      url.searchParams.set("revision", next);
    } else {
      url.searchParams.delete("revision");
    }

    const href = `${url.pathname}${url.search}${url.hash}`;
    await goto(href, {
      replaceState: true,
      keepFocus: true,
      noScroll: true,
    });
  }

  $effect(() => {
    const id = documentId;
    if (id && id !== loadedDocumentId) loadDocument(id);
  });

  $effect(() => {
    documentId;
    confirmModal = { open: false, action: "" };
  });

  $effect(() => {
    if (!documentId || !headRevision?.revision_id) {
      return;
    }

    const revisionId = requestedRevisionId;
    if (!revisionId || revisionId === headRevision.revision_id) {
      if (selectedRevision) {
        selectedRevision = null;
      }
      return;
    }

    if (selectedRevision?.revision_id === revisionId) {
      return;
    }

    const cachedRevision = revisions.find(
      (rev) => rev.revision_id === revisionId,
    );
    if (cachedRevision?.content) {
      selectedRevision = cachedRevision;
      return;
    }

    void loadSelectedRevision(documentId, revisionId, cachedRevision ?? null);
  });

  async function loadDocument(targetId) {
    if (!targetId) return;
    loading = true;
    loadError = "";
    loadedDocumentId = targetId;
    revisions = [];
    selectedRevision = null;
    historyLoading = false;
    historyOpen = false;
    editOpen = false;
    try {
      const result = await coreClient.getDocument(targetId);
      document = result.document ?? null;
      headRevision = result.revision ?? null;
      if (!document) {
        loadError = "Document not found.";
      }
    } catch (e) {
      loadError = `Failed to load document: ${e instanceof Error ? e.message : String(e)}`;
      document = null;
      headRevision = null;
    } finally {
      loading = false;
    }
  }

  async function loadHistory() {
    if (!documentId || revisions.length > 0) {
      historyOpen = !historyOpen;
      return;
    }
    historyOpen = true;
    historyLoading = true;
    try {
      const result = await coreClient.getDocumentHistory(documentId);
      revisions = (result.revisions ?? []).slice().reverse();
    } catch {
      revisions = [];
    } finally {
      historyLoading = false;
    }
  }

  async function selectRevision(rev) {
    if (rev.revision_id === headRevision?.revision_id) {
      await setRequestedRevision("");
      return;
    }
    if (rev.content) {
      selectedRevision = rev;
    }
    await setRequestedRevision(rev.revision_id);
  }

  function returnToHead() {
    void setRequestedRevision("");
  }

  async function loadSelectedRevision(
    targetDocumentId,
    targetRevisionId,
    cachedRevision = null,
  ) {
    const requestKey = `${targetDocumentId}:${targetRevisionId}`;
    if (loadingSelectedRevisionKey === requestKey) {
      return;
    }

    loadingSelectedRevisionKey = requestKey;
    try {
      const result = await coreClient.getDocumentRevision(
        targetDocumentId,
        targetRevisionId,
      );
      if (
        documentId !== targetDocumentId ||
        requestedRevisionId !== targetRevisionId
      ) {
        return;
      }

      const loaded = result.revision ?? cachedRevision;
      if (!loaded) {
        selectedRevision = null;
        return;
      }

      selectedRevision = loaded;
      const idx = revisions.findIndex(
        (r) => r.revision_id === targetRevisionId,
      );
      if (idx >= 0) {
        revisions[idx] = { ...revisions[idx], ...loaded };
      } else if (loaded.revision_id) {
        revisions = [...revisions, loaded];
      }
    } catch {
      if (
        documentId === targetDocumentId &&
        requestedRevisionId === targetRevisionId
      ) {
        selectedRevision = cachedRevision;
      }
    } finally {
      if (loadingSelectedRevisionKey === requestKey) {
        loadingSelectedRevisionKey = "";
      }
    }
  }

  function openEdit() {
    editDraft = {
      content: headRevision?.content ?? "",
      title: document?.title ?? "",
      status: document?.status ?? "",
      labels: (document?.labels ?? []).join(", "),
    };
    saveError = "";
    editOpen = true;
    historyOpen = false;
  }

  function closeEdit() {
    editOpen = false;
    saveError = "";
  }

  async function handleSave() {
    if (!editDraft.content.trim()) {
      saveError = "Content is required.";
      return;
    }

    if (!headRevision?.revision_id) {
      saveError = "Cannot determine base revision. Please reload.";
      return;
    }

    saving = true;
    saveError = "";

    try {
      const labels = editDraft.labels
        .split(",")
        .map((l) => l.trim())
        .filter(Boolean);

      const docPatch = {};
      if (
        editDraft.title.trim() &&
        editDraft.title.trim() !== document?.title
      ) {
        docPatch.title = editDraft.title.trim();
      }
      if (editDraft.status && editDraft.status !== document?.status) {
        docPatch.status = editDraft.status;
      }
      const labelsChanged =
        JSON.stringify(labels) !== JSON.stringify(document?.labels ?? []);
      if (labelsChanged) {
        docPatch.labels = labels;
      }
      const result = await coreClient.updateDocument(documentId, {
        content: editDraft.content.trim(),
        content_type: headContentType || "text",
        if_base_revision: headRevision.revision_id,
        ...(Object.keys(docPatch).length > 0 ? { document: docPatch } : {}),
      });

      document = result.document ?? document;
      headRevision = result.revision ?? headRevision;
      selectedRevision = null;
      revisions = [];
      editOpen = false;
    } catch (e) {
      saveError = `Failed to save revision: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      saving = false;
    }
  }

  async function handleArchiveDocument() {
    if (!documentId || docLifecycleBusy || document?.trashed_at) return;
    docLifecycleBusy = true;
    try {
      await coreClient.archiveDocument(documentId, {});
      await loadDocument(documentId);
    } finally {
      docLifecycleBusy = false;
    }
  }

  async function handleUnarchiveDocument() {
    confirmModal = { open: false, action: "" };
    if (!documentId || docLifecycleBusy || document?.trashed_at) return;
    docLifecycleBusy = true;
    try {
      await coreClient.unarchiveDocument(documentId, {});
      await loadDocument(documentId);
    } finally {
      docLifecycleBusy = false;
    }
  }

  function handleConfirm() {
    const action = confirmModal.action;
    confirmModal = { open: false, action: "" };
    if (action === "archive") handleArchiveDocument();
    else if (action === "trash") handleTrashDocument();
  }

  async function handleTrashDocument() {
    if (!documentId || docLifecycleBusy) return;
    docLifecycleBusy = true;
    try {
      await coreClient.trashDocument(documentId, {});
      await goto(workspacePath(workspaceSlug, "/docs"));
    } finally {
      docLifecycleBusy = false;
    }
  }

  async function handleRestoreDocument() {
    confirmModal = { open: false, action: "" };
    if (!documentId || docLifecycleBusy) return;
    docLifecycleBusy = true;
    try {
      await coreClient.restoreDocument(documentId, {});
      await loadDocument(documentId);
    } finally {
      docLifecycleBusy = false;
    }
  }
</script>

<nav
  class="mb-3 flex items-center gap-1.5 text-[12px] text-[var(--ui-text-muted)]"
  aria-label="Breadcrumb"
>
  <a
    class="transition-colors hover:text-[var(--ui-text)]"
    href={workspaceHref("/docs")}>Docs</a
  >
  <span class="text-[var(--ui-text-subtle)]">/</span>
  <span class="truncate text-[var(--ui-text-muted)]"
    >{document?.title || documentId}</span
  >
</nav>

{#if loading}
  <div
    class="mt-8 flex items-center justify-center gap-2 text-[13px] text-[var(--ui-text-muted)]"
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
    Loading...
  </div>
{:else if loadError}
  <div class="rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
    {loadError}
  </div>
{:else if document}
  {#if document.trashed_at}
    <div
      class="mb-4 flex flex-wrap items-start justify-between gap-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-[13px] text-red-400"
    >
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2 font-semibold">
          <span>⚠</span>
          <span>This document is in trash</span>
        </div>
        {#if document.trash_reason}
          <p class="mt-2">Reason: {document.trash_reason}</p>
        {/if}
        <p class="mt-1 text-[11px] text-red-400/80">
          Trashed {#if document.trashed_by}by {actorName(
              document.trashed_by,
            )}{/if}
          {#if document.trashed_at}
            at {formatTimestamp(document.trashed_at)}
          {/if}
        </p>
      </div>
      <button
        class="shrink-0 cursor-pointer rounded-md border border-red-500/40 bg-red-500/15 px-2 py-1 text-[12px] font-medium text-red-400 hover:bg-red-500/25 disabled:opacity-50"
        disabled={docLifecycleBusy}
        onclick={handleRestoreDocument}
        type="button"
      >
        {docLifecycleBusy ? "…" : "Restore"}
      </button>
    </div>
  {:else if document.archived_at}
    <div
      class="mb-4 flex flex-wrap items-start justify-between gap-3 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-[13px] text-amber-400"
    >
      <p class="min-w-0 flex-1">
        This document was archived on {formatTimestamp(document.archived_at) ||
          "—"}{#if document.archived_by}
          by {actorName(document.archived_by)}{/if}.
      </p>
      <button
        class="shrink-0 cursor-pointer rounded-md border border-amber-500/40 bg-amber-500/15 px-2 py-1 text-[12px] font-medium text-amber-400 hover:bg-amber-500/25 disabled:opacity-50"
        disabled={docLifecycleBusy}
        onclick={handleUnarchiveDocument}
        type="button"
      >
        {docLifecycleBusy ? "…" : "Unarchive"}
      </button>
    </div>
  {/if}

  <div class="flex gap-4">
    <div class="min-w-0 flex-1">
      <div class="flex items-start justify-between gap-3">
        <div class="min-w-0 flex-1">
          <h1 class="text-lg font-semibold text-[var(--ui-text)]">
            {document.title || ""}{#if !document.title}<span
                class="font-mono text-[var(--ui-text-subtle)]"
                >{document.id}</span
              >{/if}
          </h1>
          <div class="mt-1 flex flex-wrap items-center gap-1.5 text-[12px]">
            {#if document.status}
              <span
                class="rounded px-1.5 py-0.5 font-medium {document.status ===
                'active'
                  ? 'text-emerald-400 bg-emerald-500/10'
                  : 'text-amber-400 bg-amber-500/10'}"
                >{{ draft: "Draft", active: "Active" }[document.status] ??
                  document.status}</span
              >
            {/if}
            {#each document.labels ?? [] as label}
              <span
                class="rounded bg-[var(--ui-border)] px-1.5 py-0.5 text-[10px] text-[var(--ui-text-muted)]"
                >{label}</span
              >
            {/each}
            <span class="text-[var(--ui-text-subtle)]">·</span>
            <span class="text-[var(--ui-text-muted)]"
              >v{displayedRevision?.revision_number ?? "\u2014"}</span
            >
            <span class="text-[var(--ui-text-subtle)]">·</span>
            <span class="text-[var(--ui-text-muted)]"
              >{formatTimestamp(displayedRevision?.created_at) || "—"}</span
            >
            <span class="text-[var(--ui-text-subtle)]">·</span>
            <span class="text-[var(--ui-text-muted)]"
              >by {actorName(displayedRevision?.created_by)}</span
            >
          </div>
          {#if document.thread_id && documentTopicHref}
            <p class="mt-0.5 text-[12px] text-[var(--ui-text-muted)]">
              Thread (timeline):
              <a
                class="text-indigo-400 transition-colors hover:text-indigo-300"
                href={workspaceHref(documentTopicHref)}
                >{String(document.subject_ref ?? "")
                  .replace(/^topic:/, "")
                  .trim() || document.thread_id}</a
              >
            </p>
          {/if}
        </div>
        {#if !document.trashed_at}
          <div class="flex shrink-0 items-center gap-1.5">
            {#if isTextEditable}
              <button
                class="cursor-pointer inline-flex items-center gap-1.5 rounded-md bg-indigo-600 px-2.5 py-1.5 text-[12px] font-medium text-white transition-colors hover:bg-indigo-500"
                onclick={openEdit}
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
                    d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"
                  />
                </svg>
                New revision
              </button>
            {:else}
              <span
                class="inline-flex items-center gap-1 rounded-md border border-[var(--ui-border)] px-2.5 py-1.5 text-[12px] text-[var(--ui-text-subtle)]"
                title="Content type '{headContentType}' can only be updated via the CLI or API"
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
                    d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                  />
                </svg>
                {headContentType} — edit via CLI
              </span>
            {/if}
            {#if !document.archived_at}
              <button
                aria-label="Archive"
                class="cursor-pointer rounded-md p-1.5 text-[var(--ui-text-muted)] hover:bg-[var(--ui-border)] hover:text-[var(--ui-accent)] disabled:opacity-50"
                disabled={docLifecycleBusy}
                onclick={() =>
                  (confirmModal = { open: true, action: "archive" })}
                type="button"
              >
                <svg
                  class="h-4 w-4"
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
              class="cursor-pointer shrink-0 inline-flex items-center gap-1.5 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-2.5 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)]"
              onclick={loadHistory}
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
                  d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
                />
              </svg>
              Revision history
            </button>
            <button
              aria-label="Move document to trash"
              class="cursor-pointer rounded-md p-1.5 text-[var(--ui-text-muted)] hover:bg-[var(--ui-border)] hover:text-red-400 disabled:opacity-50"
              disabled={docLifecycleBusy}
              onclick={() => (confirmModal = { open: true, action: "trash" })}
              type="button"
            >
              <svg
                class="h-4 w-4"
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
        {/if}
      </div>

      {#if editOpen}
        <div
          class="mt-3 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] p-4"
        >
          <div class="mb-3">
            <button
              class="cursor-pointer flex w-full items-center gap-2 text-left"
              onclick={() => (metadataExpanded = !metadataExpanded)}
              type="button"
            >
              <svg
                class="h-3 w-3 text-[var(--ui-text-subtle)] transition-transform {metadataExpanded
                  ? 'rotate-90'
                  : ''}"
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
              <span class="text-[12px] font-medium text-[var(--ui-text-muted)]"
                >Metadata</span
              >
            </button>
            {#if !metadataExpanded}
              <p
                class="mt-1 ml-5 truncate text-[11px] text-[var(--ui-text-subtle)]"
              >
                Title: {editDraft.title || "—"} · Status: {editDraft.status ||
                  "—"} · Labels: {editDraft.labels || "none"}
              </p>
            {/if}
            {#if metadataExpanded}
              <div class="mt-2 ml-5 grid gap-3 sm:grid-cols-2">
                <label class="sm:col-span-2">
                  <span
                    class="text-[12px] font-medium text-[var(--ui-text-muted)]"
                    >Title</span
                  >
                  <input
                    bind:value={editDraft.title}
                    class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-3 py-1.5 text-[13px] text-[var(--ui-text)]"
                    type="text"
                  />
                </label>
                <label>
                  <span
                    class="text-[12px] font-medium text-[var(--ui-text-muted)]"
                    >Status</span
                  >
                  <select
                    bind:value={editDraft.status}
                    class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
                  >
                    <option value="draft">draft</option>
                    <option value="active">active</option>
                  </select>
                </label>
                <label>
                  <span
                    class="text-[12px] font-medium text-[var(--ui-text-muted)]"
                    >Labels (comma-separated)</span
                  >
                  <input
                    bind:value={editDraft.labels}
                    class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-3 py-1.5 text-[13px] text-[var(--ui-text)] placeholder:text-[var(--ui-text-subtle)]"
                    placeholder="ops, runbook"
                    type="text"
                  />
                </label>
              </div>
            {/if}
          </div>

          <label>
            <span class="text-[12px] font-medium text-[var(--ui-text-muted)]"
              >Content (Markdown) <span class="text-red-400">*</span></span
            >
            <textarea
              bind:value={editDraft.content}
              class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-3 py-2 text-[13px] text-[var(--ui-text)] font-mono leading-relaxed resize-y"
              rows="20"
            ></textarea>
          </label>

          <div class="mt-3 flex items-center gap-2">
            <button
              class="cursor-pointer rounded-md bg-indigo-600 px-3 py-1.5 text-[12px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
              disabled={saving}
              onclick={handleSave}
              type="button"
            >
              {saving ? "Saving…" : "Save revision"}
            </button>
            <button
              class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] hover:bg-[var(--ui-border-subtle)]"
              onclick={closeEdit}
              type="button"
            >
              Cancel
            </button>
          </div>

          {#if saveError}
            <div
              class="mt-3 rounded-md bg-red-500/10 px-3 py-2 text-[12px] text-red-400"
              role="alert"
            >
              {saveError}
            </div>
          {/if}
          <p class="mt-2 text-[11px] text-[var(--ui-text-subtle)]">
            Base revision: <span class="font-mono"
              >{headRevision?.revision_id ?? "—"}</span
            > — optimistic concurrency is enforced.
          </p>
        </div>
      {/if}

      {#if isViewingOldRevision}
        <div
          class="mt-3 flex items-center gap-2 rounded-md bg-amber-500/10 px-3 py-2 text-[12px] text-amber-400"
        >
          <span
            >Viewing revision {selectedRevision.revision_number} from {formatTimestamp(
              selectedRevision.created_at,
            )}</span
          >
          <button
            class="cursor-pointer ml-auto font-medium underline"
            onclick={returnToHead}
            type="button">Return to current</button
          >
        </div>
      {/if}

      <div
        class="mt-3 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)]"
      >
        <div class="px-4 py-3">
          {#if displayedContent}
            <MarkdownRenderer
              source={displayedContent}
              class="text-[13px] leading-relaxed text-[var(--ui-text)]"
            />
          {:else}
            <p class="text-[13px] text-[var(--ui-text-muted)]">(No content)</p>
          {/if}
        </div>
      </div>

      <div class="mt-6 border-t border-[var(--ui-border)] pt-4">
        <p
          class="mb-2 text-[11px] font-medium uppercase tracking-[0.12em] text-[var(--ui-text-subtle)]"
        >
          Technical details
        </p>

        {#if displayedRevision?.content_hash || displayedRevision?.revision_hash}
          <details
            class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)]"
          >
            <summary
              class="cursor-pointer px-4 py-2.5 text-[11px] text-[var(--ui-text-muted)] hover:text-[var(--ui-text)]"
              >Integrity hashes</summary
            >
            <div class="px-4 pb-3 pt-1 space-y-2">
              {#if displayedRevision.content_hash}
                <div>
                  <p
                    class="text-[11px] uppercase tracking-[0.12em] text-[var(--ui-text-subtle)]"
                  >
                    Content hash
                  </p>
                  <p
                    class="mt-1 break-all font-mono text-[12px] text-[var(--ui-text-muted)]"
                  >
                    {displayedRevision.content_hash}
                  </p>
                </div>
              {/if}
              {#if displayedRevision.revision_hash}
                <div>
                  <p
                    class="text-[11px] uppercase tracking-[0.12em] text-[var(--ui-text-subtle)]"
                  >
                    Revision hash
                  </p>
                  <p
                    class="mt-1 break-all font-mono text-[12px] text-[var(--ui-text-muted)]"
                  >
                    {displayedRevision.revision_hash}
                  </p>
                </div>
              {/if}
            </div>
          </details>
        {/if}

        <details
          class="mt-2 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)]"
        >
          <summary
            class="cursor-pointer px-4 py-2.5 text-[11px] text-[var(--ui-text-muted)] hover:text-[var(--ui-text)]"
            >Raw metadata JSON</summary
          >
          <pre
            class="overflow-auto px-4 pb-3 text-[11px] text-[var(--ui-text-muted)]">{JSON.stringify(
              document,
              null,
              2,
            )}</pre>
        </details>
      </div>
    </div>

    {#if historyOpen}
      <aside class="w-72 shrink-0">
        <div
          class="sticky top-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)]"
        >
          <div
            class="flex items-center justify-between border-b border-[var(--ui-border)] px-4 py-2.5"
          >
            <h2 class="text-[13px] font-medium text-[var(--ui-text)]">
              Revision history
            </h2>
            <button
              class="cursor-pointer text-[var(--ui-text-muted)] hover:text-[var(--ui-text)]"
              onclick={() => (historyOpen = false)}
              type="button"
              aria-label="Close history"
            >
              <svg
                class="h-4 w-4"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                stroke-width="2"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  d="M6 18L18 6M6 6l12 12"
                />
              </svg>
            </button>
          </div>

          {#if historyLoading}
            <div
              class="flex items-center gap-2 px-4 py-4 text-[12px] text-[var(--ui-text-muted)]"
            >
              <svg
                class="h-3.5 w-3.5 animate-spin"
                fill="none"
                viewBox="0 0 24 24"
              >
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
              Loading revision history...
            </div>
          {:else if revisions.length === 0}
            <p class="px-4 py-4 text-[12px] text-[var(--ui-text-muted)]">
              No earlier revisions found.
            </p>
          {:else}
            <div class="max-h-[calc(100vh-12rem)] overflow-y-auto">
              {#each revisions as rev, i}
                {@const isHead = rev.revision_id === headRevision?.revision_id}
                {@const isSelected =
                  displayedRevision?.revision_id === rev.revision_id}
                <button
                  class="w-full text-left px-4 py-3 transition-colors hover:bg-[var(--ui-border-subtle)] {i >
                  0
                    ? 'border-t border-[var(--ui-border)]'
                    : ''} {isSelected ? 'bg-[var(--ui-border-subtle)]' : ''}"
                  onclick={() => selectRevision(rev)}
                  type="button"
                >
                  <div class="flex items-center gap-2">
                    <div class="relative flex flex-col items-center">
                      <div
                        class="h-2.5 w-2.5 rounded-full {isHead
                          ? 'bg-emerald-400'
                          : isSelected
                            ? 'bg-indigo-400'
                            : 'bg-[var(--ui-text-subtle)]'}"
                      ></div>
                      {#if i < revisions.length - 1}
                        <div
                          class="absolute top-3 h-full w-px bg-[var(--ui-border)]"
                        ></div>
                      {/if}
                    </div>
                    <div class="min-w-0 flex-1">
                      <p class="text-[12px] font-medium text-[var(--ui-text)]">
                        {#if isHead}Current version{:else}Version {rev.revision_number}{/if}
                      </p>
                      <p class="text-[11px] text-[var(--ui-text-muted)]">
                        {formatTimestamp(rev.created_at)} · {actorName(
                          rev.created_by,
                        )}
                      </p>
                      {#if rev.revision_hash}
                        <p
                          class="mt-0.5 font-mono text-[10px] text-[var(--ui-text-subtle)]"
                        >
                          {rev.revision_hash.slice(0, 12)}...
                        </p>
                      {/if}
                    </div>
                  </div>
                </button>
              {/each}
            </div>
          {/if}
        </div>
      </aside>
    {/if}
  </div>
{:else}
  <div class="mt-8 text-center text-[13px] text-[var(--ui-text-muted)]">
    Document not found.
  </div>
{/if}

<ConfirmModal
  open={confirmModal.open}
  title={confirmModal.action === "trash" ? "Move to trash" : "Archive document"}
  message={confirmModal.action === "trash"
    ? "This document will be moved to trash. You can restore it later."
    : "This document will be hidden from default views. You can unarchive it later."}
  confirmLabel={confirmModal.action === "trash" ? "Trash" : "Archive"}
  variant={confirmModal.action === "trash" ? "danger" : "warning"}
  busy={docLifecycleBusy}
  onconfirm={handleConfirm}
  oncancel={() => (confirmModal = { open: false, action: "" })}
/>
