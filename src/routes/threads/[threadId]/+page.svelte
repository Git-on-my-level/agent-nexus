<script>
  import { onMount } from "svelte";
  import { page } from "$app/stores";

  import {
    actorRegistry,
    lookupActorDisplayName,
    selectedActorId,
  } from "$lib/actorSession";
  import {
    buildCommitmentPatch,
    parseCommitmentListInput,
    serializeCommitmentListInput,
    validateCommitmentStatusTransition,
  } from "$lib/commitmentUtils";
  import {
    formatTimestamp,
    isoToDatetimeLocal,
    datetimeLocalToIso,
  } from "$lib/formatDate";
  import ProvenanceBadge from "$lib/components/ProvenanceBadge.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import { coreClient } from "$lib/coreClient";
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
    getPriorityLabel,
    isLikelyCronExpression,
    validateCadenceSelection,
  } from "$lib/threadFilters";
  import { validateReceiptDraft } from "$lib/receiptUtils";
  import { toTimelineView } from "$lib/timelineUtils";
  import { parseRef } from "$lib/typedRefs";
  import { validateWorkOrderDraft } from "$lib/workOrderUtils";

  let threadId = $derived($page.params.threadId);
  let actorName = $derived((actorId) =>
    lookupActorDisplayName(actorId, $actorRegistry),
  );
  let openCommitmentIds = $derived(
    Array.isArray(snapshot?.open_commitments) ? snapshot.open_commitments : [],
  );
  let commitmentMap = $derived(
    new Map(commitments.map((commitment) => [commitment.id, commitment])),
  );
  let openCommitments = $derived(
    openCommitmentIds.map((commitmentId) => {
      const commitment = commitmentMap.get(commitmentId);
      if (commitment) return commitment;
      return {
        id: commitmentId,
        title: commitmentId,
        owner: "",
        status: "unknown",
        due_at: "",
        links: [],
        definition_of_done: [],
        provenance: { sources: [] },
      };
    }),
  );

  let snapshot = $state(null);
  let snapshotLoading = $state(false);
  let snapshotError = $state("");
  let commitments = $state([]);
  let commitmentsLoading = $state(false);
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
  let timeline = $state([]);
  let timelineLoading = $state(false);
  let timelineError = $state("");
  let workOrderDraft = $state(null);
  let creatingWorkOrder = $state(false);
  let workOrderErrors = $state([]);
  let workOrderNotice = $state("");
  let createdWorkOrder = $state(null);
  let workOrderArtifacts = $state([]);
  let workOrdersError = $state("");
  let workOrderPrefillNotice = $state("");
  let appliedWorkOrderPrefillKey = $state("");
  let receiptDraft = $state(null);
  let creatingReceipt = $state(false);
  let receiptErrors = $state([]);
  let receiptNotice = $state("");
  let createdReceipt = $state(null);
  let messageText = $state("");
  let replyToEventId = $state("");
  let postingMessage = $state(false);
  let postMessageError = $state("");
  let activeTab = $state("overview");
  let editOpen = $state(false);
  let editDraft = $state(null);
  let savingEdit = $state(false);
  let editError = $state("");
  let editNotice = $state("");
  let conflictWarning = $state("");
  let commitmentFormOpen = $state(false);

  onMount(async () => {
    await ensureActorRegistry();
    createCommitmentDraft = blankCreateCommitmentDraft();
    workOrderDraft = blankWorkOrderDraft();
    receiptDraft = blankReceiptDraft();
    await loadThreadDetail(threadId);
  });

  let timelineView = $derived(toTimelineView(timeline, { threadId }));
  let canPost = $derived(Boolean(messageText.trim()) && !postingMessage);
  let workOrderShouldPrefill = $derived(
    $page.url.searchParams.get("compose") === "work-order",
  );
  let workOrderPrefillRefs = $derived(
    $page.url.searchParams
      .getAll("context_ref")
      .map((v) => String(v).trim())
      .filter(Boolean),
  );
  let workOrderPrefillKey = $derived(
    `${threadId}|${$page.url.searchParams.toString()}`,
  );

  $effect(() => {
    if (workOrderShouldPrefill) activeTab = "work";
  });
  $effect(() => {
    if (
      workOrderDraft &&
      workOrderShouldPrefill &&
      workOrderPrefillKey !== appliedWorkOrderPrefillKey
    ) {
      const existingRefs = parseListInput(
        workOrderDraft.contextRefsInput ?? "",
      );
      const mergedRefs = Array.from(
        new Set([
          `thread:${threadId}`,
          ...workOrderPrefillRefs,
          ...existingRefs,
        ]),
      );
      workOrderDraft = {
        ...workOrderDraft,
        contextRefsInput: mergedRefs.join("\n"),
      };
      workOrderPrefillNotice = "Composer prefilled from review context.";
      appliedWorkOrderPrefillKey = workOrderPrefillKey;
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
  function blankWorkOrderDraft() {
    return {
      objective: "",
      constraintsInput: "",
      contextRefsInput: `thread:${threadId}`,
      acceptanceCriteriaInput: "",
      definitionOfDoneInput: "",
    };
  }
  function generateWorkOrderId() {
    return `artifact-work-order-${Math.random().toString(36).slice(2, 10)}`;
  }
  function generateReceiptId() {
    return `artifact-receipt-${Math.random().toString(36).slice(2, 10)}`;
  }
  function blankReceiptDraft() {
    return {
      workOrderId: workOrderArtifacts[0]?.id ?? "",
      outputsInput: "",
      verificationEvidenceInput: "",
      changesSummary: "",
      knownGapsInput: "",
    };
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
    editNotice = "";
    conflictWarning = "";
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

  function normalizeKeyArtifactRef(rawValue) {
    const normalized = String(rawValue ?? "").trim();
    if (!normalized) return "";
    const parsed = parseRef(normalized);
    if (parsed.prefix && parsed.value) return normalized;
    return `artifact:${normalized}`;
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

  async function loadOpenCommitments(commitmentIds = []) {
    commitmentsLoading = true;
    if (!Array.isArray(commitmentIds) || commitmentIds.length === 0) {
      commitments = [];
      commitmentsLoading = false;
      return;
    }
    try {
      const loaded = await Promise.all(
        commitmentIds.map(async (id) => {
          try {
            return (await coreClient.getCommitment(id)).commitment ?? null;
          } catch {
            return null;
          }
        }),
      );
      commitments = loaded.filter(Boolean);
    } catch (error) {
      commitments = [];
      void error;
    } finally {
      commitmentsLoading = false;
    }
  }

  async function loadWorkOrders(targetThreadId) {
    workOrdersError = "";
    try {
      const response = await coreClient.listArtifacts({
        kind: "work_order",
        thread_id: targetThreadId,
      });
      workOrderArtifacts = response.artifacts ?? [];
      if (receiptDraft && !receiptDraft.workOrderId && workOrderArtifacts[0])
        receiptDraft = {
          ...receiptDraft,
          workOrderId: workOrderArtifacts[0].id,
        };
    } catch (error) {
      workOrdersError = `Failed to load work orders: ${error instanceof Error ? error.message : String(error)}`;
      workOrderArtifacts = [];
    }
  }

  async function saveEdit() {
    if (!snapshot || !editDraft) return;
    savingEdit = true;
    editError = "";
    editNotice = "";
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
      const response = await coreClient.updateThread(threadId, {
        patch,
        if_updated_at: snapshot.updated_at,
      });
      snapshot = response.thread ?? snapshot;
      editOpen = false;
      editDraft = null;
      editNotice = "Changes saved.";
      conflictWarning = "";
    } catch (error) {
      if (error?.status === 409) {
        conflictWarning =
          "Thread was updated elsewhere. Reloaded — reapply your changes.";
        editOpen = false;
        editDraft = null;
        await loadSnapshot(threadId);
      } else {
        editError = `Failed to save: ${error instanceof Error ? error.message : String(error)}`;
      }
    } finally {
      savingEdit = false;
    }
  }

  async function ensureActorRegistry() {
    if ($actorRegistry.length > 0) return;
    try {
      actorRegistry.set((await coreClient.listActors()).actors ?? []);
    } catch (error) {
      void error;
    }
  }
  async function loadThreadDetail(id) {
    const s = await loadSnapshot(id);
    await Promise.all([
      loadTimeline(id),
      loadOpenCommitments(s?.open_commitments ?? []),
      loadWorkOrders(id),
      ensureActorRegistry(),
    ]);
  }
  async function loadSnapshot(id) {
    snapshotLoading = true;
    snapshotError = "";
    try {
      snapshot = (await coreClient.getThread(id)).thread ?? null;
      return snapshot;
    } catch (e) {
      snapshotError = `Failed to load thread: ${e instanceof Error ? e.message : String(e)}`;
      snapshot = null;
      return null;
    } finally {
      snapshotLoading = false;
    }
  }
  async function reloadSnapshotAndCommitments() {
    const s = await loadSnapshot(threadId);
    await loadOpenCommitments(s?.open_commitments ?? []);
  }

  async function createCommitment() {
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
      await coreClient.createCommitment({
        commitment: {
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
        },
      });
      createCommitmentDraft = blankCreateCommitmentDraft();
      createCommitmentNotice = "Commitment created.";
      commitmentFormOpen = false;
      await reloadSnapshotAndCommitments();
    } catch (error) {
      createCommitmentError = `Failed to create commitment: ${error instanceof Error ? error.message : String(error)}`;
    } finally {
      creatingCommitment = false;
    }
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

  async function saveCommitmentEdit(commitmentId) {
    const original = commitmentMap.get(commitmentId);
    if (!original || !editCommitmentDraft) return;
    savingCommitmentEdit = true;
    editCommitmentError = "";
    editCommitmentNotice = "";
    commitmentConflictWarning = "";
    try {
      const draftSnapshot = {
        title: editCommitmentDraft.title.trim(),
        owner: editCommitmentDraft.owner.trim(),
        due_at: datetimeLocalToIso(editCommitmentDraft.due_at.trim()),
        status: editCommitmentDraft.status,
        definition_of_done: parseCommitmentListInput(
          editCommitmentDraft.definitionOfDoneInput,
        ),
        links: parseCommitmentListInput(editCommitmentDraft.linksInput),
      };
      const patch = buildCommitmentPatch(original, draftSnapshot);
      if (Object.keys(patch).length === 0) {
        editCommitmentNotice = "No changes.";
        return;
      }
      const refs = [];
      if (Object.prototype.hasOwnProperty.call(patch, "status")) {
        const v = validateCommitmentStatusTransition(
          patch.status,
          editCommitmentDraft.statusRefInput,
        );
        if (!v.valid) {
          editCommitmentError = v.error;
          return;
        }
        const ref = String(editCommitmentDraft.statusRefInput ?? "").trim();
        if (ref) refs.push(ref);
      }
      const payload = { patch, if_updated_at: original.updated_at };
      if (refs.length > 0) payload.refs = refs;
      await coreClient.updateCommitment(commitmentId, payload);
      editCommitmentNotice = "Commitment updated.";
      cancelCommitmentEdit();
      commitmentConflictWarning = "";
      await reloadSnapshotAndCommitments();
    } catch (error) {
      if (error?.status === 409) {
        commitmentConflictWarning =
          "Updated elsewhere. Reloaded — reapply changes.";
        cancelCommitmentEdit();
        await reloadSnapshotAndCommitments();
      } else {
        editCommitmentError = `Failed to update: ${error instanceof Error ? error.message : String(error)}`;
      }
    } finally {
      savingCommitmentEdit = false;
    }
  }

  async function loadTimeline(id) {
    timelineLoading = true;
    timelineError = "";
    try {
      timeline = (await coreClient.listThreadTimeline(id)).events ?? [];
    } catch (e) {
      timelineError = `Failed to load timeline: ${e instanceof Error ? e.message : String(e)}`;
      timeline = [];
    } finally {
      timelineLoading = false;
    }
  }
  function setReplyTarget(eventId) {
    replyToEventId = eventId;
  }
  function clearReplyTarget() {
    replyToEventId = "";
  }

  async function postMessage() {
    if (!messageText.trim()) {
      postMessageError = "Message text is required.";
      return;
    }
    postingMessage = true;
    postMessageError = "";
    try {
      const refs = [`thread:${threadId}`];
      if (replyToEventId) refs.push(`event:${replyToEventId}`);
      await coreClient.createEvent({
        event: {
          type: "message_posted",
          thread_id: threadId,
          refs,
          summary: `Message: ${messageText.trim().slice(0, 100)}`,
          payload: { text: messageText.trim() },
          provenance: { sources: ["actor_statement:ui"] },
        },
      });
      messageText = "";
      replyToEventId = "";
      await loadTimeline(threadId);
    } catch (error) {
      postMessageError = `Failed to post: ${error instanceof Error ? error.message : String(error)}`;
    } finally {
      postingMessage = false;
    }
  }

  async function submitWorkOrder() {
    if (!workOrderDraft || !snapshot) return;
    creatingWorkOrder = true;
    workOrderErrors = [];
    workOrderNotice = "";
    const v = validateWorkOrderDraft(workOrderDraft, { threadId });
    if (!v.valid) {
      workOrderErrors = v.errors;
      creatingWorkOrder = false;
      return;
    }
    const workOrderId = generateWorkOrderId();
    try {
      const response = await coreClient.createWorkOrder({
        artifact: {
          id: workOrderId,
          kind: "work_order",
          thread_id: threadId,
          summary: v.normalized.objective,
          refs: [`thread:${threadId}`],
        },
        packet: {
          work_order_id: workOrderId,
          thread_id: threadId,
          ...v.normalized,
        },
      });
      createdWorkOrder = response.artifact ?? null;
      workOrderNotice = "Work order created.";
      workOrderDraft = blankWorkOrderDraft();
      await Promise.all([loadTimeline(threadId), loadWorkOrders(threadId)]);
    } catch (error) {
      workOrderErrors = [
        `Failed to create work order: ${error instanceof Error ? error.message : String(error)}`,
      ];
    } finally {
      creatingWorkOrder = false;
    }
  }

  async function submitReceipt() {
    if (!receiptDraft || !snapshot) return;
    creatingReceipt = true;
    receiptErrors = [];
    receiptNotice = "";
    const v = validateReceiptDraft(receiptDraft, { threadId });
    if (!v.valid) {
      receiptErrors = v.errors;
      creatingReceipt = false;
      return;
    }
    const receiptId = generateReceiptId();
    try {
      const response = await coreClient.createReceipt({
        artifact: {
          id: receiptId,
          kind: "receipt",
          thread_id: threadId,
          summary: v.normalized.changes_summary.slice(0, 120),
          refs: [
            `thread:${threadId}`,
            `artifact:${v.normalized.work_order_id}`,
          ],
        },
        packet: {
          receipt_id: receiptId,
          work_order_id: v.normalized.work_order_id,
          thread_id: threadId,
          ...v.normalized,
        },
      });
      createdReceipt = response.artifact ?? null;
      receiptNotice = "Receipt submitted.";
      receiptDraft = blankReceiptDraft();
      await Promise.all([loadTimeline(threadId), loadWorkOrders(threadId)]);
    } catch (error) {
      receiptErrors = [
        `Failed to submit receipt: ${error instanceof Error ? error.message : String(error)}`,
      ];
    } finally {
      creatingReceipt = false;
    }
  }
</script>

<nav
  class="mb-3 flex items-center gap-1.5 text-sm text-gray-400"
  aria-label="Breadcrumb"
>
  <a class="hover:text-gray-600" href="/threads">Threads</a>
  <span class="text-gray-300">/</span>
  <span class="truncate text-gray-700">{snapshot?.title || threadId}</span>
</nav>

{#if snapshotLoading}
  <p class="text-sm text-gray-400">Loading...</p>
{:else if snapshotError}
  <p class="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">
    {snapshotError}
  </p>
{:else if !snapshot}
  <p class="text-sm text-gray-400">Thread not found.</p>
{:else}
  <div class="flex items-start justify-between gap-4">
    <h1 class="text-lg font-semibold text-gray-900">{snapshot.title}</h1>
    <div class="flex shrink-0 items-center gap-2 text-xs">
      <span class="rounded bg-gray-100 px-2 py-0.5 capitalize text-gray-600"
        >{snapshot.status}</span
      >
      <span class="rounded bg-gray-100 px-2 py-0.5 text-gray-600"
        >{getPriorityLabel(snapshot.priority)}</span
      >
    </div>
  </div>

  <nav
    class="mt-3 flex gap-0 border-b border-gray-200"
    aria-label="Thread sections"
  >
    {#each [["overview", "Overview"], ["work", "Work"], ["timeline", "Timeline"]] as [tabId, tabLabel]}
      <button
        class={`relative px-3 py-2 text-sm font-medium transition-colors ${activeTab === tabId ? "text-gray-900" : "text-gray-400 hover:text-gray-600"}`}
        onclick={() => (activeTab = tabId)}
        type="button"
      >
        {tabLabel}
        {#if activeTab === tabId}
          <span class="absolute inset-x-0 -bottom-px h-0.5 bg-indigo-600"
          ></span>
        {/if}
      </button>
    {/each}
  </nav>

  {#if activeTab === "overview"}
    {#if conflictWarning}
      <p class="mt-3 rounded-md bg-amber-50 px-3 py-2 text-xs text-amber-700">
        {conflictWarning}
      </p>
    {/if}
    {#if editNotice}
      <p
        class="mt-3 rounded-md bg-emerald-50 px-3 py-2 text-xs text-emerald-700"
      >
        {editNotice}
      </p>
    {/if}

    <div class="mt-4 rounded-lg border border-gray-200 bg-white">
      <div
        class="flex items-center justify-between border-b border-gray-100 px-4 py-2.5"
      >
        <h2
          class="text-xs font-semibold uppercase tracking-wider text-gray-400"
        >
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
              <span
                class="rounded bg-gray-100 px-2 py-0.5 text-xs text-gray-600"
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
          void saveEdit();
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

    <!-- Commitments -->
    <div class="mt-4 rounded-lg border border-gray-200 bg-white">
      <div
        class="flex items-center justify-between border-b border-gray-100 px-4 py-2.5"
      >
        <h2
          class="text-xs font-semibold uppercase tracking-wider text-gray-400"
        >
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
            void createCommitment();
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
            type="submit"
            >{creatingCommitment ? "Creating..." : "Create"}</button
          >
        </form>
      {/if}

      {#if commitmentsLoading}
        <p class="px-4 py-3 text-xs text-gray-400">Loading...</p>
      {:else if openCommitments.length === 0}
        <p class="px-4 py-3 text-sm text-gray-400">No open commitments.</p>
      {:else}
        {#each openCommitments as commitment, i}
          <div
            class="border-b border-gray-100 px-4 py-3 {i ===
            openCommitments.length - 1
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
                    {item}
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
                  void saveCommitmentEdit(commitment.id);
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
                      ><option value="done">done</option><option
                        value="canceled">canceled</option
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
  {/if}

  {#if activeTab === "work"}
    <!-- Work Order -->
    <div class="mt-4 rounded-lg border border-gray-200 bg-white p-4">
      <h2 class="text-xs font-semibold uppercase tracking-wider text-gray-400">
        New Work Order
      </h2>
      <p class="mt-0.5 text-xs text-gray-500">
        Create a new work order for this thread.
      </p>
      {#if workOrderPrefillNotice}<p
          class="mt-2 rounded bg-indigo-50 px-3 py-1.5 text-xs text-indigo-700"
        >
          {workOrderPrefillNotice}
        </p>{/if}
      {#if workOrderErrors.length > 0}<ul
          class="mt-2 list-inside list-disc rounded bg-red-50 px-3 py-1.5 text-xs text-red-700"
        >
          {#each workOrderErrors as e}<li>{e}</li>{/each}
        </ul>{/if}
      {#if workOrderNotice}<p
          class="mt-2 rounded bg-emerald-50 px-3 py-1.5 text-xs text-emerald-700"
        >
          {workOrderNotice}
        </p>{/if}
      {#if workOrderDraft}
        <form
          class="mt-3 grid gap-3"
          onsubmit={(event) => {
            event.preventDefault();
            void submitWorkOrder();
          }}
        >
          <label class="text-xs font-medium text-gray-600"
            >Work order objective <textarea
              bind:value={workOrderDraft.objective}
              class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
              rows="2"
            ></textarea></label
          >
          <label class="text-xs font-medium text-gray-600"
            >Constraints (one per line) <textarea
              bind:value={workOrderDraft.constraintsInput}
              class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
              rows="2"
            ></textarea></label
          >
          <label class="text-xs font-medium text-gray-600"
            >Context references (one per line) <textarea
              bind:value={workOrderDraft.contextRefsInput}
              class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
              rows="2"
            ></textarea></label
          >
          <label class="text-xs font-medium text-gray-600"
            >Acceptance criteria (one per line) <textarea
              bind:value={workOrderDraft.acceptanceCriteriaInput}
              class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
              rows="2"
            ></textarea></label
          >
          <label class="text-xs font-medium text-gray-600"
            >Definition of done (one per line) <textarea
              bind:value={workOrderDraft.definitionOfDoneInput}
              class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
              rows="2"
            ></textarea></label
          >
          <button
            class="w-fit rounded bg-indigo-600 px-4 py-1.5 text-xs font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
            disabled={creatingWorkOrder}
            type="submit"
            >{creatingWorkOrder ? "Creating..." : "Create work order"}</button
          >
        </form>
      {/if}
      {#if createdWorkOrder}
        <div class="mt-3 rounded-md border border-gray-100 bg-gray-50 p-3">
          <p class="text-xs text-gray-500">
            Created: <a
              class="text-indigo-600 underline"
              href={`/artifacts/${createdWorkOrder.id}`}
              >{createdWorkOrder.summary || createdWorkOrder.id}</a
            >
          </p>
        </div>
      {/if}
    </div>

    <!-- Receipt -->
    <div class="mt-4 rounded-lg border border-gray-200 bg-white p-4">
      <h2 class="text-xs font-semibold uppercase tracking-wider text-gray-400">
        Add Receipt
      </h2>
      <p class="mt-0.5 text-xs text-gray-500">
        Submit a receipt tied to an existing work order.
      </p>
      {#if workOrdersError}<p
          class="mt-2 rounded bg-amber-50 px-3 py-1.5 text-xs text-amber-700"
        >
          {workOrdersError}
        </p>{/if}
      {#if receiptErrors.length > 0}<ul
          class="mt-2 list-inside list-disc rounded bg-red-50 px-3 py-1.5 text-xs text-red-700"
        >
          {#each receiptErrors as e}<li>{e}</li>{/each}
        </ul>{/if}
      {#if receiptNotice}<p
          class="mt-2 rounded bg-emerald-50 px-3 py-1.5 text-xs text-emerald-700"
        >
          {receiptNotice}
        </p>{/if}
      {#if receiptDraft}
        <form
          class="mt-3 grid gap-3"
          onsubmit={(event) => {
            event.preventDefault();
            void submitReceipt();
          }}
        >
          <label class="text-xs font-medium text-gray-600"
            >Work order <select
              bind:value={receiptDraft.workOrderId}
              class="mt-1 w-full rounded border border-gray-200 px-2 py-1.5 text-sm"
              required
              ><option value="">Select work order</option
              >{#each workOrderArtifacts as wo}<option value={wo.id}
                  >{wo.summary || wo.id}</option
                >{/each}</select
            ></label
          >
          <label class="text-xs font-medium text-gray-600"
            >Outputs (one per line) <textarea
              bind:value={receiptDraft.outputsInput}
              class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
              rows="2"
            ></textarea></label
          >
          <label class="text-xs font-medium text-gray-600"
            >Verification evidence (one per line) <textarea
              bind:value={receiptDraft.verificationEvidenceInput}
              class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
              rows="2"
            ></textarea></label
          >
          <label class="text-xs font-medium text-gray-600"
            >Changes summary <textarea
              bind:value={receiptDraft.changesSummary}
              class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
              rows="2"
            ></textarea></label
          >
          <label class="text-xs font-medium text-gray-600"
            >Known gaps (one per line) <textarea
              bind:value={receiptDraft.knownGapsInput}
              class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
              rows="2"
            ></textarea></label
          >
          <button
            class="w-fit rounded bg-indigo-600 px-4 py-1.5 text-xs font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
            disabled={creatingReceipt || workOrderArtifacts.length === 0}
            type="submit"
            >{creatingReceipt ? "Submitting..." : "Submit receipt"}</button
          >
        </form>
      {/if}
      {#if createdReceipt}
        <div class="mt-3 rounded-md border border-gray-100 bg-gray-50 p-3">
          <p class="text-xs text-gray-500">
            Submitted: <a
              class="text-indigo-600 underline"
              href={`/artifacts/${createdReceipt.id}`}
              >{createdReceipt.summary || createdReceipt.id}</a
            >
          </p>
        </div>
      {/if}
    </div>
  {/if}

  {#if activeTab === "timeline"}
    <!-- Post Message -->
    <div class="mt-4 rounded-lg border border-gray-200 bg-white p-4">
      {#if postMessageError}<p
          class="mb-3 rounded bg-red-50 px-3 py-1.5 text-xs text-red-700"
        >
          {postMessageError}
        </p>{/if}
      <textarea
        bind:value={messageText}
        class="w-full rounded border border-gray-200 px-3 py-2 text-sm"
        id="message-text"
        placeholder="Write a message..."
        rows="2"
      ></textarea>
      <div class="mt-2 flex items-center justify-between gap-2">
        <div class="flex items-center gap-2 text-xs text-gray-400">
          {#if replyToEventId}
            <span>Replying to event</span>
            <button
              class="text-indigo-600 hover:text-indigo-800"
              onclick={clearReplyTarget}
              type="button">Clear</button
            >
          {/if}
        </div>
        <button
          class="rounded bg-indigo-600 px-4 py-1.5 text-xs font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
          disabled={!canPost}
          onclick={postMessage}
          type="button"
        >
          {postingMessage ? "Posting..." : "Post"}
        </button>
      </div>
    </div>

    <!-- Timeline -->
    <div class="mt-4">
      <h2
        class="mb-3 text-xs font-semibold uppercase tracking-wider text-gray-400"
      >
        Timeline
      </h2>
      {#if timelineLoading}
        <p class="text-sm text-gray-400">Loading timeline...</p>
      {:else if timelineError}
        <p class="rounded bg-red-50 px-3 py-2 text-sm text-red-700">
          {timelineError}
        </p>
      {:else if timelineView.length === 0}
        <p class="text-sm text-gray-400">No events yet.</p>
      {:else}
        <div class="space-y-1">
          {#each timelineView as event}
            <div
              class="group rounded-md border border-gray-200 bg-white px-4 py-2.5"
              id={`event-${event.id}`}
            >
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0 flex-1">
                  <p class="text-sm text-gray-900">{event.summary}</p>
                  <p class="mt-0.5 text-xs text-gray-400">
                    {actorName(event.actor_id)} · {event.typeLabel} · {formatTimestamp(
                      event.ts,
                    ) || "—"}
                  </p>
                </div>
                <button
                  class="shrink-0 rounded px-2 py-0.5 text-xs text-gray-400 opacity-0 transition-opacity hover:bg-gray-100 hover:text-gray-600 group-hover:opacity-100"
                  onclick={() => setReplyTarget(event.id)}
                  type="button">Reply</button
                >
              </div>

              {#if event.changedFields.length > 0}
                <div class="mt-1.5 flex flex-wrap gap-1 text-xs">
                  {#each event.changedFields as field}
                    <span
                      class="rounded bg-gray-100 px-1.5 py-0.5 text-gray-500"
                      >{field}</span
                    >
                  {/each}
                </div>
              {/if}

              {#if event.refs.length > 0}
                <div class="mt-1.5 flex flex-wrap gap-1.5 text-xs">
                  {#each event.refs as refValue}<RefLink
                      {refValue}
                      {threadId}
                    />{/each}
                </div>
              {/if}

              {#if !event.isKnownType}
                <details class="mt-1.5">
                  <summary class="cursor-pointer text-xs text-gray-400"
                    >Details</summary
                  >
                  <pre
                    class="mt-1 overflow-auto rounded bg-gray-50 p-2 text-[11px] text-gray-600">{JSON.stringify(
                      event.payload ?? {},
                      null,
                      2,
                    )}</pre>
                </details>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
{/if}
