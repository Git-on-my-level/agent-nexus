<script>
  import { get } from "svelte/store";
  import { onDestroy } from "svelte";
  import { durableForm } from "$lib/actions/durableForm.js";
  import { localWorkScope } from "$lib/localWorkScope.js";
  import { localWorkEpoch, saveLocalWork } from "$lib/localWorkStore.js";
  import {
    enqueueInboxControls,
    inboxControlLabel,
    mutationOutbox,
  } from "$lib/mutationOutbox.js";
  import { coreClient } from "$lib/coreClient.js";
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  let { target = "inbox:controls", href } = $props();
  let draft = $state(null);
  let ready = $state(false);
  let preparing = $state(false);
  let error = $state("");
  let alive = $state(true);
  const waiters = new Set();
  let pending = $derived(
    $mutationOutbox.find((entry) => entry.target === target),
  );
  function markReady(value) {
    ready = value;
    if (value) {
      const all = [...waiters];
      waiters.clear();
      all.forEach((resolve) => resolve());
    }
  }
  onDestroy(() => {
    alive = false;
    const all = [...waiters];
    waiters.clear();
    all.forEach((resolve) => resolve());
  });
  function restore(value) {
    draft = value;
    error = "";
  }
  export async function queue(items) {
    const captured = JSON.parse(JSON.stringify({ items }));
    const scope = get(localWorkScope),
      epoch = localWorkEpoch(scope);
    while (!ready && alive)
      await new Promise((resolve) => waiters.add(resolve));
    if (
      !alive ||
      scope !== get(localWorkScope) ||
      epoch !== localWorkEpoch(scope)
    )
      throw new Error("Select the inbox item again.");
    if (pending || preparing || draft?.items?.length)
      throw new Error(
        "Send or edit the saved inbox request before starting another.",
      );
    draft = captured;
    await submit();
  }
  async function submit() {
    if (!draft?.items?.length || pending || preparing) return;
    preparing = true;
    error = "";
    const scope = get(localWorkScope),
      epoch = localWorkEpoch(scope),
      value = JSON.parse(JSON.stringify(draft));
    try {
      await saveLocalWork(scope, "draft", target, value, { epoch });
      if (
        !alive ||
        scope !== get(localWorkScope) ||
        epoch !== localWorkEpoch(scope)
      )
        return;
      await enqueueInboxControls({ draft: value, target, href });
    } catch (e) {
      if (scope === get(localWorkScope) && epoch === localWorkEpoch(scope))
        error =
          e.message ||
          "Your request is still here. Copy it before closing this page.";
      throw e;
    } finally {
      preparing = false;
    }
  }
  async function latest() {
    if (pending || preparing) return;
    preparing = true;
    const scope = get(localWorkScope),
      epoch = localWorkEpoch(scope),
      saved = JSON.stringify(draft);
    try {
      const items = [];
      for (const item of draft.items) {
        let body;
        if (item.action === "read") {
          const result = await coreClient.getHomeUnread();
          const group = result.groups?.find((row) => row.group_ref === item.id);
          if (!group?.newest_event)
            throw Object.assign(new Error("Activity no longer unread"), {
              publicMessage:
                "This activity is no longer unread. Discard this draft if it was already handled.",
            });
          body = {
            group_ref: item.id,
            expected_newest_event_cursor: {
              id: group.newest_event.id,
              ts: group.newest_event.ts,
            },
          };
        } else if (item.action === "archive") {
          const result = await coreClient.getWork(
            item.ref || `card:${item.id}`,
          );
          body = {
            if_version: result.work.version,
            if_lifecycle: {
              archived_at: result.work.archived_at ?? null,
              trashed_at: result.work.trashed_at ?? null,
            },
            if_latest_observation_id: result.work.latest_observation?.id || "",
          };
        } else if (item.action === "dispatch") {
          const decision = await coreClient.getPmDecision(item.id);
          const action = await coreClient.getPmAction(decision.action_id);
          body = {
            if_revision: decision.revision,
            if_action_revision: action.revision,
          };
        } else {
          const action = await coreClient.getPmAction(item.id);
          body = { if_revision: action.revision };
        }
        items.push({ ...item, body });
      }
      if (
        alive &&
        scope === get(localWorkScope) &&
        epoch === localWorkEpoch(scope) &&
        saved === JSON.stringify(draft)
      ) {
        draft = { ...draft, items };
        error = "";
      }
    } catch (e) {
      if (
        scope === get(localWorkScope) &&
        epoch === localWorkEpoch(scope) &&
        saved === JSON.stringify(draft)
      )
        error =
          e.publicMessage ||
          "Could not load the latest versions. Your original request is retained.";
    } finally {
      preparing = false;
    }
  }
  async function discard() {
    if (pending || preparing) return;
    const scope = get(localWorkScope),
      epoch = localWorkEpoch(scope),
      saved = draft;
    restore(null);
    try {
      await saveLocalWork(scope, "draft", target, null, { epoch });
    } catch {
      if (
        scope === get(localWorkScope) &&
        epoch === localWorkEpoch(scope) &&
        !draft
      ) {
        draft = saved;
        error = "Could not remove the saved draft.";
      }
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
    void submit().catch(() => {});
  }}
  onlocal-save-error={() =>
    (error =
      "Local saving is unavailable. Copy this request before closing this page.")}
  data-inbox-control-work
>
  <MutationSendStatus {target} />
  {#if draft?.items?.length && !pending}
    <div class="mb-3 rounded-md border border-line bg-panel p-3 text-meta">
      <p class="font-medium text-fg">Saved inbox request</p>
      <textarea
        class="mt-2 w-full rounded border border-line bg-bg-soft p-2 text-fg"
        aria-label="Saved inbox request"
        readonly
        value={draft.items
          .map((item) => `${inboxControlLabel(item.action)}: ${item.title}`)
          .join("\n")}
      ></textarea>
      <details class="mt-2 text-micro text-fg-muted">
        <summary>Saved versions</summary>
        <pre
          class="mt-1 whitespace-pre-wrap break-words"
          data-inbox-control-bases>{JSON.stringify(draft, null, 2)}</pre>
      </details>
      {#if draft.confirmed?.length}<p>
          {draft.confirmed.length} earlier actions already confirmed.
        </p>{/if}
      <div class="mt-2 flex gap-2">
        <button type="submit" disabled={preparing}>Send saved request</button>
        <button type="button" disabled={preparing} onclick={() => void latest()}
          >Use latest version</button
        >
        <button
          type="button"
          disabled={preparing}
          onclick={() => void discard()}>Discard draft</button
        >
      </div>
    </div>
  {/if}
  {#if error}<p class="mb-2 text-meta text-danger-text" role="alert">
      {error}
    </p>{/if}
</form>
