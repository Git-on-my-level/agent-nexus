<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { coreClient } from "$lib/coreClient";
  import {
    documentRevisionPath,
    isNotFoundFailure,
    isSafeRefValue,
  } from "$lib/refRoute.js";
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
      /*
       * One bounded batch-resolve read finds the revision's document by
       * revision id or `<document>-r<N>` handle. Listing every document and
       * walking their histories made one link cost O(workspace) requests.
       */
      const ref = targetRevisionId.startsWith("document_revision:")
        ? targetRevisionId
        : `document_revision:${targetRevisionId}`;
      const value = ref.slice("document_revision:".length);
      const path = isSafeRefValue(value)
        ? await documentRevisionPath(coreClient, ref, value)
        : "";
      if (activeLookupKey !== lookupKey) return;
      if (path) {
        await goto(workspaceHref(path), { replaceState: true });
        return;
      }
      error = `Document version '${targetRevisionId}' was not found.`;
    } catch (e) {
      if (activeLookupKey !== lookupKey) return;
      error = isNotFoundFailure(e)
        ? `Document version '${targetRevisionId}' was not found.`
        : `Failed to resolve document version: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      if (activeLookupKey === lookupKey) {
        loading = false;
      }
    }
  }
</script>

<div class="mx-auto max-w-2xl rounded-md border border-line bg-panel p-4">
  {#if loading}
    <p class="text-meta text-fg-muted">Resolving document version…</p>
  {:else if error}
    <div class="space-y-3">
      <p class="text-meta text-danger-text [overflow-wrap:anywhere]">{error}</p>
      <a
        class="inline-flex rounded-md border border-line bg-bg-soft px-3 py-1.5 text-micro font-medium text-fg transition-colors hover:bg-line-subtle"
        href={workspaceHref("/docs")}
      >
        Back to Docs
      </a>
    </div>
  {/if}
</div>
