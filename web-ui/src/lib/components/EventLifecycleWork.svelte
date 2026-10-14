<script>
  import { get } from "svelte/store";
  import { onMount } from "svelte";
  import { durableForm } from "$lib/actions/durableForm.js";
  import { localWorkScope } from "$lib/localWorkScope.js";
  import { saveLocalWork, localWorkEpoch } from "$lib/localWorkStore.js";
  import {
    enqueueEventLifecycle,
    mutationOutbox,
  } from "$lib/mutationOutbox.js";
  import { coreClient } from "$lib/coreClient.js";
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  let { target, href, onconfirmed = () => {} } = $props();
  let draft = $state(null);
  let error = $state("");
  let preparing = $state(false);
  let ready = $state(false);
  let readyWaiters = [];
  let pending = $derived(
    $mutationOutbox.find((entry) => entry.target === target),
  );
  function restore(value) {
    draft = value;
    error = "";
  }
  function markReady(value) {
    ready = value;
    if (!ready) return;
    const waiters = readyWaiters;
    readyWaiters = [];
    waiters.forEach((resolve) => resolve());
  }
  function whenReady() {
    if (ready) return Promise.resolve();
    return new Promise((resolve) => readyWaiters.push(resolve));
  }
  export async function queue(events, action) {
    while (!ready) await whenReady();
    if (pending || preparing) {
      error =
        "An event request is already saved. Confirm or edit it before starting another.";
      throw new Error(error);
    }
    if (!events.length || events.some((doc) => !doc?.id)) {
      error =
        "Some selected events are no longer available. Review the selection before sending.";
      throw new Error(error);
    }
    // Send selected ancestors first so their atomic receipts can cover replies.
    const selected = new Map();
    for (const event of events) {
      for (const key of [
        event.id,
        event.handle,
        String(event.ref || "").replace(/^event:/, ""),
      ]) {
        if (key) selected.set(key, event);
      }
    }
    const visited = new Set();
    const ordered = [];
    const visit = (event) => {
      if (visited.has(event.id)) return;
      visited.add(event.id);
      for (const ref of event.refs || []) {
        const parent = selected.get(String(ref).replace(/^event:/, ""));
        if (String(ref).startsWith("event:") && parent) visit(parent);
      }
      ordered.push(event);
    };
    events.forEach(visit);
    events = ordered;
    if (
      draft?.events?.length &&
      JSON.stringify(draft.events.map((doc) => doc.id)) !==
        JSON.stringify(events.map((doc) => doc.id))
    ) {
      error = "Send or discard the saved draft before replacing its events.";
      throw new Error(error);
    }
    if (!draft)
      draft = {
        action,
        reason: "",
        events: events.map((doc) => ({
          id: doc.id,
          title: doc.summary || doc.type || doc.id,
          if_lifecycle: {
            archived_at: doc.archived_at ?? null,
            trashed_at: doc.trashed_at ?? null,
          },
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
    const savedTarget = target;
    const savedHref = href;
    try {
      await saveLocalWork(scope, "draft", savedTarget, value, { epoch });
      if (scope !== get(localWorkScope) || epoch !== localWorkEpoch(scope))
        return;
      await enqueueEventLifecycle({
        draft: value,
        target: savedTarget,
        href: savedHref,
      });
    } catch (e) {
      if (
        target !== savedTarget ||
        scope !== get(localWorkScope) ||
        epoch !== localWorkEpoch(scope)
      )
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
    const savedTarget = target;
    try {
      const docs = await Promise.all(
        draft.events.map(async (doc) => {
          const result = await coreClient.getEvent(doc.id);
          return {
            ...doc,
            if_lifecycle: {
              archived_at: result.event.archived_at ?? null,
              trashed_at: result.event.trashed_at ?? null,
            },
          };
        }),
      );
      if (
        scope !== get(localWorkScope) ||
        epoch !== localWorkEpoch(scope) ||
        JSON.stringify(draft) !== value ||
        target !== savedTarget
      )
        return;
      draft = { ...draft, events: docs };
      error = "";
    } catch {
      if (
        scope !== get(localWorkScope) ||
        epoch !== localWorkEpoch(scope) ||
        JSON.stringify(draft) !== value ||
        target !== savedTarget
      )
        return;
      error =
        "Could not load the latest versions. Your original request is retained.";
    }
  }
  async function copyRequest() {
    try {
      await navigator.clipboard.writeText(JSON.stringify(draft, null, 2));
    } catch {
      error =
        "Select and copy the saved reason and event names before closing this page.";
    }
  }
  async function discard() {
    if (pending || preparing) return;
    const scope = get(localWorkScope);
    const epoch = localWorkEpoch(scope);
    const savedTarget = target;
    try {
      const saved = draft;
      const saving = saveLocalWork(scope, "draft", target, null, { epoch });
      restore(null);
      try {
        await saving;
      } catch (cause) {
        if (
          scope === get(localWorkScope) &&
          epoch === localWorkEpoch(scope) &&
          target === savedTarget &&
          !draft
        )
          restore(saved);
        throw cause;
      }
    } catch {
      if (
        target !== savedTarget ||
        scope !== get(localWorkScope) ||
        epoch !== localWorkEpoch(scope)
      )
        return;
      error =
        "Could not discard the saved draft. It is retained on this device.";
    }
  }
  onMount(() => {
    const confirmed = (event) => {
      if (
        event.detail.scope === get(localWorkScope) &&
        event.detail.operation === "events.lifecycle" &&
        !(
          event.type === "anx:mutation-progress" &&
          event.detail.receipt?.events?.length ===
            event.detail.request.items.length
        )
      )
        Promise.resolve(onconfirmed(event.detail)).catch(() => {});
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
    onReady: markReady,
    checkpointClicks: false,
  }}
  onsubmit={(event) => {
    event.preventDefault();
    void submit().catch(() => {});
  }}
  onlocal-save-error={() => {
    error =
      "Local saving is unavailable. Your request is still visible; copy it before closing this page.";
  }}
  data-event-lifecycle-work
>
  <MutationSendStatus {target} />
  {#if draft?.events?.length}
    <div class="mb-3 rounded-md border border-line bg-panel p-3 text-meta">
      <p class="font-medium text-fg">Saved event request</p>
      <p class="mt-1 break-words text-fg-muted">
        {draft.action}: {draft.events.map((doc) => doc.title).join(", ")}
      </p>
      {#if draft.confirmed?.length}<p class="mt-1 break-words text-fg-muted">
          {draft.confirmed.length} earlier events already confirmed.
        </p>{/if}
      {#if ["trash", "restore"].includes(draft.action)}
        <label class="mt-2 block text-fg"
          >Reason
          <textarea
            aria-label="Event action reason"
            class="mt-1 w-full rounded border border-line bg-bg-soft p-2 text-fg"
            disabled={Boolean(pending) || preparing}
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
          disabled={Boolean(pending) || preparing}
          onclick={() => void discard()}>Discard draft</button
        >
      </div>
    </div>
  {/if}
  {#if error}<p class="mb-3 text-meta text-danger-text" role="alert">
      {error}
    </p>{/if}
</form>
