<script>
  import { threadDetailStore } from "$lib/threadDetailStore";
  import {
    actorRegistry,
    lookupActorDisplayName,
    selectedActorId,
  } from "$lib/actorSession";
  import {
    formatTimestamp,
    isoToDatetimeLocal,
    datetimeLocalToIso,
  } from "$lib/formatDate";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import {
    buildCommitmentPatch,
    validateCommitmentStatusTransition,
  } from "$lib/commitmentUtils";
  import { parseListInput, serializeListInput } from "$lib/typedRefs.js";

  const COMMITMENT_STATUS_LABELS = {
    open: "Open",
    blocked: "Blocked",
    done: "Completed",
    canceled: "Canceled",
  };

  let { threadId, onCommitmentSave, onCommitmentCreate } = $props();

  let commitments = $derived($threadDetailStore.commitments);
  let commitmentsLoading = $derived($threadDetailStore.commitmentsLoading);
  let timeline = $derived($threadDetailStore.timeline);

  let actorName = $derived((id) => lookupActorDisplayName(id, $actorRegistry));

  let evidenceRefSuggestions = $derived(buildEvidenceRefSuggestions(timeline));

  function buildEvidenceRefSuggestions(events) {
    const suggestions = [];
    for (const event of events ?? []) {
      if (event.type === "receipt_added") {
        const artifactRef = (event.refs ?? []).find((r) =>
          String(r).startsWith("artifact:"),
        );
        if (artifactRef) {
          suggestions.push({
            ref: artifactRef,
            label: event.summary || "Receipt",
          });
        }
      }
      if (event.type === "decision_made") {
        suggestions.push({
          ref: `event:${event.id}`,
          label: event.summary || "Decision",
        });
      }
    }
    return suggestions;
  }

  let commitmentFormOpen = $state(false);
  let createCommitmentDraft = $state(null);
  let creatingCommitment = $state(false);
  let createCommitmentError = $state("");
  let createCommitmentNotice = $state("");
  let editingCommitmentId = $state("");
  let editCommitmentDraft = $state(null);
  let editCommitmentError = $state("");
  let editCommitmentNotice = $state("");
  let commitmentConflictWarning = $state("");
  let savingCommitmentEdit = $state(false);

  function defaultCommitmentOwner() {
    return $selectedActorId || $actorRegistry[0]?.id || "";
  }

  function blankCreateCommitmentDraft() {
    return {
      title: "",
      owner: defaultCommitmentOwner(),
      due_at: "",
      definitionOfDoneInput: "",
      linksInput: `thread:${threadId}`,
    };
  }

  function toCommitmentEditDraft(commitment) {
    return {
      title: commitment.title ?? "",
      owner: commitment.owner ?? defaultCommitmentOwner(),
      due_at: isoToDatetimeLocal(commitment.due_at ?? ""),
      status: commitment.status ?? "open",
      definitionOfDoneInput: serializeListInput(
        commitment.definition_of_done ?? [],
      ),
      linksInput: serializeListInput(commitment.links ?? []),
      statusRefInput: "",
    };
  }

  function statusBadgeClass(status) {
    if (status === "done") return "bg-emerald-500/10 text-emerald-400";
    if (status === "blocked") return "bg-amber-500/10 text-amber-400";
    if (status === "canceled")
      return "bg-[var(--ui-border)] text-[var(--ui-text-muted)]";
    return "bg-blue-500/10 text-blue-400";
  }

  function statusRequirementText(status) {
    if (status === "done")
      return "Evidence required: link to a receipt artifact or decision event.";
    if (status === "canceled")
      return "Evidence required: link to a decision event.";
    return "";
  }

  function commitmentRiskState(commitment) {
    const dueAt = Date.parse(String(commitment?.due_at ?? ""));
    if (!Number.isFinite(dueAt)) {
      return {
        label: commitment?.status === "blocked" ? "Blocked" : "No due date",
        tone:
          commitment?.status === "blocked"
            ? "bg-amber-500/10 text-amber-400"
            : "bg-[var(--ui-border)] text-[var(--ui-text-muted)]",
      };
    }

    const deltaMs = dueAt - Date.now();
    if (deltaMs < 0) {
      return {
        label:
          commitment?.status === "blocked" ? "Blocked and overdue" : "Overdue",
        tone: "bg-red-500/10 text-red-400",
      };
    }

    if (commitment?.status === "blocked") {
      return {
        label: "Blocked",
        tone: "bg-amber-500/10 text-amber-400",
      };
    }

    if (deltaMs <= 48 * 60 * 60 * 1000) {
      return {
        label: "Due soon",
        tone: "bg-amber-500/10 text-amber-400",
      };
    }

    return {
      label: "On track",
      tone: "bg-emerald-500/10 text-emerald-400",
    };
  }

  function beginCommitmentEdit(commitment) {
    createCommitmentNotice = "";
    editCommitmentNotice = "";
    editCommitmentError = "";
    commitmentConflictWarning = "";
    editingCommitmentId = commitment.id;
    editCommitmentDraft = toCommitmentEditDraft(commitment);
  }

  function cancelCommitmentEdit() {
    editingCommitmentId = "";
    editCommitmentDraft = null;
    editCommitmentError = "";
  }

  async function handleCreateCommitment() {
    if (!createCommitmentDraft) return;
    creatingCommitment = true;
    createCommitmentError = "";
    createCommitmentNotice = "";
    try {
      const title = createCommitmentDraft.title.trim();
      const owner = createCommitmentDraft.owner.trim();
      const dueAt = datetimeLocalToIso(createCommitmentDraft.due_at.trim());
      if (!title || !owner || !dueAt) {
        createCommitmentError = "Title, owner, and due date are required.";
        return;
      }
      await onCommitmentCreate(threadId, {
        thread_id: threadId,
        title,
        owner,
        due_at: dueAt,
        status: "open",
        definition_of_done: parseListInput(
          createCommitmentDraft.definitionOfDoneInput,
        ),
        links: parseListInput(createCommitmentDraft.linksInput),
        provenance: { sources: ["actor_statement:ui"] },
      });
      createCommitmentDraft = blankCreateCommitmentDraft();
      createCommitmentNotice = "Commitment created.";
      commitmentFormOpen = false;
    } catch (error) {
      createCommitmentError = `Failed to create commitment: ${error instanceof Error ? error.message : String(error)}`;
    } finally {
      creatingCommitment = false;
    }
  }

  async function handleSaveCommitmentEdit(commitmentId) {
    const original = commitments.find((c) => c.id === commitmentId);
    if (!original || !editCommitmentDraft) return;
    const draft = { ...editCommitmentDraft };
    const isStillEditingTarget = () => editingCommitmentId === commitmentId;
    savingCommitmentEdit = true;
    editCommitmentError = "";
    editCommitmentNotice = "";
    commitmentConflictWarning = "";
    try {
      const draftSnapshot = {
        title: draft.title.trim(),
        owner: draft.owner.trim(),
        due_at: datetimeLocalToIso(draft.due_at.trim()),
        status: draft.status,
        definition_of_done: parseListInput(draft.definitionOfDoneInput),
        links: parseListInput(draft.linksInput),
      };
      const patch = buildCommitmentPatch(original, draftSnapshot);
      if (Object.keys(patch).length === 0) {
        if (isStillEditingTarget()) editCommitmentNotice = "No changes.";
        return;
      }
      const refs = [];
      if (Object.prototype.hasOwnProperty.call(patch, "status")) {
        const v = validateCommitmentStatusTransition(
          patch.status,
          draft.statusRefInput,
        );
        if (!v.valid) {
          if (isStillEditingTarget()) editCommitmentError = v.error;
          return;
        }
        const ref = String(draft.statusRefInput ?? "").trim();
        if (ref) refs.push(ref);
      }
      const payload = { patch, if_updated_at: original.updated_at };
      if (refs.length > 0) payload.refs = refs;
      await onCommitmentSave(commitmentId, payload);
      if (isStillEditingTarget()) {
        editCommitmentNotice = "Commitment updated.";
        cancelCommitmentEdit();
        commitmentConflictWarning = "";
      }
    } catch (error) {
      if (error?.status === 409) {
        commitmentConflictWarning =
          "Updated elsewhere. Reloaded — reapply changes.";
        if (isStillEditingTarget()) cancelCommitmentEdit();
      } else {
        if (isStillEditingTarget()) {
          editCommitmentError = `Failed to update: ${error instanceof Error ? error.message : String(error)}`;
        }
      }
    } finally {
      savingCommitmentEdit = false;
    }
  }

  $effect(() => {
    if (!createCommitmentDraft) {
      createCommitmentDraft = blankCreateCommitmentDraft();
    }
  });

  $effect(() => {
    if (createCommitmentDraft && !createCommitmentDraft.owner) {
      const fallbackOwnerId = defaultCommitmentOwner();
      if (fallbackOwnerId)
        createCommitmentDraft = {
          ...createCommitmentDraft,
          owner: fallbackOwnerId,
        };
    }
  });
