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
    parseCommitmentListInput,
    serializeCommitmentListInput,
    validateCommitmentStatusTransition,
  } from "$lib/commitmentUtils";

  let { threadId, onCommitmentSave, onCommitmentCreate } = $props();

  let commitments = $derived($threadDetailStore.commitments);
  let commitmentsLoading = $derived($threadDetailStore.commitmentsLoading);

  let actorName = $derived((id) => lookupActorDisplayName(id, $actorRegistry));

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
      definitionOfDoneInput: serializeCommitmentListInput(
        commitment.definition_of_done ?? [],
      ),
      linksInput: serializeCommitmentListInput(commitment.links ?? []),
      statusRefInput: "",
    };
  }

  function statusBadgeClass(status) {
    if (status === "done") return "bg-emerald-100 text-emerald-700";
    if (status === "blocked") return "bg-amber-100 text-amber-700";
    if (status === "canceled") return "bg-gray-100 text-gray-500";
    return "bg-blue-50 text-blue-700";
  }

  function statusRequirementText(status) {
    if (status === "done")
      return "Evidence required: link to a receipt artifact or decision event.";
    if (status === "canceled")
      return "Evidence required: link to a decision event.";
    return "";
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
        definition_of_done: parseCommitmentListInput(
          createCommitmentDraft.definitionOfDoneInput,
        ),
        links: parseCommitmentListInput(createCommitmentDraft.linksInput),
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
        definition_of_done: parseCommitmentListInput(
          draft.definitionOfDoneInput,
        ),
        links: parseCommitmentListInput(draft.linksInput),
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

<div class="mt-4 rounded-lg border border-gray-200 bg-white">
  <div
    class="flex items-center justify-between border-b border-gray-100 px-4 py-2.5"
  >
    <h2 class="text-xs font-semibold uppercase tracking-wider text-gray-400">
      Commitments
    </h2>
    <button
      class="text-xs font-medium text-indigo-600 hover:text-indigo-800"
      onclick={() => (commitmentFormOpen = !commitmentFormOpen)}
      type="button">{commitmentFormOpen ? "Cancel" : "New"}</button
    >
  </div>

  {#if commitmentConflictWarning}<p
      class="border-b border-gray-100 bg-amber-50 px-4 py-2 text-xs text-amber-700"
    >
      {commitmentConflictWarning}
    </p>{/if}
  {#if createCommitmentNotice}<p
      class="border-b border-gray-100 bg-emerald-50 px-4 py-2 text-xs text-emerald-700"
    >
      {createCommitmentNotice}
    </p>{/if}

  {#if commitmentFormOpen}
    <form
      class="border-b border-gray-100 px-4 py-3"
      onsubmit={(event) => {
        event.preventDefault();
        void handleCreateCommitment();
      }}
    >
      {#if createCommitmentError}<p
          class="mb-2 rounded bg-red-50 px-3 py-1.5 text-xs text-red-700"
        >
          {createCommitmentError}
        </p>{/if}
      <div class="grid gap-2 sm:grid-cols-2">
        <label class="text-xs font-medium text-gray-600 sm:col-span-2"
          >Title <input
            bind:value={createCommitmentDraft.title}
            class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
            required
            type="text"
          /></label
        >
        <label class="text-xs font-medium text-gray-600"
          >Owner <select
            bind:value={createCommitmentDraft.owner}
            class="mt-1 w-full rounded border border-gray-200 px-2 py-1.5 text-sm"
            required
            ><option disabled value="">Select</option
            >{#each $actorRegistry as actor}<option value={actor.id}
                >{actor.display_name || actor.id}</option
              >{/each}</select
          ></label
        >
        <label class="text-xs font-medium text-gray-600"
          >Due date <input
            bind:value={createCommitmentDraft.due_at}
            class="mt-1 w-full rounded border border-gray-200 px-2 py-1.5 text-sm"
            required
            type="datetime-local"
          /></label
        >
        <label class="text-xs font-medium text-gray-600 sm:col-span-2"
          >Completion criteria (one per line) <textarea
            bind:value={createCommitmentDraft.definitionOfDoneInput}
            class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
            rows="2"
          ></textarea></label
        >
      </div>
      <button
        class="mt-2 rounded bg-indigo-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
        disabled={creatingCommitment}
        type="submit">{creatingCommitment ? "Creating..." : "Create"}</button
      >
    </form>
  {/if}

  {#if commitmentsLoading}
    <p class="px-4 py-3 text-xs text-gray-400">Loading...</p>
  {:else if commitments.length === 0}
    <p class="px-4 py-3 text-sm text-gray-400">No open commitments.</p>
  {:else}
    {#each commitments as commitment, i}
      <div
        class="border-b border-gray-100 px-4 py-3 {i === commitments.length - 1
          ? 'border-b-0'
          : ''}"
        id={`commitment-card-${commitment.id}`}
      >
        <div class="flex items-start justify-between gap-2">
          <div class="min-w-0 flex-1">
            <p class="text-sm font-medium text-gray-900">
              {commitment.title || commitment.id}
            </p>
            <p class="mt-0.5 text-xs text-gray-500">
              {actorName(commitment.owner)} · Due {commitment.due_at
                ? formatTimestamp(commitment.due_at)
                : "—"}
            </p>
          </div>
          <div class="flex shrink-0 items-center gap-2">
            <span
              class={`rounded px-2 py-0.5 text-xs font-medium ${statusBadgeClass(commitment.status)}`}
              >{commitment.status}</span
            >
            <button
              class="text-xs text-gray-400 hover:text-gray-600"
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
          <ul class="mt-1.5 list-inside list-disc text-xs text-gray-600">
            {#each commitment.definition_of_done ?? [] as item}<li>
                <MarkdownRenderer
                  source={item}
                  inline
                  class="text-xs text-gray-600"
                />
              </li>{/each}
          </ul>
        {/if}

        {#if (commitment.links ?? []).length > 0}
          <div class="mt-1.5 flex flex-wrap gap-1.5 text-xs">
            {#each commitment.links ?? [] as refValue}<RefLink
                {refValue}
                {threadId}
              />{/each}
          </div>
        {/if}

        {#if editingCommitmentId === commitment.id && editCommitmentDraft}
          <form
            class="mt-3 rounded-md border border-gray-200 bg-gray-50 p-3"
            onsubmit={(event) => {
              event.preventDefault();
              void handleSaveCommitmentEdit(commitment.id);
            }}
          >
            {#if editCommitmentError}<p
                class="mb-2 rounded bg-red-50 px-3 py-1.5 text-xs text-red-700"
              >
                {editCommitmentError}
              </p>{/if}
            {#if editCommitmentNotice}<p
                class="mb-2 rounded bg-emerald-50 px-3 py-1.5 text-xs text-emerald-700"
              >
                {editCommitmentNotice}
              </p>{/if}
            <div class="grid gap-2 sm:grid-cols-2">
              <label class="text-xs font-medium text-gray-600 sm:col-span-2"
                >Title <input
                  bind:value={editCommitmentDraft.title}
                  class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
                  required
                  type="text"
                /></label
              >
              <label class="text-xs font-medium text-gray-600"
                >Owner <select
                  bind:value={editCommitmentDraft.owner}
                  class="mt-1 w-full rounded border border-gray-200 px-2 py-1.5 text-sm"
                  required
                  ><option disabled value="">Select</option
                  >{#each $actorRegistry as actor}<option value={actor.id}
                      >{actor.display_name || actor.id}</option
                    >{/each}</select
                ></label
              >
              <label class="text-xs font-medium text-gray-600"
                >Due date <input
                  bind:value={editCommitmentDraft.due_at}
                  class="mt-1 w-full rounded border border-gray-200 px-2 py-1.5 text-sm"
                  required
                  type="datetime-local"
                /></label
              >
              <label class="text-xs font-medium text-gray-600"
                >Status <select
                  bind:value={editCommitmentDraft.status}
                  class="mt-1 w-full rounded border border-gray-200 px-2 py-1.5 text-sm"
                  ><option value="open">open</option><option value="blocked"
                    >blocked</option
                  ><option value="done">done</option><option value="canceled"
                    >canceled</option
                  ></select
                ></label
              >
              <div class="self-end text-xs text-gray-500">
                {#if statusRequirementText(editCommitmentDraft.status)}<p
                    class="text-amber-600"
                  >
                    {statusRequirementText(editCommitmentDraft.status)}
                  </p>{/if}
              </div>
              <label class="text-xs font-medium text-gray-600 sm:col-span-2"
                >Evidence link <input
                  bind:value={editCommitmentDraft.statusRefInput}
                  class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
                  placeholder="artifact:receipt-123 or event:decision-456"
                  type="text"
                /></label
              >
              <label class="text-xs font-medium text-gray-600 sm:col-span-2"
                >Completion criteria (one per line) <textarea
                  bind:value={editCommitmentDraft.definitionOfDoneInput}
                  class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
                  rows="2"
                ></textarea></label
              >
              <label class="text-xs font-medium text-gray-600 sm:col-span-2"
                >Links (one per line) <textarea
                  bind:value={editCommitmentDraft.linksInput}
                  class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
                  rows="2"
                ></textarea></label
              >
            </div>
            <div class="mt-2 flex gap-2">
              <button
                class="rounded bg-indigo-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
                disabled={savingCommitmentEdit}
                type="submit"
                >{savingCommitmentEdit ? "Saving..." : "Save"}</button
              >
              <button
                class="rounded px-3 py-1.5 text-xs text-gray-500 hover:bg-gray-100"
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
