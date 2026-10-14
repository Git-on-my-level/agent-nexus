<script>
  import { get } from "svelte/store";
  import { onMount } from "svelte";
  import { durableForm } from "$lib/actions/durableForm.js";
  import { localWorkScope } from "$lib/localWorkScope.js";
  import { saveLocalWork, localWorkEpoch } from "$lib/localWorkStore.js";
  import {
    enqueueDocumentLifecycle,
    mutationOutbox,
  } from "$lib/mutationOutbox.js";
  import { coreClient } from "$lib/coreClient.js";
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  let { target, href, onconfirmed = () => {} } = $props();
  let draft = $state(null);
  let error = $state("");
  let preparing = $state(false);
  let pending = $derived(
    $mutationOutbox.find((entry) => entry.target === target),
  );
  function restore(value) {
    draft = value;
    error = "";
  }
  export async function queue(documents, action) {
    if (pending || preparing) {
      error =
        "A document request is already saved. Confirm or edit it before starting another.";
      throw new Error(error);
    }
    if (!documents.length || documents.some((doc) => !doc?.id)) {
      error =
        "Some selected documents are no longer available. Review the selection before sending.";
      throw new Error(error);
    }
    if (
      draft?.documents?.length &&
      JSON.stringify(draft.documents.map((doc) => doc.id)) !==
        JSON.stringify(documents.map((doc) => doc.id))
    ) {
      error = "Send or discard the saved draft before replacing its documents.";
      throw new Error(error);
    }
    if (!draft)
      draft = {
        action,
        reason: "",
        documents: documents.map((doc) => ({
          id: doc.id,
          title: doc.title || doc.id,
          updated_at: doc.updated_at,
        })),
      };
    else if (draft.action !== action) {
      error = "Send or discard the saved draft before changing its action.";
      throw new Error(error);
    }
    await submit();
  }
  async function submit() {
    if (!draft || pending || preparing) return;
    preparing = true;
    error = "";
    const scope = get(localWorkScope);
    const epoch = localWorkEpoch(scope);
    const value = JSON.parse(JSON.stringify(draft));
    try {
      await saveLocalWork(scope, "draft", target, value, { epoch });
      if (scope !== get(localWorkScope) || epoch !== localWorkEpoch(scope))
        return;
      await enqueueDocumentLifecycle({ draft: value, target, href });
    } catch (e) {
      if (scope !== get(localWorkScope) || epoch !== localWorkEpoch(scope))
        return;
      error =
        e.message ||
        "Your request is retained. Copy it before closing this page.";
      throw e;
    } finally {
      preparing = false;
    }
  }
  async function latest() {
    const scope = get(localWorkScope);
    const epoch = localWorkEpoch(scope);
    const value = JSON.stringify(draft);
    try {
      const docs = await Promise.all(
        draft.documents.map(async (doc) => {
          const result = await coreClient.getDocument(doc.id);
          return { ...doc, updated_at: result.document.updated_at };
        }),
      );
      if (
        scope !== get(localWorkScope) ||
        epoch !== localWorkEpoch(scope) ||
        JSON.stringify(draft) !== value
      )
        return;
      draft = { ...draft, documents: docs };
      error = "";
    } catch {
      error =
        "Could not load the latest versions. Your original request is retained.";
    }
  }
  async function copyRequest() {
    try {
      await navigator.clipboard.writeText(JSON.stringify(draft, null, 2));
    } catch {
      error =
        "Select and copy the saved reason and document names before closing this page.";
    }
  }
  async function discard() {
    const scope = get(localWorkScope);
    const epoch = localWorkEpoch(scope);
    try {
      await saveLocalWork(scope, "draft", target, null, { epoch });
      if (scope === get(localWorkScope) && epoch === localWorkEpoch(scope))
        restore(null);
    } catch {
      error =
        "Could not discard the saved draft. It is retained on this device.";
    }
  }
  onMount(() => {
    const confirmed = (event) => {
      if (
        event.detail.scope === get(localWorkScope) &&
        event.detail.operation === "docs.lifecycle" &&
        !(
          event.type === "anx:mutation-progress" &&
          event.detail.receipt?.documents?.length ===
            event.detail.request.items.length
        )
      )
        onconfirmed(event.detail);
    };
    window.addEventListener("anx:mutation-confirmed", confirmed);
    window.addEventListener("anx:mutation-progress", confirmed);
    return () => {
      window.removeEventListener("anx:mutation-confirmed", confirmed);
      window.removeEventListener("anx:mutation-progress", confirmed);
    };
  });
</script>

<form
  use:durableForm={{
    target,
    value: draft,
    read: () => draft,
    onRestore: restore,
  }}
  onsubmit={(event) => {
    event.preventDefault();
    void submit().catch(() => {});
  }}
  onlocal-save-error={() => {
    error =
      "Local saving is unavailable. Your request is still visible; copy it before closing this page.";
  }}
  data-document-lifecycle-work
>
  <MutationSendStatus {target} />
  {#if draft?.documents?.length}
    <div class="mb-3 rounded-md border border-line bg-panel p-3 text-meta">
      <p class="font-medium text-fg">Saved document request</p>
      <p class="mt-1 break-words text-fg-muted">
        {draft.action}: {draft.documents.map((doc) => doc.title).join(", ")}
      </p>
      {#if draft.confirmed?.length}<p class="mt-1 break-words text-fg-muted">
          {draft.confirmed.length} earlier documents already confirmed.
        </p>{/if}
      {#if ["trash", "restore"].includes(draft.action)}
        <label class="mt-2 block text-fg"
          >Reason
          <textarea
            aria-label="Document action reason"
            class="mt-1 w-full rounded border border-line bg-bg-soft p-2 text-fg"
            bind:value={draft.reason}
          ></textarea>
        </label>
      {/if}
      <div class="mt-2 flex flex-wrap gap-2">
        <button
          type="button"
          class="rounded border border-line px-2 py-1 text-fg"
          onclick={() => void copyRequest()}>Copy request</button
        >
        <button
          type="submit"
          class="rounded border border-line px-2 py-1 text-fg"
          disabled={Boolean(pending) || preparing}>Send saved request</button
        >
        <button
          type="button"
          class="rounded border border-line px-2 py-1 text-fg"
          disabled={Boolean(pending) || preparing}
          onclick={() => void latest()}>Use latest version</button
        >
        <button
          type="button"
          class="rounded border border-line px-2 py-1 text-fg"
          disabled={preparing}
          onclick={() => void discard()}>Discard draft</button
        >
      </div>
    </div>
  {/if}
  {#if error}<p class="mb-3 text-meta text-danger-text" role="alert">
      {error}
    </p>{/if}
</form>
