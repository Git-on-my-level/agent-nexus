<script>
  import { get } from "svelte/store";
  import { onMount, onDestroy } from "svelte";
  import { durableForm } from "$lib/actions/durableForm.js";
  import { localWorkScope } from "$lib/localWorkScope.js";
  import { localWorkEpoch, saveLocalWork } from "$lib/localWorkStore.js";
  import { enqueueLocalMutation, mutationOutbox } from "$lib/mutationOutbox.js";
  import { coreClient } from "$lib/coreClient.js";
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  let { href, onconfirmed = () => {} } = $props();
  const target = "workspace:dashboard";
  let draft = $state(null),
    ready = $state(false),
    preparing = $state(false),
    error = $state("");
  const lifetime = { alive: true };
  const waiters = new Set();
  let pending = $derived(
    $mutationOutbox.find((entry) => entry.target === target),
  );
  function restore(value) {
    draft = value;
    error = "";
  }
  function markReady(value) {
    ready = value;
    if (value) {
      [...waiters].forEach((resolve) => resolve());
      waiters.clear();
    }
  }
  onMount(() => {
    const confirmed = (event) => {
      if (
        event.detail?.operation === "workspace.dashboard.set" &&
        event.detail.scope === get(localWorkScope)
      )
        onconfirmed();
    };
    window.addEventListener("anx:mutation-confirmed", confirmed);
    return () =>
      window.removeEventListener("anx:mutation-confirmed", confirmed);
  });
  onDestroy(() => {
    lifetime.alive = false;
    [...waiters].forEach((resolve) => resolve());
    waiters.clear();
  });
  export async function queue(document_ref, base) {
    const scope = get(localWorkScope),
      epoch = localWorkEpoch(scope);
    while (!ready && lifetime.alive)
      await new Promise((resolve) => waiters.add(resolve));
    if (
      !lifetime.alive ||
      scope !== get(localWorkScope) ||
      epoch !== localWorkEpoch(scope)
    )
      return;
    if (pending || preparing || draft) {
      error = "Confirm or edit the saved dashboard choice first.";
      return;
    }
    if (base === undefined) {
      error = "Load the current dashboard setting before pinning a report.";
      return;
    }
    draft = { document_ref, if_updated_at: base };
    await submit();
  }
  async function submit() {
    if (!ready || !draft || pending || preparing) return;
    preparing = true;
    const scope = get(localWorkScope),
      epoch = localWorkEpoch(scope),
      saved = JSON.parse(JSON.stringify(draft));
    try {
      await saveLocalWork(scope, "draft", target, saved, { epoch });
      if (
        !lifetime.alive ||
        scope !== get(localWorkScope) ||
        epoch !== localWorkEpoch(scope)
      )
        return;
      const key = crypto.randomUUID();
      await enqueueLocalMutation({
        key,
        operation: "workspace.dashboard.set",
        target,
        draft: saved,
        href,
        title: "Saved dashboard choice",
        preview: saved.document_ref || "Use newest report",
        request: { ...saved, request_key: key, actor_id: JSON.parse(scope)[3] },
      });
      error = "";
    } catch {
      error =
        "Your dashboard choice is still here. Copy it before closing this page if local saving is unavailable.";
    } finally {
      preparing = false;
    }
  }
  async function discard() {
    if (!ready || !draft || pending || preparing) return;
    preparing = true;
    const scope = get(localWorkScope),
      epoch = localWorkEpoch(scope),
      saved = JSON.stringify(draft);
    try {
      await saveLocalWork(scope, "draft", target, null, { epoch });
      if (
        scope === get(localWorkScope) &&
        epoch === localWorkEpoch(scope) &&
        saved === JSON.stringify(draft)
      )
        draft = null;
    } catch {
      error =
        "The saved choice could not be discarded. Keep this page open and try again.";
    } finally {
      preparing = false;
    }
  }
  async function latest() {
    if (!draft || pending || preparing) return;
    preparing = true;
    const scope = get(localWorkScope),
      epoch = localWorkEpoch(scope),
      saved = JSON.stringify(draft);
    try {
      const current = await coreClient.getDashboardReports();
      if (!Object.hasOwn(current, "pin_updated_at"))
        throw new Error("Setting unavailable");
      if (
        lifetime.alive &&
        scope === get(localWorkScope) &&
        epoch === localWorkEpoch(scope) &&
        saved === JSON.stringify(draft)
      )
        draft = { ...draft, if_updated_at: current.pin_updated_at };
    } catch {
      error =
        "The current dashboard setting could not be loaded. Your choice is retained.";
    } finally {
      preparing = false;
    }
  }
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
    void submit();
  }}
  onlocal-save-error={() =>
    (error =
      "Local saving is unavailable. Copy your dashboard choice before closing this page.")}
>
  <MutationSendStatus {target} />
  {#if draft && !pending}
    <div class="m-3 rounded border border-line p-3 text-meta">
      <p>Saved dashboard choice: {draft.document_ref || "Use newest report"}</p>
      <button type="submit" disabled={!ready || preparing}
        >Save dashboard choice</button
      >
      <button
        type="button"
        disabled={!ready || preparing}
        onclick={() => void latest()}>Use latest version</button
      >
      <button
        type="button"
        disabled={!ready || preparing}
        onclick={() => void discard()}>Discard saved choice</button
      >
    </div>
  {/if}
  {#if error}<p class="m-3 text-meta text-danger-text" role="alert">
      {error}
    </p>{/if}
</form>
