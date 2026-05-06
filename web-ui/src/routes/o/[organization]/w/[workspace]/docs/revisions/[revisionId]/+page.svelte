<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { coreClient } from "$lib/coreClient";
  import {
    resourceRouteSegment,
    revisionRouteSegment,
  } from "$lib/resourceIdentity.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let revisionId = $derived(String($page.params.revisionId ?? "").trim());

  let loading = $state(false);
  let error = $state("");
  let activeLookupKey = $state("");

  let workspaceHref = $derived(
    bindWorkspaceHref(organizationSlug, workspaceSlug),
  );

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
        state: ["active", "archived", "trashed"],
      });
      const documents = listResponse.documents ?? [];

      const headMatch = documents.find(
        (document) =>
          String(document?.head_revision_id ?? "").trim() ===
            targetRevisionId ||
          String(document?.head_revision_ref ?? "").trim() === targetRevisionId,
      );
      if (headMatch?.id) {
        await goto(
          documentRevisionHref(
            resourceRouteSegment(headMatch, "document"),
            targetRevisionId,
          ),
        );
        return;
      }

      for (const document of documents) {
        const documentId = resourceRouteSegment(document, "document");
        if (!documentId) {
          continue;
        }

        const historyResponse = await coreClient.getDocumentHistory(documentId);
        const revisions = historyResponse.revisions ?? [];
        const revisionMatch = revisions.find(
          (revision) =>
            String(revision?.revision_id ?? "").trim() === targetRevisionId ||
            String(revision?.ref ?? "").trim() === targetRevisionId ||
            String(revision?.handle ?? "").trim() === targetRevisionId,
        );
        if (revisionMatch) {
          await goto(
            documentRevisionHref(
              documentId,
              revisionRouteSegment(revisionMatch, "document_revision") ||
                targetRevisionId,
            ),
          );
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

<div class="mx-auto max-w-2xl rounded-md border border-line bg-panel p-4">
  {#if loading}
    <p class="text-meta text-fg-muted">Resolving document revision…</p>
  {:else if error}
    <div class="space-y-3">
      <p class="text-meta text-danger-text">{error}</p>
      <a
        class="inline-flex rounded-md border border-line bg-bg-soft px-3 py-1.5 text-micro font-medium text-fg transition-colors hover:bg-line-subtle"
        href={workspaceHref("/docs")}
      >
        Back to Docs
      </a>
    </div>
  {/if}
</div>
