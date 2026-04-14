<script>
  import { topicDetailStore } from "$lib/topicDetailStore";
  import {
    actorRegistry,
    lookupActorDisplayName,
    principalRegistry,
  } from "$lib/actorSession";
  import {
    formatTimestamp,
    isoToDatetimeLocal,
    datetimeLocalToIso,
  } from "$lib/formatDate";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import ProvenanceBadge from "$lib/components/ProvenanceBadge.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import {
    buildTopicPatch,
    describeCron,
    parseListInput,
    serializeListInput,
  } from "$lib/topicPatch";
  import {
    TOPIC_SCHEDULE_PRESETS,
    TOPIC_SCHEDULE_PRESET_LABELS,
    cadencePresetFromValue,
    cadenceToRequestValue,
    formatCadenceLabel,
    isLikelyCronExpression,
    validateCadenceSelection,
  } from "$lib/topicFilters";
  import { parseRef } from "$lib/typedRefs";

  let { threadId, onSave, conflictWarning = "", editNotice = "" } = $props();

  let topic = $derived($topicDetailStore.topic);
  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );

  let editOpen = $state(false);
  let editDraft = $state(null);
  let savingEdit = $state(false);
  let editError = $state("");

  function normalizeKeyArtifactRef(rawValue) {
    const normalized = String(rawValue ?? "").trim();
    if (!normalized) return "";
    const parsed = parseRef(normalized);
    if (parsed.prefix && parsed.value) return normalized;
    return `artifact:${normalized}`;
  }

  function toEditDraft(thread) {
    const cadenceValue = thread.cadence ?? "reactive";
    const cadencePreset = cadencePresetFromValue(cadenceValue);
    return {
      title: thread.title ?? "",
      type: thread.type ?? "case",
      status: thread.status ?? "active",
      priority: thread.priority ?? "p2",
      cadencePreset,
      cadenceCron:
        cadencePreset === "custom" && isLikelyCronExpression(cadenceValue)
          ? cadenceValue
          : "",
      next_check_in_at: isoToDatetimeLocal(thread.next_check_in_at ?? ""),
      current_summary: thread.current_summary ?? "",
      tagsInput: serializeListInput(thread.tags ?? []),
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
      type: editDraft.type,
      status: editDraft.status,
      priority: editDraft.priority,
      cadence: cadenceToRequestValue({
        preset: editDraft.cadencePreset,
        customCron: editDraft.cadenceCron,
        fallbackCadence: topic?.cadence ?? "",
      }),
      next_check_in_at: editDraft.next_check_in_at
        ? datetimeLocalToIso(editDraft.next_check_in_at)
        : null,
      tags: parseListInput(editDraft.tagsInput),
      current_summary: editDraft.current_summary.trim(),
    };
  }

  async function handleSave() {
    if (!topic || !editDraft) return;
    savingEdit = true;
    editError = "";
    try {
      const cadenceError = validateCadenceSelection({
        preset: editDraft.cadencePreset,
        customCron: editDraft.cadenceCron,
        fallbackCadence: topic.cadence,
        allowLegacyCustom: true,
      });
      if (cadenceError) {
        editError = cadenceError;
        return;
      }
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
  <p
    class="mt-3 rounded-md bg-amber-500/10 px-3 py-2 text-[12px] text-amber-400"
  >
    {conflictWarning}
  </p>
{/if}
{#if editNotice}
  <p
    class="mt-3 rounded-md bg-emerald-500/10 px-3 py-2 text-[12px] text-emerald-400"
  >
    {editNotice}
  </p>
{/if}

{#if topic}
  <div
    class="mt-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]"
  >
    <div
      class="flex items-center justify-between border-b border-[var(--ui-border-subtle)] px-4 py-2.5"
    >
      <h2 class="text-[12px] font-medium text-[var(--ui-text-muted)]">
        Details
      </h2>
      <button
        class="cursor-pointer rounded px-2 py-1 text-[12px] font-medium text-indigo-400 hover:bg-[var(--ui-bg-soft)] hover:text-indigo-300"
        onclick={editOpen ? cancelEdit : beginEdit}
        type="button"
      >
        {editOpen ? "Cancel" : "Edit"}
      </button>
    </div>

    <div
      class="flex flex-wrap items-center gap-x-2 gap-y-1 px-4 py-2.5 text-[12px] text-[var(--ui-text-muted)]"
    >
      <span class="capitalize text-[var(--ui-text)]">{topic.type}</span>
      <span aria-hidden="true">·</span>
      <span>{formatCadenceLabel(topic.cadence)}</span>
      {#if topic.next_check_in_at}
        <span aria-hidden="true">·</span>
        <span>next check-in {formatTimestamp(topic.next_check_in_at)}</span>
      {/if}
      <span aria-hidden="true">·</span>
      <span
        >updated {formatTimestamp(topic.updated_at) || "—"} by {actorName(
          topic.updated_by,
        )}</span
      >
    </div>

    {#if (topic.tags ?? []).length > 0}
      <div class="border-t border-[var(--ui-border-subtle)] px-4 py-2.5">
        <div class="flex flex-wrap gap-1.5">
          {#each topic.tags ?? [] as tag}
            <span
              class="rounded bg-[var(--ui-border)] px-2 py-0.5 text-[12px] text-[var(--ui-text-muted)]"
              >{tag}</span
            >
          {/each}
        </div>
      </div>
    {/if}

    <div class="border-t border-[var(--ui-border-subtle)] px-4 py-3">
      <p class="text-[12px] text-[var(--ui-text-muted)]">Summary</p>
      <MarkdownRenderer
        source={topic.current_summary}
        class="mt-1 text-[13px] text-[var(--ui-text)]"
      />
    </div>

    {#if (topic.next_actions ?? []).length > 0}
      <div class="border-t border-[var(--ui-border-subtle)] px-4 py-3">
        <p class="text-[12px] text-[var(--ui-text-muted)]">Next actions</p>
        <ul
          class="mt-1 list-inside list-disc text-[13px] text-[var(--ui-text)]"
        >
          {#each topic.next_actions ?? [] as action}<li>
              {action}
            </li>{/each}
        </ul>
      </div>
    {/if}

    {#if (topic.key_artifacts ?? []).length > 0}
      <div class="border-t border-[var(--ui-border-subtle)] px-4 py-3">
        <p class="text-[12px] text-[var(--ui-text-muted)]">Key artifacts</p>
        <div class="mt-1 flex flex-wrap gap-2 text-[13px]">
          {#each topic.key_artifacts ?? [] as artifactId}
            <RefLink
              refValue={normalizeKeyArtifactRef(artifactId)}
              {threadId}
            />
          {/each}
        </div>
      </div>
    {/if}

    <div class="border-t border-[var(--ui-border-subtle)] px-4 py-2.5">
      <ProvenanceBadge provenance={topic.provenance} />
    </div>

    <details class="border-t border-[var(--ui-border-subtle)]">
      <summary
        class="cursor-pointer px-4 py-2.5 text-[12px] text-[var(--ui-text-muted)] hover:text-[var(--ui-text)]"
        >Debug data</summary
      >
      <pre
        class="overflow-auto px-4 pb-3 text-[11px] text-[var(--ui-text-muted)]">{JSON.stringify(
          topic,
          null,
          2,
        )}</pre>
    </details>
  </div>

  {#if editOpen && editDraft}
    <form
      class="mt-3 border-t border-[var(--ui-border)] p-4"
      onsubmit={(event) => {
        event.preventDefault();
        void handleSave();
      }}
    >
      {#if editError}<p
          class="mb-3 rounded bg-red-500/10 px-3 py-1.5 text-[12px] text-red-400"
        >
          {editError}
        </p>{/if}
      <div class="grid gap-3 sm:grid-cols-2">
        <label
          class="text-[12px] font-medium text-[var(--ui-text-muted)] sm:col-span-2"
          >Title <input
            bind:value={editDraft.title}
            class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
            required
            type="text"
          /></label
        >
        <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >Type <select
            bind:value={editDraft.type}
            class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2 py-1.5 text-[13px] text-[var(--ui-text)]"
            ><option value="case">Case</option><option value="process"
              >Process</option
            ><option value="relationship">Relationship</option><option
              value="initiative">Initiative</option
            ><option value="incident">Incident</option><option value="other"
              >Other</option
            ></select
          ></label
        >
        <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >Status <select
            bind:value={editDraft.status}
            class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2 py-1.5 text-[13px] text-[var(--ui-text)]"
            ><option value="active">Active</option><option value="paused"
              >Paused</option
            ><option value="closed">Closed</option></select
          ></label
        >
        <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >Priority <select
            bind:value={editDraft.priority}
            class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2 py-1.5 text-[13px] text-[var(--ui-text)]"
            ><option value="p0">Critical (P0)</option><option value="p1"
              >High (P1)</option
            ><option value="p2">Medium (P2)</option><option value="p3"
              >Low (P3)</option
            ></select
          ></label
        >
        <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >Schedule <select
            bind:value={editDraft.cadencePreset}
            class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2 py-1.5 text-[13px] text-[var(--ui-text)]"
            >{#each TOPIC_SCHEDULE_PRESETS as cadence}
              <option value={cadence}
                >{TOPIC_SCHEDULE_PRESET_LABELS[cadence]}</option
              >
            {/each}</select
          ></label
        >
        {#if editDraft.cadencePreset === "custom"}
          <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
            >Cron expression <input
              bind:value={editDraft.cadenceCron}
              class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
              placeholder="0 9 * * *"
              type="text"
            />{#if describeCron(editDraft.cadenceCron)}<span
                class="mt-1 block text-[11px] text-[var(--ui-text-muted)]"
                >{describeCron(editDraft.cadenceCron)}</span
              >{/if}<span
              class="mt-0.5 block text-[11px] text-[var(--ui-text-muted)]"
              >Five cron fields, server timezone.</span
            ></label
          >
        {/if}
        <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >Next check-in <input
            bind:value={editDraft.next_check_in_at}
            class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2 py-1.5 text-[13px] text-[var(--ui-text)]"
            type="datetime-local"
          /></label
        >
        <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >Tags (one per line) <textarea
            bind:value={editDraft.tagsInput}
            class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
            rows="2"
          ></textarea></label
        >
        <label
          class="text-[12px] font-medium text-[var(--ui-text-muted)] sm:col-span-2"
          >Summary <textarea
            bind:value={editDraft.current_summary}
            class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
            rows="2"
          ></textarea></label
        >
      </div>
      <div class="mt-3 flex gap-2">
        <button
          class="cursor-pointer rounded bg-indigo-600 px-4 py-1.5 text-[12px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
          disabled={savingEdit}
          type="submit">{savingEdit ? "Saving..." : "Save changes"}</button
        >
        <button
          class="cursor-pointer rounded px-3 py-1.5 text-[12px] text-[var(--ui-text-muted)] hover:bg-[var(--ui-bg-soft)]"
          onclick={cancelEdit}
          type="button">Cancel</button
        >
      </div>
    </form>
  {/if}
{/if}
