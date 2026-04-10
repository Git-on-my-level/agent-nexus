<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { coreClient } from "$lib/coreClient";
  import { workspacePath } from "$lib/workspacePaths";

  let workspaceSlug = $derived($page.params.workspace);
  let revisionId = $derived(String($page.params.revisionId ?? "").trim());

  let loading = $state(false);
  let error = $state("");
  let activeLookupKey = $state("");

  function workspaceHref(pathname = "/") {
    return workspacePath(workspaceSlug, pathname);
  }

  function documentRevisionHref(documentId, targetRevisionId) {
    const baseHref = workspaceHref(
      `/docs/${encodeURIComponent(String(documentId ?? "").trim())}`,
    );
    const search = new URLSearchParams({ revision: targetRevisionId });
    return `${baseHref}?${search.toString()}`;
  }

  $effect(() => {
    if (!workspaceSlug || !revisionId) {
      return;
    }

    const lookupKey = `${workspaceSlug}:${revisionId}`;
    if (lookupKey === activeLookupKey) {
      return;
    }

    activeLookupKey = lookupKey;
    void resolveDocumentRevision(revisionId, lookupKey);
  });

  async function resolveDocumentRevision(targetRevisionId, lookupKey) {
    loading = true;
    error = "";

    try {
      const listResponse = await coreClient.listDocuments({
        include_trashed: true,
      });
      const documents = listResponse.documents ?? [];

      const headMatch = documents.find(
        (document) =>
          String(document?.head_revision_id ?? "").trim() === targetRevisionId,
      );
      if (headMatch?.id) {
        await goto(documentRevisionHref(headMatch.id, targetRevisionId));
        return;
      }

      for (const document of documents) {
        const documentId = String(document?.id ?? "").trim();
        if (!documentId) {
          continue;
        }

        const historyResponse = await coreClient.getDocumentHistory(documentId);
        const revisions = historyResponse.revisions ?? [];
        if (
          revisions.some(
            (revision) =>
              String(revision?.revision_id ?? "").trim() === targetRevisionId,
          )
        ) {
          await goto(documentRevisionHref(documentId, targetRevisionId));
          return;
        }
      }

      error = `Document revision '${targetRevisionId}' was not found.`;
    } catch (e) {
      error = `Failed to resolve document revision: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      if (activeLookupKey === lookupKey) {
        loading = false;
      }
    }
  }
</script>

<div
  class="mx-auto max-w-2xl rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] p-4"
>
  {#if loading}
    <p class="text-[13px] text-[var(--ui-text-muted)]">
      Resolving document revision…
    </p>
  {:else if error}
    <div class="space-y-3">
      <p class="text-[13px] text-red-400">{error}</p>
      <a
        class="inline-flex rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text)] transition-colors hover:bg-[var(--ui-border-subtle)]"
        href={workspaceHref("/docs")}
      >
        Back to Docs
      </a>
    </div>
  {/if}
</div>
