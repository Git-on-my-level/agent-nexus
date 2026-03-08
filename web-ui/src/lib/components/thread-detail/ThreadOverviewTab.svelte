<script>
  import { threadDetailStore } from "$lib/threadDetailStore";
  import { actorRegistry, lookupActorDisplayName } from "$lib/actorSession";
  import {
    formatTimestamp,
    isoToDatetimeLocal,
    datetimeLocalToIso,
  } from "$lib/formatDate";
  import ProvenanceBadge from "$lib/components/ProvenanceBadge.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import {
    buildThreadPatch,
    parseListInput,
    serializeListInput,
  } from "$lib/threadPatch";
  import {
    THREAD_SCHEDULE_PRESETS,
    THREAD_SCHEDULE_PRESET_LABELS,
    cadencePresetFromValue,
    cadenceToRequestValue,
    formatCadenceLabel,
    isLikelyCronExpression,
    validateCadenceSelection,
  } from "$lib/threadFilters";
  import { parseRef } from "$lib/typedRefs";

  let { threadId, onSave, conflictWarning = "", editNotice = "" } = $props();

  let snapshot = $derived(threadDetailStore.snapshot);
  let actorName = $derived((id) => lookupActorDisplayName(id, $actorRegistry));

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
      nextActionsInput: serializeListInput(thread.next_actions ?? []),
      keyArtifactsInput: serializeListInput(thread.key_artifacts ?? []),
    };
  }

  function beginEdit() {
    if (!snapshot) return;
    editError = "";
    editDraft = toEditDraft(snapshot);
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
        fallbackCadence: snapshot?.cadence ?? "",
      }),
      next_check_in_at: editDraft.next_check_in_at
        ? datetimeLocalToIso(editDraft.next_check_in_at)
        : null,
      tags: parseListInput(editDraft.tagsInput),
      current_summary: editDraft.current_summary.trim(),
      next_actions: parseListInput(editDraft.nextActionsInput),
      key_artifacts: parseListInput(editDraft.keyArtifactsInput),
    };
  }

  async function handleSave() {
    if (!snapshot || !editDraft) return;
    savingEdit = true;
    editError = "";
    try {
      const cadenceError = validateCadenceSelection({
        preset: editDraft.cadencePreset,
        customCron: editDraft.cadenceCron,
        fallbackCadence: snapshot.cadence,
        allowLegacyCustom: true,
      });
      if (cadenceError) {
        editError = cadenceError;
        return;
      }
      const keyArtifactRefs = parseListInput(editDraft.keyArtifactsInput);
      const invalidRefs = keyArtifactRefs.filter((r) => {
        const p = parseRef(normalizeKeyArtifactRef(r));
        return !p.prefix || !p.value;
      });
      if (invalidRefs.length > 0) {
        editError = `Invalid key artifact refs: ${invalidRefs.join(", ")}`;
        return;
      }
      const patch = buildThreadPatch(snapshot, buildDraftSnapshotFromEdit());
      if (Object.keys(patch).length === 0) {
        editNotice = "No changes to save.";
        savingEdit = false;
        return;
      }
      await onSave(threadId, patch, snapshot.updated_at);
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
  <p class="mt-3 rounded-md bg-amber-50 px-3 py-2 text-xs text-amber-700">
    {conflictWarning}
  </p>
{/if}
{#if editNotice}
  <p class="mt-3 rounded-md bg-emerald-50 px-3 py-2 text-xs text-emerald-700">
    {editNotice}
  </p>
{/if}

{#if snapshot}
  <div class="mt-4 rounded-lg border border-gray-200 bg-white">
    <div
      class="flex items-center justify-between border-b border-gray-100 px-4 py-2.5"
    >
      <h2 class="text-xs font-semibold uppercase tracking-wider text-gray-400">
        Details
      </h2>
      <button
        class="text-xs font-medium text-indigo-600 hover:text-indigo-800"
        onclick={editOpen ? cancelEdit : beginEdit}
        type="button"
      >
        {editOpen ? "Cancel" : "Edit"}
      </button>
    </div>

    <div
      class="grid grid-cols-2 gap-x-6 gap-y-2 px-4 py-3 text-sm sm:grid-cols-4"
    >
      <div>
        <p class="text-xs text-gray-400">Type</p>
        <p class="capitalize text-gray-900">{snapshot.type}</p>
      </div>
      <div>
        <p class="text-xs text-gray-400">Cadence</p>
        <p class="text-gray-900">{formatCadenceLabel(snapshot.cadence)}</p>
      </div>
      <div>
        <p class="text-xs text-gray-400">Next check-in</p>
        <p class="text-gray-900">
          {snapshot.next_check_in_at
            ? formatTimestamp(snapshot.next_check_in_at)
            : "—"}
        </p>
      </div>
      <div>
        <p class="text-xs text-gray-400">Updated</p>
        <p class="text-gray-900">
          {formatTimestamp(snapshot.updated_at) || "—"} by {actorName(
            snapshot.updated_by,
          )}
        </p>
      </div>
    </div>

    {#if (snapshot.tags ?? []).length > 0}
      <div class="border-t border-gray-100 px-4 py-2.5">
        <div class="flex flex-wrap gap-1.5">
          {#each snapshot.tags ?? [] as tag}
            <span class="rounded bg-gray-100 px-2 py-0.5 text-xs text-gray-600"
              >{tag}</span
            >
          {/each}
        </div>
      </div>
    {/if}

    <div class="border-t border-gray-100 px-4 py-3">
      <p class="text-xs text-gray-400">Summary</p>
      <p class="mt-1 text-sm text-gray-800">{snapshot.current_summary}</p>
    </div>

    {#if (snapshot.next_actions ?? []).length > 0}
      <div class="border-t border-gray-100 px-4 py-3">
        <p class="text-xs text-gray-400">Next actions</p>
        <ul class="mt-1 list-inside list-disc text-sm text-gray-800">
          {#each snapshot.next_actions ?? [] as action}<li>
              {action}
            </li>{/each}
        </ul>
      </div>
    {/if}

    {#if (snapshot.key_artifacts ?? []).length > 0}
      <div class="border-t border-gray-100 px-4 py-3">
        <p class="text-xs text-gray-400">Key artifacts</p>
        <div class="mt-1 flex flex-wrap gap-2 text-sm">
          {#each snapshot.key_artifacts ?? [] as artifactId}
            <RefLink
              refValue={normalizeKeyArtifactRef(artifactId)}
              {threadId}
            />
          {/each}
        </div>
      </div>
    {/if}

    <div class="border-t border-gray-100 px-4 py-2.5">
      <ProvenanceBadge provenance={snapshot.provenance} />
    </div>

    <details class="border-t border-gray-100">
      <summary
        class="cursor-pointer px-4 py-2.5 text-xs text-gray-400 hover:text-gray-600"
        >Raw JSON</summary
      >
      <pre
        class="overflow-auto px-4 pb-3 text-[11px] text-gray-600">{JSON.stringify(
          snapshot,
          null,
          2,
        )}</pre>
    </details>
  </div>

  {#if editOpen && editDraft}
    <form
      class="mt-3 rounded-lg border border-gray-200 bg-white p-4"
      onsubmit={(event) => {
        event.preventDefault();
        void handleSave();
      }}
    >
      {#if editError}<p
          class="mb-3 rounded bg-red-50 px-3 py-1.5 text-xs text-red-700"
        >
          {editError}
        </p>{/if}
      <div class="grid gap-3 sm:grid-cols-2">
        <label class="text-xs font-medium text-gray-600 sm:col-span-2"
          >Title <input
            bind:value={editDraft.title}
            class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
            required
            type="text"
          /></label
        >
        <label class="text-xs font-medium text-gray-600"
          >Type <select
            bind:value={editDraft.type}
            class="mt-1 w-full rounded border border-gray-200 px-2 py-1.5 text-sm"
            ><option value="case">case</option><option value="process"
              >process</option
            ><option value="relationship">relationship</option><option
              value="initiative">initiative</option
            ><option value="incident">incident</option><option value="other"
              >other</option
            ></select
          ></label
        >
        <label class="text-xs font-medium text-gray-600"
          >Status <select
            bind:value={editDraft.status}
            class="mt-1 w-full rounded border border-gray-200 px-2 py-1.5 text-sm"
            ><option value="active">active</option><option value="paused"
              >paused</option
            ><option value="closed">closed</option></select
          ></label
        >
        <label class="text-xs font-medium text-gray-600"
          >Priority <select
            bind:value={editDraft.priority}
            class="mt-1 w-full rounded border border-gray-200 px-2 py-1.5 text-sm"
            ><option value="p0">Critical (P0)</option><option value="p1"
              >High (P1)</option
            ><option value="p2">Medium (P2)</option><option value="p3"
              >Low (P3)</option
            ></select
          ></label
        >
        <label class="text-xs font-medium text-gray-600"
          >Schedule <select
            bind:value={editDraft.cadencePreset}
            class="mt-1 w-full rounded border border-gray-200 px-2 py-1.5 text-sm"
            >{#each THREAD_SCHEDULE_PRESETS as cadence}
              <option value={cadence}
                >{THREAD_SCHEDULE_PRESET_LABELS[cadence]}</option
              >
            {/each}</select
          ></label
        >
        {#if editDraft.cadencePreset === "custom"}
          <label class="text-xs font-medium text-gray-600"
            >Cron expression <input
              bind:value={editDraft.cadenceCron}
              class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
              placeholder="0 9 * * *"
              type="text"
            /><span class="mt-1 block text-[11px] text-gray-400"
              >Use five cron fields in server timezone.</span
            ></label
          >
        {/if}
        <label class="text-xs font-medium text-gray-600"
          >Next check-in <input
            bind:value={editDraft.next_check_in_at}
            class="mt-1 w-full rounded border border-gray-200 px-2 py-1.5 text-sm"
            type="datetime-local"
          /></label
        >
        <label class="text-xs font-medium text-gray-600"
          >Tags (one per line) <textarea
            bind:value={editDraft.tagsInput}
            class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
            rows="2"
          ></textarea></label
        >
        <label class="text-xs font-medium text-gray-600 sm:col-span-2"
          >Summary <textarea
            bind:value={editDraft.current_summary}
            class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
            rows="2"
          ></textarea></label
        >
        <label class="text-xs font-medium text-gray-600 sm:col-span-2"
          >Next actions (one per line) <textarea
            bind:value={editDraft.nextActionsInput}
            class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
            rows="2"
          ></textarea></label
        >
        <label class="text-xs font-medium text-gray-600 sm:col-span-2"
          >Key artifacts (one per line) <textarea
            bind:value={editDraft.keyArtifactsInput}
            class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
            rows="2"
          ></textarea></label
        >
      </div>
      <div class="mt-3 flex gap-2">
        <button
          class="rounded bg-indigo-600 px-4 py-1.5 text-xs font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
          disabled={savingEdit}
          type="submit">{savingEdit ? "Saving..." : "Save changes"}</button
        >
        <button
          class="rounded px-3 py-1.5 text-xs text-gray-500 hover:bg-gray-100"
          onclick={cancelEdit}
          type="button">Cancel</button
        >
      </div>
    </form>
  {/if}
{/if}
