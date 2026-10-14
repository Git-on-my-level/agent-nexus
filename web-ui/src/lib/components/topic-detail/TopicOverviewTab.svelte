<script>
  import { onMount } from "svelte";
  import { get } from "svelte/store";
  import { page } from "$app/stores";
  import { coreClient } from "$lib/coreClient";
  import { durableForm } from "$lib/actions/durableForm.js";
  import { localWorkScope } from "$lib/localWorkScope.js";
  import { localWorkEpoch, saveLocalWork } from "$lib/localWorkStore.js";
  import {
    enqueueAuthoring,
    mutationOutbox,
    editMutation,
  } from "$lib/mutationOutbox.js";
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  import { topicDetailStore } from "$lib/topicDetailStore";
  import Button from "$lib/components/Button.svelte";
  import { formatShortcut } from "$lib/keyboardHints.js";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import ProvenanceBadge from "$lib/components/ProvenanceBadge.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import { buildTopicPatch } from "$lib/topicPatch";
  import { buildPrimitiveRefRoutes } from "$lib/refLinkModel";

  let { threadId, onConfirmed = () => {} } = $props();

  let topic = $derived($topicDetailStore.topic);

  let editOpen = $state(false);
  let editDraft = $state(null);
  let savingEdit = $state(false);
  let loadingEdit = $state(false);
  let draftReady = $state(false);
  let editNotice = $state("");
  let topicId = $derived(
    /^topic:(.+)$/.exec(String(topic?.topic_ref ?? ""))?.[1] || "",
  );
  let target = $derived(topicId ? `topic:${topicId}:metadata` : "");
  let saved = $derived(
    $mutationOutbox.find((entry) => entry.target === target),
  );
  let loadTicket = 0;
  let alive = true;
  let editError = $state("");

  let overviewArtifactRoutesById = $derived.by(() => {
    const s = $topicDetailStore;
    return buildPrimitiveRefRoutes({
      artifacts: s.timelineArtifacts ?? [],
      events: [],
      cards: s.timelineCards ?? [],
      documents: s.timelineDocuments ?? [],
      threadId,
    }).artifactRoutesById;
  });

  let topicRefGroups = $derived.by(() => {
    if (!topic) return [];
    const groups = [
      { label: "Owners", refs: topic.owner_refs },
      { label: "Documents", refs: topic.document_refs },
      { label: "Boards", refs: topic.board_refs },
      { label: "Related refs", refs: topic.related_refs },
    ];

    return groups
      .map(({ label, refs }) => ({
        label,
        refs: Array.isArray(refs)
          ? [
              ...new Set(
                refs.map((ref) => String(ref ?? "").trim()).filter(Boolean),
              ),
            ]
          : [],
      }))
      .filter((group) => group.refs.length > 0);
  });

  function snapshot() {
    return editDraft ? JSON.parse(JSON.stringify(editDraft)) : null;
  }

  function restore(value, reason = "") {
    ++loadTicket;
    loadingEdit = false;
    editDraft = value;
    editOpen = !!value;
    if (reason === "confirmed") editNotice = "Changes saved.";
    if (reason === "privacy") {
      editError = "";
      editNotice = "";
    }
  }

  onMount(() => {
    const confirmed = (event) => {
      if (
        event.detail.scope !== get(localWorkScope) ||
        event.detail.target !== target
      )
        return;
      // Canonical display refresh never changes this composer's saved base or
      // newer text. Only the matching durableForm confirmation clears it.
      void onConfirmed();
    };
    window.addEventListener("anx:mutation-confirmed", confirmed);
    return () => {
      alive = false;
      ++loadTicket;
      window.removeEventListener("anx:mutation-confirmed", confirmed);
    };
  });

  async function loadCanonical() {
    const ticket = ++loadTicket;
    const scope = get(localWorkScope);
    const epoch = localWorkEpoch(scope);
    const id = topicId;
    const result = await coreClient.getTopic(id);
    if (
      !alive ||
      ticket !== loadTicket ||
      id !== topicId ||
      scope !== get(localWorkScope) ||
      epoch !== localWorkEpoch(scope)
    )
      return null;
    if (result.topic?.id !== id || !result.topic?.updated_at)
      throw new Error("Topic version is unavailable.");
    return result.topic;
  }

  async function beginEdit() {
    editError = "";
    editNotice = "";
    if (editDraft) {
      editOpen = true;
      return;
    }
    if (!topicId || loadingEdit || !draftReady) return;
    loadingEdit = true;
    try {
      // The thread projection's timestamp is not the topic concurrency token.
      const canonical = await loadCanonical();
      if (!canonical || editDraft) return;
      editDraft = {
        title: canonical.title ?? "",
        summary: canonical.summary ?? "",
        base: {
          title: canonical.title ?? "",
          summary: canonical.summary ?? "",
          updated_at: canonical.updated_at,
        },
      };
      editOpen = true;
    } catch {
      editError =
        "The topic could not be loaded. Try opening the editor again.";
    } finally {
      loadingEdit = false;
    }
  }

  function closeEdit() {
    editOpen = false;
  }

  async function discardEdit() {
    if (saved || savingEdit) return;
    const scope = get(localWorkScope);
    const epoch = localWorkEpoch(scope);
    const draft = snapshot();
    try {
      const saving = saveLocalWork(scope, "draft", target, null, { epoch });
      // Close synchronously: a delayed device write must not clear text typed
      // after this explicit discard was initiated.
      restore(null);
      editError = "";
      await saving;
    } catch {
      if (
        !alive ||
        scope !== get(localWorkScope) ||
        epoch !== localWorkEpoch(scope)
      )
        return;
      if (!editDraft) restore(draft);
      editError =
        "The saved draft could not be discarded. Your text is still here.";
    }
  }

  async function useLatestVersion() {
    if (
      !editDraft ||
      savingEdit ||
      loadingEdit ||
      (saved && saved.state !== "attention")
    )
      return;
    loadingEdit = true;
    editError = "";
    const scope = get(localWorkScope);
    const epoch = localWorkEpoch(scope);
    try {
      // Load before retiring the failed request. A failed read must leave the
      // original request and its exact base available for recovery.
      const canonical = await loadCanonical();
      if (!canonical) return;
      if (saved) await editMutation(saved.key);
      if (
        !alive ||
        !editDraft ||
        scope !== get(localWorkScope) ||
        epoch !== localWorkEpoch(scope)
      )
        return;
      editDraft = {
        ...snapshot(),
        base: {
          title: canonical.title ?? "",
          summary: canonical.summary ?? "",
          updated_at: canonical.updated_at,
        },
      };
      editOpen = true;
    } catch {
      editError =
        "The latest version could not be loaded. Your edits are still here.";
    } finally {
      loadingEdit = false;
    }
  }

  async function handleSave() {
    if (
      !topicId ||
      !editDraft?.base?.updated_at ||
      saved ||
      savingEdit ||
      loadingEdit
    )
      return;
    savingEdit = true;
    editError = "";
    editNotice = "";
    try {
      const draft = snapshot();
      const patch = buildTopicPatch(draft.base, {
        title: draft.title.trim(),
        summary: draft.summary.trim(),
      });
      if (Object.keys(patch).length === 0) {
        editNotice = "No changes to save.";
        return;
      }
      await enqueueAuthoring({
        operation: "topics.patch",
        id: topicId,
        body: {
          patch: { ...patch, provenance: { sources: ["event:ui"] } },
          if_updated_at: draft.base.updated_at,
        },
        target,
        draft,
        preview: `${draft.title}\n\n${draft.summary}`,
        href: `${$page.url.pathname}?tab=about`,
        title: "Saved topic edits",
      });
    } catch {
      editError =
        "Your edits are still here. Review the saved work, or copy your text before closing this tab.";
    } finally {
      savingEdit = false;
    }
  }
