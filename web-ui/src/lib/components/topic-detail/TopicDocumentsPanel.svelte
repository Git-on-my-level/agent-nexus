<script>
  import { page } from "$app/stores";
  import {
    lookupActorDisplayName,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import { formatTimestamp } from "$lib/formatDate";
  import { workspacePath } from "$lib/workspacePaths";
  import { topicDetailStore } from "$lib/topicDetailStore";

  const DOC_STATUS_LABELS = { draft: "Draft", active: "Active" };

  let { threadId } = $props();

  let documents = $derived($topicDetailStore.documents);
  let documentsLoading = $derived($topicDetailStore.documentsLoading);
  let documentsError = $derived($topicDetailStore.documentsError);
  let workspaceSlug = $derived($page.params.workspace);
  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );

  function workspaceHref(pathname = "/") {
    return workspacePath(workspaceSlug, pathname);
  }

  function docsListHref() {
    return `${workspaceHref("/docs")}?thread_id=${encodeURIComponent(threadId)}`;
  }

  function documentHref(doc) {
    const documentId = String(doc?.id ?? "").trim();
    if (!documentId) {
      return workspaceHref("/docs");
    }
    const revisionId = String(
      doc?.head_revision?.revision_id ?? doc?.head_revision_id ?? "",
    ).trim();
    const base = workspaceHref(`/docs/${encodeURIComponent(documentId)}`);
    if (!revisionId) {
      return base;
    }
    return `${base}?revision=${encodeURIComponent(revisionId)}`;
  }

  function statusTone(status) {
    if (status === "active") return "text-ok-text bg-ok-soft";
    if (status === "draft") return "text-warn-text bg-warn-soft";
    return "text-[var(--fg-muted)] bg-[var(--line)]";
  }
</script>

<section
  class="mt-4 rounded-md border border-[var(--line)] bg-[var(--panel)]"
>
  <div
    class="flex items-center justify-between border-b border-[var(--line-subtle)] px-4 py-2.5"
  >
    <div>
      <h2 class="text-micro font-medium text-[var(--fg-muted)]">Docs</h2>
      <p class="mt-0.5 text-micro text-[var(--fg-muted)]">
        Topic-linked documents and current head revisions.
      </p>
    </div>
    <a
      class="text-micro font-medium text-accent-text transition-colors hover:text-accent-text"
      href={docsListHref()}
    >
      Open scoped docs
    </a>
  </div>

  {#if documentsLoading}
    <p class="px-4 py-3 text-meta text-[var(--fg-muted)]">
      Loading docs...
    </p>
  {:else if documentsError}
    <p class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text">
      {documentsError}
    </p>
  {:else if documents.length === 0}
    <p class="px-4 py-3 text-meta text-[var(--fg-muted)]">
      No documents linked to this topic.
    </p>
  {:else}
    <div class="divide-y divide-[var(--line-subtle)]">
      {#each documents as doc}
        <a
          class="block px-4 py-3 transition-colors hover:bg-[var(--bg-soft)]"
          href={documentHref(doc)}
        >
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2">
                {#if doc.status}
                  <span
                    class={`rounded px-1.5 py-0.5 text-micro font-semibold ${statusTone(doc.status)}`}
                  >
                    {DOC_STATUS_LABELS[doc.status] ?? doc.status}
                  </span>
                {/if}
                <span class="text-micro text-[var(--fg-muted)]">
                  v{doc.head_revision?.revision_number ??
                    doc.head_revision_number ??
                    "?"}
                </span>
                {#if doc.head_revision?.content_type}
                  <span
                    class="rounded bg-[var(--line)] px-1.5 py-0.5 text-micro text-[var(--fg-muted)]"
                  >
                    {doc.head_revision.content_type}
                  </span>
                {/if}
                {#each (doc.labels ?? []).slice(0, 3) as label}
                  <span
                    class="rounded bg-[var(--line)] px-1.5 py-0.5 text-micro text-[var(--fg-muted)]"
                  >
                    {label}
                  </span>
                {/each}
              </div>
              <p
                class="mt-1 truncate text-meta font-medium text-[var(--fg)]"
              >
                {doc.title || doc.id}
              </p>
              <p class="mt-1 text-micro text-[var(--fg-muted)]">
                Updated {formatTimestamp(doc.updated_at) || "—"} by {actorName(
                  doc.updated_by,
                )}
              </p>
            </div>
            <div
              class="shrink-0 text-right text-micro text-[var(--fg-muted)]"
            >
              <div>
                Head revision {doc.head_revision?.revision_number ??
                  doc.head_revision_number ??
                  "?"}
              </div>
              <div>{formatTimestamp(doc.head_revision?.created_at) || "—"}</div>
            </div>
          </div>
        </a>
      {/each}
    </div>
  {/if}
</section>
