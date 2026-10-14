<script>
  import { onMount } from "svelte";
  import { get } from "svelte/store";
  import { durableForm } from "$lib/actions/durableForm.js";
  import {
    enqueueAuthoring,
    mutationOutbox,
    editMutation,
  } from "$lib/mutationOutbox.js";
  import { localWorkScope } from "$lib/localWorkScope.js";
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
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
  let baseUpdatedAt = $state("");
  let target = $derived(`document:${document?.id || documentId}:metadata`);
  let saved = $derived(
    $mutationOutbox.find((entry) => entry.target === target),
  );
  function snapshot() {
    return { summary: summaryDraft, baseUpdatedAt };
  }
  function restore(value, reason = "", entry = null) {
    if (reason === "confirmed" && entry?.result?.document) {
      summaryDraft = String(entry.result.document.summary ?? "");
      baseUpdatedAt = String(entry.result.document.updated_at ?? "");
      return;
    }
    summaryDraft = value?.summary ?? "";
    baseUpdatedAt = value?.baseUpdatedAt ?? "";
  }
  onMount(() => {
    const confirmed = (event) => {
      const entry = event.detail;
      if (entry.scope !== get(localWorkScope) || entry.target !== target)
        return;
      const unchanged =
        JSON.stringify(snapshot()) === JSON.stringify(entry.draft) ||
        (!summaryDraft && !baseUpdatedAt);
      document = entry.result.document;
      headRevision = entry.result.revision ?? headRevision;
      if (unchanged) {
        summaryDraft = String(document.summary ?? "");
        baseUpdatedAt = document.updated_at;
      }
    };
    window.addEventListener("anx:mutation-confirmed", confirmed);
    return () =>
      window.removeEventListener("anx:mutation-confirmed", confirmed);
  });
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

  let loadTicket = 0;
  $effect(() => {
    const ticket = ++loadTicket;
    const scope = get(localWorkScope);
    const id = documentId;
    if (!documentId) return;
    loading = true;
    loadError = "";
    void (async () => {
      try {
        const r = await coreClient.getDocument(id);
        if (
          ticket !== loadTicket ||
          scope !== get(localWorkScope) ||
          id !== documentId
        )
          return;
        const d = r?.document ?? r;
        document = d && typeof d === "object" ? d : null;
        headRevision = r?.revision ?? null;
        if (!document) {
          loadError = "Document not found.";
          return;
        }
        summaryDraft = String(document.summary ?? "");
        baseUpdatedAt = String(document.updated_at ?? "");
      } catch (e) {
        if (ticket !== loadTicket || scope !== get(localWorkScope)) return;
        loadError = `Failed to load document: ${e instanceof Error ? e.message : String(e)}`;
        document = null;
        headRevision = null;
      } finally {
        if (ticket === loadTicket) loading = false;
      }
    })();
  });

  async function useLatestVersion() {
    const scope = get(localWorkScope);
    const id = document.id;
    if (saved && saved.state !== "attention") return;
    try {
      if (saved) await editMutation(saved.key);
      const result = await coreClient.getDocument(id);
      if (scope !== get(localWorkScope) || id !== document?.id) return;
      document = result.document ?? document;
      headRevision = result.revision ?? headRevision;
      baseUpdatedAt = document.updated_at;
    } catch {
      saveError =
        "The latest version could not be loaded. Your description is still here.";
    }
  }
  async function submit() {
    if (!document?.id || !baseUpdatedAt || saved || saving) return;
    saving = true;
    saveError = "";
    try {
      await enqueueAuthoring({
        operation: "docs.patch",
        id: document.id,
        body: {
          patch: { summary: summaryDraft.trim() },
          if_updated_at: baseUpdatedAt,
        },
        target,
        draft: snapshot(),
        href: $page.url.pathname,
        title: "Saved document settings",
      });
    } catch {
      saveError =
        "Your description is still here. Review the saved work above, or copy it before closing this tab.";
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
    <div
      class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text [overflow-wrap:anywhere]"
    >
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
          class="mb-4 rounded-md bg-warn-soft px-3 py-2 text-meta text-warn-text [overflow-wrap:anywhere]"
        >
          {saveError}
        </div>
      {/if}

      <div
        class="rounded-md border border-line bg-panel p-5"
        data-anx-save-scope
        use:durableForm={{
          target,
          value: snapshot(),
          read: snapshot,
          onRestore: restore,
        }}
      >
        <MutationSendStatus {target} />
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
            type="button"
            class="ui-btn-secondary"
            disabled={saving || Boolean(saved && saved.state !== "attention")}
            onclick={useLatestVersion}>Use latest version</button
          >
          <button
            class="rounded-md bg-accent-solid px-4 py-2 text-meta font-medium text-white transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-60"
            disabled={saving || Boolean(saved)}
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