</script>

{#if editNotice}
  <p class="mt-3 rounded-md bg-ok-soft px-3 py-2 text-micro text-ok-text">
    {editNotice}
  </p>
{/if}

{#if editError}
  <p
    role="alert"
    class="mt-3 rounded bg-warn-soft px-3 py-2 text-micro text-warn-text"
  >
    {editError}
  </p>
{/if}
{#if topic}
  <div
    use:durableForm={{
      target,
      value: snapshot(),
      read: snapshot,
      onRestore: restore,
      onReady: (ready) => {
        draftReady = ready;
      },
      checkpointClicks: false,
    }}
    onlocal-save-error={(event) => {
      editError = event.detail;
    }}
  >
    <MutationSendStatus {target} />
    <div class="mt-4 rounded-md border border-line bg-panel">
      <div
        class="flex items-center justify-between border-b border-line-subtle px-4 py-2.5"
      >
        <h2 class="text-micro font-medium text-fg-muted">Details</h2>
        <button
          class="cursor-pointer rounded px-2 py-1 text-micro font-medium text-accent-text hover:bg-bg-soft hover:text-accent-text"
          onclick={editOpen ? closeEdit : beginEdit}
          disabled={loadingEdit || !draftReady}
          type="button"
        >
          {loadingEdit ? "Loading…" : editOpen ? "Close editor" : "Edit"}
        </button>
      </div>

      {#if !editOpen}
        <div class="divide-y divide-line-subtle">
          {#if topic.summary || topic.current_summary}
            {@const summary = String(
              topic.summary ?? topic.current_summary ?? "",
            ).trim()}
            {#if summary}
              <div class="px-4 py-3">
                <p class="text-micro text-fg-muted">Description</p>
                <!-- Summaries quote ids and URLs that have no break opportunity. -->
                <MarkdownRenderer
                  source={summary}
                  class="mt-0.5 text-meta text-fg [overflow-wrap:anywhere]"
                />
              </div>
            {/if}
          {/if}
          {#if topic.state}
            <div class="px-4 py-3 text-micro">
              <span class="text-fg-muted">State: </span><span
                class="capitalize text-fg">{topic.state}</span
              >
            </div>
          {/if}
        </div>
      {:else if topic.state}
        <div
          class="flex flex-wrap items-center gap-x-2 gap-y-1 px-4 py-2.5 text-micro text-fg-muted"
        >
          <span class="capitalize text-fg">State: {topic.state}</span>
        </div>
      {/if}

      {#if (topic.next_actions ?? []).length > 0}
        <div class="border-t border-line-subtle px-4 py-3">
          <p class="text-micro text-fg-muted">Next actions</p>
          <ul
            class="mt-1 list-inside list-disc text-meta text-fg [overflow-wrap:anywhere]"
          >
            {#each topic.next_actions ?? [] as action (action)}<li>
                {action}
              </li>{/each}
          </ul>
        </div>
      {/if}

      {#if topicRefGroups.length > 0}
        <div class="border-t border-line-subtle px-4 py-3">
          <p class="text-micro text-fg-muted">Canonical refs</p>
          <div class="mt-1 space-y-2 text-meta">
            {#each topicRefGroups as group (group.label)}
              <div class="flex flex-wrap items-center gap-2">
                <span class="shrink-0 text-micro text-fg-muted"
                  >{group.label}</span
                >
                <div class="flex min-w-0 flex-wrap items-center gap-2">
                  {#each group.refs as ref (ref)}
                    <RefLink
                      refValue={ref}
                      {threadId}
                      humanize
                      artifactRoutesById={overviewArtifactRoutesById}
                    />
                  {/each}
                </div>
              </div>
            {/each}
          </div>
        </div>
      {/if}

      <div class="border-t border-line-subtle px-4 py-2.5">
        <ProvenanceBadge provenance={topic.provenance} />
      </div>
    </div>

    {#if editOpen && editDraft}
      <form
        class="mt-3 border-t border-line p-4"
        data-anx-save-scope
        onsubmit={(event) => {
          event.preventDefault();
          void handleSave();
        }}
      >
        <div class="grid gap-3">
          <label class="text-micro font-medium text-fg-muted"
            >Title <input
              bind:value={editDraft.title}
              class="mt-1 w-full rounded border border-line bg-bg-soft px-2.5 py-1.5 text-meta text-fg"
              required
              type="text"
            /></label
          >
          <label class="text-micro font-medium text-fg-muted"
            >Summary <textarea
              bind:value={editDraft.summary}
              class="mt-1 w-full rounded border border-line bg-bg-soft px-2.5 py-1.5 text-meta text-fg"
              rows="2"
            ></textarea></label
          >
        </div>
        <div class="mt-3 flex flex-wrap gap-2">
          <button
            type="button"
            class="ui-btn-secondary"
            disabled={savingEdit ||
              loadingEdit ||
              Boolean(saved && saved.state !== "attention")}
            onclick={useLatestVersion}>Use latest version</button
          >
          <button
            type="button"
            class="ui-btn-secondary"
            disabled={savingEdit || Boolean(saved)}
            onclick={discardEdit}>Discard draft</button
          >
          <Button
            variant="primary"
            size="compact"
            type="submit"
            disabled={savingEdit ||
              loadingEdit ||
              Boolean(saved) ||
              !editDraft.base?.updated_at}
            saveShortcut
            aria-keyshortcuts="Meta+S Control+S"
            title={`Save changes (${formatShortcut("S")})`}
            >{savingEdit ? "Saving…" : "Save changes"}</Button
          >
          <button
            class="cursor-pointer rounded px-3 py-1.5 text-micro text-fg-muted hover:bg-bg-soft"
            onclick={closeEdit}
            type="button">Close editor</button
          >
        </div>
      </form>
    {/if}
  </div>
{/if}
