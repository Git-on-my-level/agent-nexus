<script>
  import { page } from "$app/stores";
  import { coreClient } from "$lib/coreClient";
  import { resourceRouteSegment } from "$lib/resourceIdentity.js";
  import { formatShortcut } from "$lib/keyboardHints.js";
  import { workspacePath } from "$lib/workspacePaths";

  let document = $state(/** @type {Record<string, any> | null} */ (null));
  let headRevision = $state(/** @type {Record<string, any> | null} */ (null));
  let loading = $state(true);
  let loadError = $state("");
  let summaryDraft = $state("");
  let saving = $state(false);
  let saveError = $state("");

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let documentId = $derived(String($page.params.documentId ?? "").trim());
  let documentRouteSegment = $derived(
    resourceRouteSegment(document, "document") || documentId,
  );

  function documentDetailHref() {
    return workspacePath(
      organizationSlug,
      workspaceSlug,
      `/docs/${encodeURIComponent(documentRouteSegment)}`,
    );
  }

  function docsHref() {
    return workspacePath(organizationSlug, workspaceSlug, "/docs");
  }

  $effect(() => {
    if (!documentId) return;
    loading = true;
    loadError = "";
    void (async () => {
      try {
        const r = await coreClient.getDocument(documentId);
        const d = r?.document ?? r;
        document = d && typeof d === "object" ? d : null;
        headRevision = r?.revision ?? null;
        if (!document) {
          loadError = "Document not found.";
          return;
        }
        summaryDraft = String(document.summary ?? "");
      } catch (e) {
        loadError = `Failed to load document: ${e instanceof Error ? e.message : String(e)}`;
        document = null;
        headRevision = null;
      } finally {
        loading = false;
      }
    })();
  });

  async function submit() {
    if (!document?.id || !document?.updated_at) return;
    saving = true;
    saveError = "";
    try {
      const result = await coreClient.patchDocument(documentId, {
        patch: { summary: summaryDraft.trim() },
        if_updated_at: document.updated_at,
      });
      document = result.document ?? document;
      headRevision = result.revision ?? headRevision;
      summaryDraft = String(document?.summary ?? "");
    } catch (e) {
      const status = /** @type {{ status?: number }} */ (e)?.status;
      if (status === 409) {
        try {
          const r = await coreClient.getDocument(documentId);
          const d = r?.document ?? r;
          document = d && typeof d === "object" ? d : document;
          headRevision = r?.revision ?? headRevision;
          if (document) {
            summaryDraft = String(document.summary ?? "");
          }
        } catch {
          /* ignore */
        }
        saveError =
          "Document was updated elsewhere. Reloaded the latest values — review and save again.";
      } else {
        saveError = `Save failed: ${e instanceof Error ? e.message : String(e)}`;
      }
    } finally {
      saving = false;
    }
  }
</script>

<div class="mx-auto max-w-lg">
  <div class="mb-6">
    <a
      class="text-micro text-fg-muted transition-colors hover:text-fg"
      href={documentDetailHref()}
    >
      ← Back to document
    </a>
    <h1 class="mt-2 text-subtitle font-semibold text-fg">Document settings</h1>
    <p class="mt-1 text-micro text-fg-muted">
      Update the short description shown on doc lists and in the header. This
      does not create a new revision.
    </p>
    {#if headRevision?.revision_number != null}
      <p class="mt-1 text-micro text-fg-subtle">
        Head revision: v{headRevision.revision_number}
      </p>
    {/if}
  </div>

  {#if loading}
    <p class="text-micro text-fg-muted">Loading…</p>
  {:else if loadError}
    <div class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text">
      {loadError}
    </div>
    <a class="mt-4 inline-block text-micro text-accent-text" href={docsHref()}
      >All docs</a
    >
  {:else if document}
    {#if document.trashed_at}
      <div
        class="mb-4 rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
      >
        This document is in trash. Restore it before editing settings.
      </div>
      <a
        class="inline-block text-micro text-accent-text"
        href={documentDetailHref()}>Back to document</a
      >
    {:else}
      {#if saveError}
        <div
          class="mb-4 rounded-md bg-warn-soft px-3 py-2 text-meta text-warn-text"
        >
          {saveError}
        </div>
      {/if}

      <div
        class="rounded-md border border-line bg-panel p-5"
        data-anx-save-scope
      >
        <label class="block text-meta font-medium text-fg">
          Short description
          <textarea
            bind:value={summaryDraft}
            class="mt-2 w-full resize-y rounded-md border border-line bg-bg-soft px-3 py-2.5 text-meta text-fg focus:border-accent focus:outline-none"
            placeholder="Optional one-line description for lists and the doc header"
            rows="3"
          ></textarea>
        </label>

        <div class="mt-4 flex flex-wrap gap-2">
          <button
            class="rounded-md bg-accent-solid px-4 py-2 text-meta font-medium text-white transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-60"
            disabled={saving}
            onclick={submit}
            type="button"
            data-anx-save-shortcut
            aria-keyshortcuts="Meta+S Control+S"
            title={`Save (${formatShortcut("S")})`}
          >
            {saving ? "Saving…" : "Save"}
          </button>
          <a
            class="rounded-md border border-line bg-panel px-4 py-2 text-meta font-medium text-fg-muted transition-colors hover:bg-line-subtle hover:text-fg"
            href={documentDetailHref()}
          >
            Cancel
          </a>
        </div>
      </div>
    {/if}
  {/if}
</div>
