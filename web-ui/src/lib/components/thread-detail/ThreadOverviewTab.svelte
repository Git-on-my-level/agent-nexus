<script>
  import { threadDetailStore } from "$lib/threadDetailStore";
  import { actorRegistry, lookupActorDisplayName } from "$lib/actorSession";
  import {
    formatTimestamp,
    isoToDatetimeLocal,
    datetimeLocalToIso,
  } from "$lib/formatDate";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import ProvenanceBadge from "$lib/components/ProvenanceBadge.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import {
    buildThreadPatch,
    describeCron,
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
  import { projectPath } from "$lib/projectPaths";
  import { page } from "$app/stores";

  let { threadId, onSave, conflictWarning = "", editNotice = "" } = $props();

  let snapshot = $derived($threadDetailStore.snapshot);
  let boardMemberships = $derived($threadDetailStore.boardMemberships);
  let actorName = $derived((id) => lookupActorDisplayName(id, $actorRegistry));
  let projectSlug = $derived($page.params.project);

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

{#if snapshot}
  <div
    class="mt-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]"
  >
    <div
      class="flex items-center justify-between border-b border-[var(--ui-border-subtle)] px-4 py-2.5"
    >
      <h2
        class="text-[12px] font-semibold uppercase tracking-wider text-[var(--ui-text-muted)]"
      >
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
      class="grid grid-cols-2 gap-x-6 gap-y-2 px-4 py-3 text-[13px] sm:grid-cols-4"
    >
      <div>
        <p class="text-[12px] text-[var(--ui-text-muted)]">Type</p>
        <p class="capitalize text-[var(--ui-text)]">{snapshot.type}</p>
      </div>
      <div>
        <p class="text-[12px] text-[var(--ui-text-muted)]">Cadence</p>
        <p class="text-[var(--ui-text)]">
          {formatCadenceLabel(snapshot.cadence)}
        </p>
      </div>
      <div>
        <p class="text-[12px] text-[var(--ui-text-muted)]">Next check-in</p>
        <p class="text-[var(--ui-text)]">
          {snapshot.next_check_in_at
            ? formatTimestamp(snapshot.next_check_in_at)
            : "—"}
        </p>
      </div>
      <div>
        <p class="text-[12px] text-[var(--ui-text-muted)]">Updated</p>
        <p class="text-[var(--ui-text)]">
          {formatTimestamp(snapshot.updated_at) || "—"} by {actorName(
            snapshot.updated_by,
          )}
        </p>
      </div>
    </div>

    {#if (snapshot.tags ?? []).length > 0}
      <div class="border-t border-[var(--ui-border-subtle)] px-4 py-2.5">
        <div class="flex flex-wrap gap-1.5">
          {#each snapshot.tags ?? [] as tag}
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
        source={snapshot.current_summary}
        class="mt-1 text-[13px] text-[var(--ui-text)]"
      />
    </div>

    {#if (snapshot.next_actions ?? []).length > 0}
      <div class="border-t border-[var(--ui-border-subtle)] px-4 py-3">
        <p class="text-[12px] text-[var(--ui-text-muted)]">Next actions</p>
        <ul
          class="mt-1 list-inside list-disc text-[13px] text-[var(--ui-text)]"
        >
          {#each snapshot.next_actions ?? [] as action}<li>
              {action}
            </li>{/each}
        </ul>
      </div>
    {/if}

    {#if (snapshot.key_artifacts ?? []).length > 0}
      <div class="border-t border-[var(--ui-border-subtle)] px-4 py-3">
        <p class="text-[12px] text-[var(--ui-text-muted)]">Key artifacts</p>
        <div class="mt-1 flex flex-wrap gap-2 text-[13px]">
          {#each snapshot.key_artifacts ?? [] as artifactId}
            <RefLink
              refValue={normalizeKeyArtifactRef(artifactId)}
              {threadId}
            />
          {/each}
        </div>
      </div>
    {/if}

    {#if boardMemberships.length > 0}
      <div class="border-t border-[var(--ui-border-subtle)] px-4 py-3">
        <p class="text-[12px] text-[var(--ui-text-muted)]">Boards</p>
        <div class="mt-1 flex flex-wrap gap-2">
          {#each boardMemberships as membership}
            {@const boardId = membership?.board?.id ?? membership?.board_id}
            {@const boardTitle =
              membership?.board?.title ?? membership?.board_title ?? boardId}
            {@const columnKey =
              membership?.card?.column_key ?? membership?.column_key}
            {@const pinnedDocumentId =
              membership?.card?.pinned_document_id ??
              membership?.pinned_document_id}

            {#if boardId}
              <div
                class="inline-flex items-center gap-2 rounded bg-[var(--ui-border)] px-2 py-1 text-[12px] text-[var(--ui-text)]"
              >
                <a
                  class="inline-flex items-center gap-1.5 transition-colors hover:text-indigo-200"
                  href={projectPath(projectSlug, `/boards/${boardId}`)}
                >
                  <span class="font-medium">{boardTitle}</span>
                  {#if columnKey}
                    <span
                      class="rounded bg-[var(--ui-bg-soft)] px-1.5 py-0.5 text-[10px] text-[var(--ui-text-muted)]"
                    >
                      {columnKey}
                    </span>
                  {/if}
                </a>

                {#if pinnedDocumentId}
                  <span class="text-[var(--ui-text-subtle)]">•</span>
                  <a
                    class="text-indigo-300 transition-colors hover:text-indigo-200"
                    href={projectPath(projectSlug, `/docs/${pinnedDocumentId}`)}
                  >
                    Pinned doc: {pinnedDocumentId}
                  </a>
                {/if}
              </div>
            {/if}
          {/each}
        </div>
      </div>
    {/if}

    <div class="border-t border-[var(--ui-border-subtle)] px-4 py-2.5">
      <ProvenanceBadge provenance={snapshot.provenance} />
    </div>

    <details class="border-t border-[var(--ui-border-subtle)]">
      <summary
        class="cursor-pointer px-4 py-2.5 text-[12px] text-[var(--ui-text-muted)] hover:text-[var(--ui-text)]"
        >Raw JSON</summary
      >
      <pre
        class="overflow-auto px-4 pb-3 text-[11px] text-[var(--ui-text-muted)]">{JSON.stringify(
          snapshot,
          null,
          2,
        )}</pre>
    </details>
  </div>

  {#if editOpen && editDraft}
    <form
      class="mt-3 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] p-4"
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
            >{#each THREAD_SCHEDULE_PRESETS as cadence}
              <option value={cadence}
                >{THREAD_SCHEDULE_PRESET_LABELS[cadence]}</option
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
              class="mt-0.5 block text-[11px] text-[var(--ui-text-subtle)]"
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
        <label
          class="text-[12px] font-medium text-[var(--ui-text-muted)] sm:col-span-2"
          >Next actions (one per line) <textarea
            bind:value={editDraft.nextActionsInput}
            class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
            rows="2"
          ></textarea></label
        >
        <label
          class="text-[12px] font-medium text-[var(--ui-text-muted)] sm:col-span-2"
          >Key artifacts (one per line) <textarea
            bind:value={editDraft.keyArtifactsInput}
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
