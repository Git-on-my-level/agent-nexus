<script>
  import { topicDetailStore } from "$lib/topicDetailStore";
  import IdsIntegrityDisclosure from "$lib/components/IdsIntegrityDisclosure.svelte";
  import Button from "$lib/components/Button.svelte";
  import ProvenanceBadge from "$lib/components/ProvenanceBadge.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import { splitTypedRef } from "$lib/inboxUtils";
  import { buildTopicPatch } from "$lib/topicPatch";
  import { buildPrimitiveRefRoutes } from "$lib/refLinkModel";

  let { threadId, onSave, conflictWarning = "", editNotice = "" } = $props();

  let topic = $derived($topicDetailStore.topic);
  let topicIntegrityRows = $derived.by(() => {
    if (!topic) return [];
    const topicRef = String(topic.topic_ref ?? "").trim();
    const p = splitTypedRef(topicRef);
    const rows = [];
    if (p.prefix === "topic" && p.id) {
      rows.push({
        label: "Topic ID",
        value: p.id,
        copyLabel: "Copy topic ID",
      });
    }
    if (topic.id) {
      rows.push({
        label: "Thread ID",
        value: String(topic.id),
        copyLabel: "Copy thread ID",
      });
    }
    if (topicRef) {
      rows.push({
        label: "topic_ref",
        value: topicRef,
        copyLabel: "Copy topic ref",
        mono: true,
      });
    }
    return rows;
  });
  let topicRawJson = $derived(topic ? JSON.stringify(topic, null, 2) : "");

  let editOpen = $state(false);
  let editDraft = $state(null);
  let savingEdit = $state(false);
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

  function toEditDraft(thread) {
    return {
      title: thread.title ?? "",
      summary: thread.summary ?? thread.current_summary ?? "",
    };
  }

  function beginEdit() {
    if (!topic) return;
    editError = "";
    editDraft = toEditDraft(topic);
    editOpen = true;
  }

  function cancelEdit() {
    editOpen = false;
    editDraft = null;
    editError = "";
  }

  function buildDraftSnapshotFromEdit() {
    return {
      title: editDraft.title.trim(),
      summary: editDraft.summary.trim(),
    };
  }

  async function handleSave() {
    if (!topic || !editDraft) return;
    savingEdit = true;
    editError = "";
    try {
      const patch = buildTopicPatch(topic, buildDraftSnapshotFromEdit());
      if (Object.keys(patch).length === 0) {
        editNotice = "No changes to save.";
        savingEdit = false;
        return;
      }
      await onSave(threadId, patch, topic.updated_at);
      editOpen = false;
      editDraft = null;
    } catch (error) {
      editError = `Failed to save: ${error instanceof Error ? error.message : String(error)}`;
    } finally {
      savingEdit = false;
    }
  }
</script>

{#if conflictWarning}
  <p class="mt-3 rounded-md bg-warn-soft px-3 py-2 text-micro text-warn-text">
    {conflictWarning}
  </p>
{/if}
{#if editNotice}
  <p class="mt-3 rounded-md bg-ok-soft px-3 py-2 text-micro text-ok-text">
    {editNotice}
  </p>
{/if}

{#if topic}
  <div class="mt-4 rounded-md border border-line bg-panel">
    <div
      class="flex items-center justify-between border-b border-line-subtle px-4 py-2.5"
    >
      <h2 class="text-micro font-medium text-fg-muted">Details</h2>
      <button
        class="cursor-pointer rounded px-2 py-1 text-micro font-medium text-accent-text hover:bg-bg-soft hover:text-accent-text"
        onclick={editOpen ? cancelEdit : beginEdit}
        type="button"
      >
        {editOpen ? "Cancel" : "Edit"}
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
              <p class="mt-0.5 text-meta text-fg whitespace-pre-wrap">
                {summary}
              </p>
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
        <ul class="mt-1 list-inside list-disc text-meta text-fg">
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
            <div class="flex flex-wrap items-baseline gap-2">
              <span class="text-micro text-fg-muted">{group.label}</span>
              <div class="flex flex-wrap gap-2">
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

    <div class="border-t border-line-subtle p-3">
      <IdsIntegrityDisclosure
        rows={topicIntegrityRows}
        rawJson={topicRawJson}
        rawJsonCopyLabel="Copy topic JSON"
      />
    </div>
  </div>

  {#if editOpen && editDraft}
    <form
      class="mt-3 border-t border-line p-4"
      onsubmit={(event) => {
        event.preventDefault();
        void handleSave();
      }}
    >
      {#if editError}<p
          class="mb-3 rounded bg-danger-soft px-3 py-1.5 text-micro text-danger-text"
        >
          {editError}
        </p>{/if}
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
      <div class="mt-3 flex gap-2">
        <Button
          variant="primary"
          size="compact"
          type="submit"
          disabled={savingEdit}
          >{savingEdit ? "Saving..." : "Save changes"}</Button
        >
        <button
          class="cursor-pointer rounded px-3 py-1.5 text-micro text-fg-muted hover:bg-bg-soft"
          onclick={cancelEdit}
          type="button">Cancel</button
        >
      </div>
    </form>
  {/if}
{/if}
