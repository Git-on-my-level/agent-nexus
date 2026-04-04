<script>
  import { page } from "$app/stores";
  import {
    lookupActorDisplayName,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import { formatTimestamp } from "$lib/formatDate";
  import { workspacePath } from "$lib/workspacePaths";
  import { threadDetailStore } from "$lib/threadDetailStore";

  const DOC_STATUS_LABELS = { draft: "Draft", active: "Active" };

  let { threadId } = $props();

  let documents = $derived($threadDetailStore.documents);
  let documentsLoading = $derived($threadDetailStore.documentsLoading);
  let documentsError = $derived($threadDetailStore.documentsError);
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
    if (status === "active") return "text-emerald-300 bg-emerald-500/10";
    if (status === "draft") return "text-amber-300 bg-amber-500/10";
    return "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
  }
</script>

<section
  class="mt-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]"
>
  <div
    class="flex items-center justify-between border-b border-[var(--ui-border-subtle)] px-4 py-2.5"
  >
    <div>
      <h2
        class="text-[12px] font-semibold uppercase tracking-wider text-[var(--ui-text-muted)]"
      >
        Documents
      </h2>
      <p class="mt-0.5 text-[12px] text-[var(--ui-text-subtle)]">
        Topic-linked docs and current head revisions.
      </p>
    </div>
    <a
      class="text-[12px] font-medium text-indigo-300 transition-colors hover:text-indigo-200"
      href={docsListHref()}
    >
      Open scoped docs
    </a>
  </div>

  {#if documentsLoading}
    <p class="px-4 py-3 text-[13px] text-[var(--ui-text-muted)]">
      Loading documents...
    </p>
  {:else if documentsError}
    <p class="px-4 py-3 text-[13px] text-red-400">{documentsError}</p>
  {:else if documents.length === 0}
    <p class="px-4 py-3 text-[13px] text-[var(--ui-text-muted)]">
      No documents linked to this topic.
    </p>
  {:else}
    <div class="divide-y divide-[var(--ui-border-subtle)]">
      {#each documents as doc}
        <a
          class="block px-4 py-3 transition-colors hover:bg-[var(--ui-bg-soft)]"
          href={documentHref(doc)}
        >
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2">
                {#if doc.status}
                  <span
                    class={`rounded px-1.5 py-0.5 text-[11px] font-semibold ${statusTone(doc.status)}`}
                  >
                    {DOC_STATUS_LABELS[doc.status] ?? doc.status}
                  </span>
                {/if}
                <span class="text-[11px] text-[var(--ui-text-subtle)]">
                  v{doc.head_revision?.revision_number ??
                    doc.head_revision_number ??
                    "?"}
                </span>
                {#if doc.head_revision?.content_type}
                  <span
                    class="rounded bg-[var(--ui-border)] px-1.5 py-0.5 text-[10px] text-[var(--ui-text-muted)]"
                  >
                    {doc.head_revision.content_type}
                  </span>
                {/if}
                {#each (doc.labels ?? []).slice(0, 3) as label}
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
                {doc.title || doc.id}
              </p>
              <p class="mt-1 text-[11px] text-[var(--ui-text-muted)]">
                Updated {formatTimestamp(doc.updated_at) || "—"} by {actorName(
                  doc.updated_by,
                )}
              </p>
            </div>
            <div
              class="shrink-0 text-right text-[11px] text-[var(--ui-text-subtle)]"
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
