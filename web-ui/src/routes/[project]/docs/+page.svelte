<script>
  import { page } from "$app/stores";
  import { goto } from "$app/navigation";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { projectPath } from "$lib/projectPaths";
  import { lookupActorDisplayName, actorRegistry } from "$lib/actorSession";

  const DOC_STATUS_LABELS = { draft: "Draft", active: "Active" };

  let documents = $state([]);
  let loading = $state(false);
  let error = $state("");
  let projectSlug = $derived($page.params.project);
  let scopedThreadId = $derived(
    String($page.url.searchParams.get("thread_id") ?? "").trim(),
  );
  let actorName = $derived((id) => lookupActorDisplayName(id, $actorRegistry));

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

  function projectHref(pathname = "/") {
    return projectPath(projectSlug, pathname);
  }

  $effect(() => {
    const threadId = scopedThreadId;
    if (projectSlug) {
      void loadDocuments(threadId);
    }
  });

  async function loadDocuments(threadId = "") {
    loading = true;
    error = "";
    try {
      const filters = {};
      if (threadId) filters.thread_id = threadId;
      const data = await coreClient.listDocuments(filters);
      documents = data.documents ?? [];
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
        await goto(projectHref(`/docs/${newDocId}`));
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
</script>

<div class="flex items-center justify-between mb-4">
  <div>
    <h1 class="text-lg font-semibold text-[var(--ui-text)]">Documents</h1>
    {#if scopedThreadId}
      <p class="mt-1 text-[12px] text-[var(--ui-text-muted)]">
        Scoped to thread
        <a
          class="text-indigo-300 transition-colors hover:text-indigo-200"
          href={projectHref(`/threads/${encodeURIComponent(scopedThreadId)}`)}
        >
          {scopedThreadId}
        </a>
      </p>
    {/if}
  </div>
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
    {createOpen ? "Cancel" : "New document"}
  </button>
</div>

{#if scopedThreadId}
  <div
    class="mb-4 flex items-center justify-between rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2"
  >
    <p class="text-[12px] text-[var(--ui-text-muted)]">
      Showing only documents linked to this thread.
    </p>
    <a
      class="text-[12px] font-medium text-indigo-300 transition-colors hover:text-indigo-200"
      href={projectHref("/docs")}
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
      New document
    </h2>
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
      <label>
        <span class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >Thread ID (optional)</span
        >
        <input
          bind:value={draft.thread_id}
          class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-3 py-1.5 text-[13px] text-[var(--ui-text)] placeholder:text-[var(--ui-text-subtle)]"
          placeholder="thread-..."
          type="text"
        />
      </label>
      <label class="sm:col-span-2">
        <span class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >Content (Markdown) <span class="text-red-400">*</span></span
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
        {creating ? "Creating…" : "Create document"}
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
      No documents yet
    </p>
    <p class="mt-1 text-[13px] text-[var(--ui-text-muted)]">
      Create your first document using the button above.
    </p>
  </div>
{/if}

{#if !loading && documents.length > 0}
  <div
    class="space-y-px rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] overflow-hidden"
  >
    {#each documents as doc, i}
      <a
        class="block px-4 py-3 transition-colors hover:bg-[var(--ui-border-subtle)] {i >
        0
          ? 'border-t border-[var(--ui-border)]'
          : ''}"
        href={projectHref(`/docs/${doc.id}`)}
      >
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
              Updated {formatTimestamp(doc.updated_at) || "—"} by {actorName(
                doc.updated_by,
              )} · v{doc.head_revision_number}
            </p>
          </div>
          <span class="shrink-0 text-[11px] text-[var(--ui-text-subtle)]">
            {doc.head_revision_number} revision{doc.head_revision_number === 1
              ? ""
              : "s"}
          </span>
        </div>
      </a>
    {/each}
  </div>
{/if}