</script>

<div
  class="mt-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]"
>
  <div
    class="flex items-center justify-between border-b border-[var(--ui-border-subtle)] px-4 py-2.5"
  >
    <h2
      class="text-[12px] font-semibold uppercase tracking-wider text-[var(--ui-text-muted)]"
    >
      Commitments
    </h2>
    <button
      class="cursor-pointer rounded px-2 py-1 text-[12px] font-medium text-indigo-400 hover:bg-[var(--ui-bg-soft)] hover:text-indigo-300"
      onclick={() => (commitmentFormOpen = !commitmentFormOpen)}
      type="button">{commitmentFormOpen ? "Cancel" : "New"}</button
    >
  </div>

  {#if commitmentConflictWarning}<p
      class="border-b border-[var(--ui-border-subtle)] bg-amber-500/10 px-4 py-2 text-[12px] text-amber-400"
    >
      {commitmentConflictWarning}
    </p>{/if}
  {#if createCommitmentNotice}<p
      class="border-b border-[var(--ui-border-subtle)] bg-emerald-500/10 px-4 py-2 text-[12px] text-emerald-400"
    >
      {createCommitmentNotice}
    </p>{/if}

  {#if commitmentFormOpen}
    <form
      class="border-b border-[var(--ui-border-subtle)] px-4 py-3"
      onsubmit={(event) => {
        event.preventDefault();
        void handleCreateCommitment();
      }}
    >
      {#if createCommitmentError}<p
          class="mb-2 rounded bg-red-500/10 px-3 py-1.5 text-[12px] text-red-400"
        >
          {createCommitmentError}
        </p>{/if}
      <div class="grid gap-2 sm:grid-cols-2">
        <label
          class="text-[12px] font-medium text-[var(--ui-text-muted)] sm:col-span-2"
          >Title <input
            bind:value={createCommitmentDraft.title}
            class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
            required
            type="text"
          /></label
        >
        <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >Owner <select
            bind:value={createCommitmentDraft.owner}
            class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2 py-1.5 text-[13px] text-[var(--ui-text)]"
            required
            ><option disabled value="">Select</option
            >{#each $actorRegistry as actor}<option value={actor.id}
                >{actor.display_name || actor.id}</option
              >{/each}</select
          ></label
        >
        <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
          >Due date <input
            bind:value={createCommitmentDraft.due_at}
            class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2 py-1.5 text-[13px] text-[var(--ui-text)]"
            required
            type="datetime-local"
          /></label
        >
        <label
          class="text-[12px] font-medium text-[var(--ui-text-muted)] sm:col-span-2"
          >Completion criteria (one per line) <textarea
            bind:value={createCommitmentDraft.definitionOfDoneInput}
            class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
            rows="2"
          ></textarea></label
        >
      </div>
      <button
        class="mt-2 cursor-pointer rounded bg-indigo-600 px-3 py-1.5 text-[12px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
        disabled={creatingCommitment}
        type="submit">{creatingCommitment ? "Creating..." : "Create"}</button
      >
    </form>
  {/if}

  {#if commitmentsLoading}
    <p class="px-4 py-3 text-[12px] text-[var(--ui-text-muted)]">Loading...</p>
  {:else if commitments.length === 0}
    <p class="px-4 py-3 text-[13px] text-[var(--ui-text-muted)]">
      No open commitments. All clear.
    </p>
  {:else}
    {#each commitments as commitment, i}
      <div
        class="border-b border-[var(--ui-border-subtle)] px-4 py-3 {i ===
        commitments.length - 1
          ? 'border-b-0'
          : ''}"
        id={`commitment-card-${commitment.id}`}
      >
        <div class="flex items-start justify-between gap-2">
          <div class="min-w-0 flex-1">
            <p class="text-[13px] font-medium text-[var(--ui-text)]">
              {commitment.title || ""}{#if !commitment.title}<span
                  class="font-mono text-[var(--ui-text-subtle)]"
                  >{commitment.id}</span
                >{/if}
            </p>
            <p class="mt-0.5 text-[12px] text-[var(--ui-text-muted)]">
              {actorName(commitment.owner)} · Due {commitment.due_at
                ? formatTimestamp(commitment.due_at)
                : "—"}
            </p>
            <div class="mt-1 flex flex-wrap items-center gap-1.5">
              <span
                class={`rounded px-2 py-0.5 text-[11px] font-medium ${commitmentRiskState(commitment).tone}`}
                >{commitmentRiskState(commitment).label}</span
              >
            </div>
          </div>
          <div class="flex shrink-0 items-center gap-2">
            <span
              class={`rounded px-2 py-0.5 text-[12px] font-medium ${statusBadgeClass(commitment.status)}`}
              >{COMMITMENT_STATUS_LABELS[commitment.status] ??
                commitment.status}</span
            >
            <button
              class="cursor-pointer rounded px-2 py-1 text-[12px] text-[var(--ui-text-muted)] hover:bg-[var(--ui-bg-soft)] hover:text-[var(--ui-text)]"
              onclick={() =>
                editingCommitmentId === commitment.id
                  ? cancelCommitmentEdit()
                  : beginCommitmentEdit(commitment)}
              type="button"
            >
              {editingCommitmentId === commitment.id ? "Cancel" : "Edit"}
            </button>
          </div>
        </div>

        {#if (commitment.definition_of_done ?? []).length > 0}
          <ul
            class="mt-1.5 list-inside list-disc text-[12px] text-[var(--ui-text-muted)]"
          >
            {#each commitment.definition_of_done ?? [] as item}<li>
                <MarkdownRenderer
                  source={item}
                  inline
                  class="text-[12px] text-[var(--ui-text-muted)]"
                />
              </li>{/each}
          </ul>
        {/if}

        {#if (commitment.links ?? []).length > 0}
          <div class="mt-1.5 flex flex-wrap gap-1.5 text-[12px]">
            {#each commitment.links ?? [] as refValue}<RefLink
                {refValue}
                {threadId}
              />{/each}
          </div>
        {/if}

        {#if editingCommitmentId === commitment.id && editCommitmentDraft}
          <form
            class="mt-3 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] p-3"
            onsubmit={(event) => {
              event.preventDefault();
              void handleSaveCommitmentEdit(commitment.id);
            }}
          >
            {#if editCommitmentError}<p
                class="mb-2 rounded bg-red-500/10 px-3 py-1.5 text-[12px] text-red-400"
              >
                {editCommitmentError}
              </p>{/if}
            {#if editCommitmentNotice}<p
                class="mb-2 rounded bg-emerald-500/10 px-3 py-1.5 text-[12px] text-emerald-400"
              >
                {editCommitmentNotice}
              </p>{/if}
            <div class="grid gap-2 sm:grid-cols-2">
              <label
                class="text-[12px] font-medium text-[var(--ui-text-muted)] sm:col-span-2"
                >Title <input
                  bind:value={editCommitmentDraft.title}
                  class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
                  required
                  type="text"
                /></label
              >
              <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
                >Owner <select
                  bind:value={editCommitmentDraft.owner}
                  class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2 py-1.5 text-[13px] text-[var(--ui-text)]"
                  required
                  ><option disabled value="">Select</option
                  >{#each $actorRegistry as actor}<option value={actor.id}
                      >{actor.display_name || actor.id}</option
                    >{/each}</select
                ></label
              >
              <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
                >Due date <input
                  bind:value={editCommitmentDraft.due_at}
                  class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2 py-1.5 text-[13px] text-[var(--ui-text)]"
                  required
                  type="datetime-local"
                /></label
              >
              <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
                >Status <select
                  bind:value={editCommitmentDraft.status}
                  class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2 py-1.5 text-[13px] text-[var(--ui-text)]"
                  >{#each Object.entries(COMMITMENT_STATUS_LABELS) as [value, label]}<option
                      {value}>{label}</option
                    >{/each}</select
                ></label
              >
              <div class="self-end text-[12px] text-[var(--ui-text-muted)]">
                {#if statusRequirementText(editCommitmentDraft.status)}<p
                    class="text-amber-400"
                  >
                    {statusRequirementText(editCommitmentDraft.status)}
                  </p>{/if}
              </div>
              <div
                class="text-[12px] font-medium text-[var(--ui-text-muted)] sm:col-span-2"
              >
                <label
                  >Evidence link <input
                    bind:value={editCommitmentDraft.statusRefInput}
                    class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
                    placeholder="artifact:receipt-123 or event:decision-456"
                    type="text"
                  /></label
                >
                {#if evidenceRefSuggestions.length > 0 && (editCommitmentDraft.status === "done" || editCommitmentDraft.status === "canceled")}
                  <div class="mt-1.5 flex flex-wrap gap-1">
                    {#each evidenceRefSuggestions as suggestion}
                      <button
                        class="cursor-pointer truncate rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2 py-0.5 text-[11px] text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border)] hover:text-[var(--ui-text)]"
                        onclick={() => {
                          editCommitmentDraft.statusRefInput = suggestion.ref;
                        }}
                        title={suggestion.ref}
                        type="button">{suggestion.label}</button
                      >
                    {/each}
                  </div>
                {/if}
              </div>
              <label
                class="text-[12px] font-medium text-[var(--ui-text-muted)] sm:col-span-2"
                >Completion criteria (one per line) <textarea
                  bind:value={editCommitmentDraft.definitionOfDoneInput}
                  class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
                  rows="2"
                ></textarea></label
              >
              <label
                class="text-[12px] font-medium text-[var(--ui-text-muted)] sm:col-span-2"
                >Links (one per line) <textarea
                  bind:value={editCommitmentDraft.linksInput}
                  class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
                  rows="2"
                ></textarea></label
              >
            </div>
            <div class="mt-2 flex gap-2">
              <button
                class="cursor-pointer rounded bg-indigo-600 px-3 py-1.5 text-[12px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
                disabled={savingCommitmentEdit}
                type="submit"
                >{savingCommitmentEdit ? "Saving..." : "Save"}</button
              >
              <button
                class="cursor-pointer rounded px-3 py-1.5 text-[12px] text-[var(--ui-text-muted)] hover:bg-[var(--ui-bg-soft)]"
                onclick={cancelCommitmentEdit}
                type="button">Cancel</button
              >
            </div>
          </form>
        {/if}
      </div>
    {/each}
  {/if}
</div>
