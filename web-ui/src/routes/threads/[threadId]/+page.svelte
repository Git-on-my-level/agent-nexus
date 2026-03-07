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
  import GuidedTypedRefsInput from "$lib/components/GuidedTypedRefsInput.svelte";
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
  let workOrderFieldErrors = $state({});
  let workOrderNotice = $state("");
  let createdWorkOrder = $state(null);
  let workOrderArtifacts = $state([]);
  let workOrdersError = $state("");
  let workOrderPrefillNotice = $state("");
  let appliedWorkOrderPrefillKey = $state("");
  let receiptDraft = $state(null);
  let creatingReceipt = $state(false);
  let receiptErrors = $state([]);
  let receiptFieldErrors = $state({});
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
  let commitmentFormOpen = $state(true);

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
  let recentTimeline = $derived(timelineView.slice(0, 4));
  let urgentCommitments = $derived(
    openCommitments.filter(
      (commitment) =>
        commitment.status === "blocked" || commitment.status === "open",
    ),
  );
  let staleCheckIn = $derived(
    Boolean(snapshot?.next_check_in_at) &&
      Date.parse(String(snapshot.next_check_in_at)) < Date.now(),
  );
  let selectedReceiptWorkOrder = $derived(
    workOrderArtifacts.find(
      (artifact) => artifact.id === receiptDraft?.workOrderId,
    ) ?? null,
  );
  let workOrderContextSuggestions = $derived(
    buildRefSuggestions([
      { value: `thread:${threadId}`, label: "Thread context" },
      ...(snapshot?.key_artifacts ?? []).map((artifactId) => ({
        value: normalizeKeyArtifactRef(artifactId),
        label: `Key artifact · ${artifactId}`,
      })),
      ...timelineView.slice(0, 8).map((event) => ({
        value: `event:${event.id}`,
        label: `Event · ${event.typeLabel}`,
      })),
    ]),
  );
  let receiptOutputSuggestions = $derived(
    buildRefSuggestions([
      { value: `thread:${threadId}`, label: "Thread context" },
      selectedReceiptWorkOrder
        ? {
            value: `artifact:${selectedReceiptWorkOrder.id}`,
            label: `Selected work order · ${selectedReceiptWorkOrder.id}`,
          }
        : null,
      ...(snapshot?.key_artifacts ?? []).map((artifactId) => ({
        value: normalizeKeyArtifactRef(artifactId),
        label: `Key artifact · ${artifactId}`,
      })),
    ]),
  );
  let receiptEvidenceSuggestions = $derived(
    buildRefSuggestions([
      ...(selectedReceiptWorkOrder
        ? [
            {
              value: `artifact:${selectedReceiptWorkOrder.id}`,
              label: `Selected work order · ${selectedReceiptWorkOrder.id}`,
            },
          ]
        : []),
      ...timelineView.slice(0, 8).map((event) => ({
        value: `event:${event.id}`,
        label: `Event · ${event.typeLabel}`,
      })),
      ...(snapshot?.key_artifacts ?? []).map((artifactId) => ({
        value: normalizeKeyArtifactRef(artifactId),
        label: `Key artifact · ${artifactId}`,
      })),
    ]),
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

  function buildRefSuggestions(candidates = []) {
    const seen = new Set();
    const suggestions = [];

    candidates.forEach((candidate) => {
      const value = String(candidate?.value ?? "").trim();
      if (!value || seen.has(value)) return;
      const parsed = parseRef(value);
      if (!parsed.prefix || !parsed.value) return;
      seen.add(value);
      suggestions.push({
        value,
        label: String(candidate?.label ?? "").trim() || value,
      });
    });

    return suggestions;
  }

  function firstFieldError(fieldErrors, fieldName) {
    const candidates = fieldErrors?.[fieldName];
    if (!Array.isArray(candidates) || candidates.length === 0) {
      return "";
    }
    return candidates[0];
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

  function threadStatusClass(status) {
    if (status === "active") return "bg-emerald-100 text-emerald-700";
    if (status === "paused") return "bg-amber-100 text-amber-700";
    return "bg-slate-100 text-slate-600";
  }

  function threadHealthLabel() {
    if (!snapshot) return "Unknown";
    if (snapshot.priority === "p0") return "Urgent";
    if (snapshot.status === "paused") return "Blocked";
    if (staleCheckIn) return "Needs check-in";
    return "Healthy";
  }

  function statusRequirementText(status) {
    if (status === "done")
      return "Evidence required: link to a receipt artifact or decision event.";
    if (status === "canceled")
      return "Evidence required: link to a decision event.";
    return "";
  }

  function parseCommitmentDueAtInput(rawValue) {
    const inputValue = String(rawValue ?? "").trim();
    const dueAt = datetimeLocalToIso(inputValue);

    if (!inputValue || !dueAt) {
      return {
        valid: false,
        dueAt: "",
        error:
          "Due at must be a valid timestamp, for example 2026-03-12T00:00:00.000Z.",
      };
    }

    return { valid: true, dueAt, error: "" };
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
      editNotice = "Snapshot updated.";
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
      const dueAtResult = parseCommitmentDueAtInput(
        createCommitmentDraft.due_at,
      );
      if (!title || !owner || !createCommitmentDraft.due_at.trim()) {
        createCommitmentError = "Title, owner, and due date are required.";
        return;
      }
      if (!dueAtResult.valid) {
        createCommitmentError = dueAtResult.error;
        return;
      }
      await coreClient.createCommitment({
        commitment: {
          thread_id: threadId,
          title,
          owner,
          due_at: dueAtResult.dueAt,
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
    const draft = { ...editCommitmentDraft };
    const isStillEditingTarget = () => editingCommitmentId === commitmentId;
    savingCommitmentEdit = true;
    editCommitmentError = "";
    editCommitmentNotice = "";
    commitmentConflictWarning = "";
    try {
      const dueAtResult = parseCommitmentDueAtInput(draft.due_at);
      if (!dueAtResult.valid) {
        if (isStillEditingTarget()) editCommitmentError = dueAtResult.error;
        return;
      }
      const draftSnapshot = {
        title: draft.title.trim(),
        owner: draft.owner.trim(),
        due_at: dueAtResult.dueAt,
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
      await coreClient.updateCommitment(commitmentId, payload);
      if (isStillEditingTarget()) {
        editCommitmentNotice = "Commitment updated.";
        cancelCommitmentEdit();
        commitmentConflictWarning = "";
      }
      await reloadSnapshotAndCommitments();
    } catch (error) {
      if (error?.status === 409) {
        commitmentConflictWarning =
          "Commitment was updated elsewhere. Reloaded latest version.";
        if (isStillEditingTarget()) cancelCommitmentEdit();
        await reloadSnapshotAndCommitments();
      } else {
        if (isStillEditingTarget()) {
          editCommitmentError = `Failed to update: ${error instanceof Error ? error.message : String(error)}`;
        }
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
    workOrderFieldErrors = {};
    workOrderNotice = "";
    const v = validateWorkOrderDraft(workOrderDraft, { threadId });
    if (!v.valid) {
      workOrderErrors = v.errors;
      workOrderFieldErrors = v.fieldErrors ?? {};
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
      workOrderFieldErrors = {};
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
    receiptFieldErrors = {};
    receiptNotice = "";
    const v = validateReceiptDraft(receiptDraft, { threadId });
    if (!v.valid) {
      receiptErrors = v.errors;
      receiptFieldErrors = v.fieldErrors ?? {};
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
      receiptFieldErrors = {};
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
  class="mb-4 flex items-center gap-1.5 text-sm text-gray-400"
  aria-label="Breadcrumb"
>
  <a class="transition-colors hover:text-gray-600" href="/threads">Threads</a>
  <svg
    class="h-3 w-3 text-gray-300"
    fill="none"
    viewBox="0 0 24 24"
    stroke="currentColor"
    stroke-width="2"
  >
    <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
  </svg>
  <span class="truncate text-gray-600">{snapshot?.title || threadId}</span>
</nav>

{#if snapshotLoading}
  <div
    class="mt-12 flex items-center justify-center gap-2 text-sm text-gray-400"
  >
    <svg class="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
      <circle
        class="opacity-25"
        cx="12"
        cy="12"
        r="10"
        stroke="currentColor"
        stroke-width="4"
      ></circle>
      <path
        class="opacity-75"
        fill="currentColor"
        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
      ></path>
    </svg>
    Loading...
  </div>
{:else if snapshotError}
  <div
    class="flex items-start gap-2 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700"
  >
    <svg
      class="mt-0.5 h-4 w-4 shrink-0 text-red-400"
      fill="none"
      viewBox="0 0 24 24"
      stroke="currentColor"
      stroke-width="2"
    >
      <path
        stroke-linecap="round"
        stroke-linejoin="round"
        d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L4.082 16.5c-.77.833.192 2.5 1.732 2.5z"
      />
    </svg>
    {snapshotError}
  </div>
{:else if !snapshot}
  <div class="mt-8 text-center">
    <p class="text-sm text-gray-400">Thread not found.</p>
  </div>
{:else}
  <header
    class="rounded-2xl border border-teal-100/80 bg-gradient-to-br from-teal-50 via-white to-sky-50 p-5 shadow-[0_12px_24px_rgba(2,132,199,0.08)]"
  >
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h1
          class="text-sm font-semibold uppercase tracking-[0.1em] text-teal-700"
        >
          Thread Detail: {threadId}
        </h1>
        <p class="mt-1 text-2xl font-semibold text-slate-900">
          {snapshot.title}
        </p>
        <p class="mt-1 text-sm text-slate-600">
          {snapshot.current_summary || "No summary has been provided yet."}
        </p>
      </div>
      <div class="flex shrink-0 items-center gap-2">
        <span
          class={`rounded-md px-2 py-0.5 text-xs font-medium capitalize ${threadStatusClass(snapshot.status)}`}
          >{snapshot.status}</span
        >
        <span
          class="rounded-md bg-slate-100 px-2 py-0.5 text-xs font-medium text-slate-700"
          >{getPriorityLabel(snapshot.priority)}</span
        >
      </div>
    </div>

    <div class="mt-4 grid gap-2 sm:grid-cols-4">
      <div class="rounded-lg border border-slate-200 bg-white px-3 py-2">
        <p class="text-[11px] uppercase tracking-[0.06em] text-slate-500">
          Health
        </p>
        <p class="mt-1 text-sm font-semibold text-slate-900">
          {threadHealthLabel()}
        </p>
      </div>
      <div class="rounded-lg border border-slate-200 bg-white px-3 py-2">
        <p class="text-[11px] uppercase tracking-[0.06em] text-slate-500">
          Next check-in
        </p>
        <p class="mt-1 text-sm font-semibold text-slate-900">
          {snapshot.next_check_in_at
            ? formatTimestamp(snapshot.next_check_in_at)
            : "Not scheduled"}
        </p>
      </div>
      <div class="rounded-lg border border-slate-200 bg-white px-3 py-2">
        <p class="text-[11px] uppercase tracking-[0.06em] text-slate-500">
          Open commitments
        </p>
        <p class="mt-1 text-sm font-semibold text-slate-900">
          {openCommitments.length}
        </p>
      </div>
      <div class="rounded-lg border border-slate-200 bg-white px-3 py-2">
        <p class="text-[11px] uppercase tracking-[0.06em] text-slate-500">
          Needs attention
        </p>
        <p class="mt-1 text-sm font-semibold text-slate-900">
          {urgentCommitments.length}
        </p>
      </div>
    </div>

    {#if (snapshot.next_actions ?? []).length > 0}
      <div class="mt-4 rounded-xl border border-slate-200 bg-white p-3">
        <p
          class="text-xs font-semibold uppercase tracking-[0.08em] text-slate-500"
        >
          What needs to happen next
        </p>
        <ul class="mt-2 grid gap-1.5 text-sm text-slate-700">
          {#each (snapshot.next_actions ?? []).slice(0, 3) as action}
            <li class="flex items-start gap-2">
              <span class="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-teal-400"
              ></span>
              <span>{action}</span>
            </li>
          {/each}
        </ul>
      </div>
    {/if}
  </header>

  <nav
    class="mt-4 flex gap-0 border-b border-gray-200"
    aria-label="Thread sections"
  >
    {#each [["overview", "Overview"], ["work", "Work"], ["timeline", "Timeline"]] as [tabId, tabLabel]}
      <button
        class={`relative px-4 py-2.5 text-[13px] font-medium transition-colors ${activeTab === tabId ? "text-gray-900" : "text-gray-400 hover:text-gray-600"}`}
        onclick={() => (activeTab = tabId)}
        type="button"
      >
        {tabLabel}
        {#if activeTab === tabId}
          <span
            class="absolute inset-x-0 -bottom-px h-0.5 rounded-full bg-indigo-600"
          ></span>
        {/if}
      </button>
    {/each}
  </nav>

  {#if activeTab === "overview"}
    {#if conflictWarning}
      <div
        class="mt-3 flex items-center gap-2 rounded-lg bg-amber-50 px-4 py-3 text-xs text-amber-700"
      >
        <svg
          class="h-4 w-4 shrink-0 text-amber-400"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="2"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L4.082 16.5c-.77.833.192 2.5 1.732 2.5z"
          />
        </svg>
        {conflictWarning}
      </div>
    {/if}
    {#if editNotice}
      <div
        class="mt-3 flex items-center gap-2 rounded-lg bg-emerald-50 px-4 py-3 text-xs text-emerald-700"
      >
        <svg
          class="h-4 w-4 shrink-0 text-emerald-400"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="2"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"
          />
        </svg>
        {editNotice}
      </div>
    {/if}

    <div class="mt-4 grid gap-4 xl:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
      <div class="space-y-3">
        <div class="rounded-xl border border-gray-200/80 bg-white shadow-sm">
          <div
            class="flex items-center justify-between border-b border-gray-100 px-5 py-3"
          >
            <h2 class="text-sm font-medium text-gray-900">Snapshot</h2>
            <button
              class="rounded-md px-2.5 py-1 text-xs font-medium text-indigo-600 transition-colors hover:bg-indigo-50"
              onclick={editOpen ? cancelEdit : beginEdit}
              type="button"
            >
              {editOpen ? "Cancel snapshot edit" : "Edit snapshot"}
            </button>
          </div>

          <div
            class="grid grid-cols-2 gap-x-6 gap-y-3 px-5 py-4 text-sm sm:grid-cols-4"
          >
            <div>
              <p class="text-xs font-medium text-gray-400">Type</p>
              <p class="mt-0.5 capitalize text-gray-900">{snapshot.type}</p>
            </div>
            <div>
              <p class="text-xs font-medium text-gray-400">Cadence</p>
              <p class="mt-0.5 text-gray-900">
                {formatCadenceLabel(snapshot.cadence)}
              </p>
            </div>
            <div>
              <p class="text-xs font-medium text-gray-400">Next check-in</p>
              <p class="mt-0.5 text-gray-900">
                {snapshot.next_check_in_at
                  ? formatTimestamp(snapshot.next_check_in_at)
                  : "—"}
              </p>
            </div>
            <div>
              <p class="text-xs font-medium text-gray-400">Updated</p>
              <p class="mt-0.5 text-gray-900">
                {formatTimestamp(snapshot.updated_at) || "—"}
              </p>
              <p class="text-xs text-gray-400">
                by {actorName(snapshot.updated_by)}
              </p>
            </div>
          </div>

          {#if (snapshot.tags ?? []).length > 0}
            <div class="border-t border-gray-100 px-5 py-3">
              <div class="flex flex-wrap gap-1.5">
                {#each snapshot.tags ?? [] as tag}
                  <span
                    class="rounded-md bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-600"
                    >{tag}</span
                  >
                {/each}
              </div>
            </div>
          {/if}

          <div class="border-t border-gray-100 px-5 py-4">
            <p class="text-xs font-medium text-gray-400">Summary</p>
            <p class="mt-1.5 text-sm leading-relaxed text-gray-800">
              {snapshot.current_summary}
            </p>
          </div>

          {#if (snapshot.next_actions ?? []).length > 0}
            <div class="border-t border-gray-100 px-5 py-4">
              <p class="text-xs font-medium text-gray-400">Next actions</p>
              <ul class="mt-1.5 space-y-1 text-sm text-gray-800">
                {#each snapshot.next_actions ?? [] as action}
                  <li class="flex items-start gap-2">
                    <span
                      class="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-gray-300"
                    ></span>
                    {action}
                  </li>
                {/each}
              </ul>
            </div>
          {/if}

          {#if (snapshot.key_artifacts ?? []).length > 0}
            <div class="border-t border-gray-100 px-5 py-4">
              <p class="text-xs font-medium text-gray-400">Key artifacts</p>
              <div class="mt-1.5 flex flex-wrap gap-2 text-sm">
                {#each snapshot.key_artifacts ?? [] as artifactId}
                  <RefLink
                    refValue={normalizeKeyArtifactRef(artifactId)}
                    {threadId}
                  />
                {/each}
              </div>
            </div>
          {/if}

          <div class="border-t border-gray-100 px-5 py-3">
            <ProvenanceBadge provenance={snapshot.provenance} />
          </div>

          <details class="border-t border-gray-100">
            <summary
              class="cursor-pointer px-5 py-3 text-xs text-gray-400 transition-colors hover:text-gray-600"
              >Raw JSON</summary
            >
            <pre
              class="overflow-auto px-5 pb-4 text-[11px] text-gray-500">{JSON.stringify(
                snapshot,
                null,
                2,
              )}</pre>
          </details>
        </div>

        {#if editOpen && editDraft}
          <form
            class="mt-3 rounded-xl border border-gray-200/80 bg-white p-5 shadow-sm"
            onsubmit={(event) => {
              event.preventDefault();
              void saveEdit();
            }}
          >
            {#if editError}<div
                class="mb-4 rounded-lg bg-red-50 px-4 py-2.5 text-xs text-red-700"
              >
                {editError}
              </div>{/if}
            <div class="grid gap-4 sm:grid-cols-2">
              <label class="text-xs font-medium text-gray-600 sm:col-span-2"
                >Title <input
                  bind:value={editDraft.title}
                  class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm transition-colors focus:bg-white"
                  required
                  type="text"
                /></label
              >
              <label class="text-xs font-medium text-gray-600"
                >Type <select
                  bind:value={editDraft.type}
                  class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-2 text-sm transition-colors focus:bg-white"
                  ><option value="case">case</option><option value="process"
                    >process</option
                  ><option value="relationship">relationship</option><option
                    value="initiative">initiative</option
                  ><option value="incident">incident</option><option
                    value="other">other</option
                  ></select
                ></label
              >
              <label class="text-xs font-medium text-gray-600"
                >Status <select
                  bind:value={editDraft.status}
                  class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-2 text-sm transition-colors focus:bg-white"
                  ><option value="active">active</option><option value="paused"
                    >paused</option
                  ><option value="closed">closed</option></select
                ></label
              >
              <label class="text-xs font-medium text-gray-600"
                >Priority <select
                  bind:value={editDraft.priority}
                  class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-2 text-sm transition-colors focus:bg-white"
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
                  class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-2 text-sm transition-colors focus:bg-white"
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
                    class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm transition-colors focus:bg-white"
                    placeholder="0 9 * * *"
                    type="text"
                  /></label
                >
              {/if}
              <label class="text-xs font-medium text-gray-600"
                >Next check-in <input
                  bind:value={editDraft.next_check_in_at}
                  class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-2 text-sm transition-colors focus:bg-white"
                  type="datetime-local"
                /></label
              >
              <label class="text-xs font-medium text-gray-600"
                >Tags (one per line) <textarea
                  bind:value={editDraft.tagsInput}
                  class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm transition-colors focus:bg-white"
                  rows="2"
                ></textarea></label
              >
              <label class="text-xs font-medium text-gray-600 sm:col-span-2"
                >Summary <textarea
                  bind:value={editDraft.current_summary}
                  class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm transition-colors focus:bg-white"
                  rows="2"
                ></textarea></label
              >
              <label class="text-xs font-medium text-gray-600 sm:col-span-2"
                >Next actions (one per line) <textarea
                  bind:value={editDraft.nextActionsInput}
                  class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm transition-colors focus:bg-white"
                  rows="2"
                ></textarea></label
              >
              <label class="text-xs font-medium text-gray-600 sm:col-span-2"
                >Key artifacts (one per line) <textarea
                  bind:value={editDraft.keyArtifactsInput}
                  class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm transition-colors focus:bg-white"
                  rows="2"
                ></textarea></label
              >
            </div>
            <div class="mt-4 flex gap-2">
              <button
                class="rounded-md bg-indigo-600 px-4 py-2 text-xs font-medium text-white shadow-sm hover:bg-indigo-500 disabled:opacity-50"
                disabled={savingEdit}
                type="submit"
                >{savingEdit
                  ? "Saving snapshot..."
                  : "Save snapshot changes"}</button
              >
              <button
                class="rounded-md px-3 py-2 text-xs font-medium text-gray-500 hover:bg-gray-100"
                onclick={cancelEdit}
                type="button">Cancel</button
              >
            </div>
          </form>
        {/if}
      </div>

      <aside class="space-y-3">
        <section
          class="rounded-xl border border-gray-200/80 bg-white p-4 shadow-sm"
        >
          <div class="flex items-center justify-between gap-2">
            <h2 class="text-sm font-medium text-gray-900">Post update</h2>
            <button
              class="rounded-md px-2 py-1 text-xs text-gray-500 transition-colors hover:bg-gray-100"
              onclick={() => (activeTab = "timeline")}
              type="button"
            >
              Open timeline
            </button>
          </div>
          {#if postMessageError}
            <div
              class="mt-3 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700"
            >
              {postMessageError}
            </div>
          {/if}
          <label
            class="mt-3 block text-xs font-medium text-gray-600"
            for="message-text">Message</label
          >
          <textarea
            aria-label="Message"
            bind:value={messageText}
            class="mt-1.5 w-full rounded-lg border border-gray-200 bg-gray-50 px-3 py-2.5 text-sm transition-colors focus:bg-white"
            id="message-text"
            placeholder="Write a message..."
            rows="2"
          ></textarea>
          <label
            class="mt-2.5 block text-xs font-medium text-gray-600"
            for="reply-to-event">Reply to event (optional)</label
          >
          <select
            aria-label="Reply to event (optional)"
            bind:value={replyToEventId}
            class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-2 text-sm transition-colors focus:bg-white"
            id="reply-to-event"
          >
            <option value="">None</option>
            {#each timelineView as event}
              <option value={event.id}>
                {event.id} · {event.typeLabel} · {formatTimestamp(event.ts)}
              </option>
            {/each}
          </select>
          <div class="mt-3 flex items-center justify-between gap-2">
            <div class="text-xs text-gray-400">
              {#if replyToEventId}
                Replying to {replyToEventId}
              {/if}
            </div>
            <button
              class="rounded-md bg-indigo-600 px-4 py-2 text-xs font-medium text-white shadow-sm transition-colors hover:bg-indigo-500 disabled:opacity-50"
              disabled={!canPost}
              onclick={postMessage}
              type="button"
            >
              {postingMessage ? "Posting..." : "Post message"}
            </button>
          </div>
        </section>

        <section
          class="rounded-xl border border-gray-200/80 bg-white p-4 shadow-sm"
        >
          <div class="flex items-center justify-between gap-2">
            <h2 class="text-sm font-medium text-gray-900">Recent activity</h2>
            <button
              class="rounded-md px-2 py-1 text-xs text-gray-500 transition-colors hover:bg-gray-100"
              onclick={() => (activeTab = "timeline")}
              type="button"
            >
              View all
            </button>
          </div>
          {#if recentTimeline.length === 0}
            <p class="mt-3 text-xs text-gray-500">No events yet.</p>
          {:else}
            <div class="mt-3 space-y-2">
              {#each recentTimeline as event}
                <article
                  class="rounded-lg border border-gray-100 bg-gray-50 px-3 py-2"
                  id={`recent-event-${event.id}`}
                >
                  <p class="text-sm text-gray-900">{event.summary}</p>
                  <p class="mt-0.5 text-xs text-gray-500">
                    {event.typeLabel} · {formatTimestamp(event.ts)}
                  </p>
                  {#if !event.isKnownType}
                    <details class="mt-1">
                      <summary class="cursor-pointer text-xs text-gray-500"
                        >Unknown event details</summary
                      >
                      <pre
                        class="mt-1 overflow-auto rounded-md bg-white p-2 text-[11px] text-gray-600">{JSON.stringify(
                          event.payload ?? {},
                          null,
                          2,
                        )}</pre>
                    </details>
                  {/if}
                </article>
              {/each}
            </div>
          {/if}
        </section>
      </aside>
    </div>

    <!-- Commitments -->
    <section
      class="mt-4 rounded-xl border border-gray-200/80 bg-white shadow-sm"
    >
      <div
        class="flex items-center justify-between border-b border-gray-100 px-5 py-3"
      >
        <h2 class="text-sm font-medium text-gray-900">Commitments</h2>
        <button
          class="inline-flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-medium text-indigo-600 transition-colors hover:bg-indigo-50"
          onclick={() => (commitmentFormOpen = !commitmentFormOpen)}
          type="button"
        >
          {#if !commitmentFormOpen}
            <svg
              class="h-3 w-3"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2.5"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M12 4v16m8-8H4"
              />
            </svg>
          {/if}
          {commitmentFormOpen ? "Hide create form" : "Add commitment"}
        </button>
      </div>

      {#if commitmentConflictWarning}
        <div
          class="border-b border-gray-100 bg-amber-50 px-5 py-2.5 text-xs text-amber-700"
        >
          {commitmentConflictWarning}
        </div>
      {/if}
      {#if createCommitmentNotice}
        <div
          class="border-b border-gray-100 bg-emerald-50 px-5 py-2.5 text-xs text-emerald-700"
        >
          {createCommitmentNotice}
        </div>
      {/if}

      {#if commitmentFormOpen}
        <form
          class="border-b border-gray-100 px-5 py-4"
          onsubmit={(event) => {
            event.preventDefault();
            void createCommitment();
          }}
        >
          {#if createCommitmentError}
            <div
              class="mb-3 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700"
            >
              {createCommitmentError}
            </div>
          {/if}
          <div class="grid gap-3 sm:grid-cols-2">
            <label class="text-xs font-medium text-gray-600 sm:col-span-2"
              >Commitment title <input
                aria-label="Commitment title"
                bind:value={createCommitmentDraft.title}
                class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm transition-colors focus:bg-white"
                placeholder="What needs to be done?"
                required
                type="text"
              /></label
            >
            <label class="text-xs font-medium text-gray-600"
              >Owner <select
                aria-label="Owner"
                bind:value={createCommitmentDraft.owner}
                class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-2 text-sm transition-colors focus:bg-white"
                required
                ><option disabled value="">Select</option
                >{#each $actorRegistry as actor}<option value={actor.id}
                    >{actor.display_name || actor.id}</option
                  >{/each}</select
              ></label
            >
            <label class="text-xs font-medium text-gray-600"
              >Due at (ISO timestamp) <input
                aria-label="Due at (ISO timestamp)"
                bind:value={createCommitmentDraft.due_at}
                class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-2 text-sm transition-colors focus:bg-white"
                placeholder="2026-03-12T00:00:00.000Z"
                required
                type="text"
              /></label
            >
            <label class="text-xs font-medium text-gray-600 sm:col-span-2"
              >Definition of done (comma/newline separated) <textarea
                aria-label="Definition of done (comma/newline separated)"
                bind:value={createCommitmentDraft.definitionOfDoneInput}
                class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm transition-colors focus:bg-white"
                rows="2"
              ></textarea></label
            >
            <label class="text-xs font-medium text-gray-600 sm:col-span-2"
              >Links (typed refs, comma/newline separated) <textarea
                aria-label="Links (typed refs, comma/newline separated)"
                bind:value={createCommitmentDraft.linksInput}
                class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm transition-colors focus:bg-white"
                rows="2"
              ></textarea></label
            >
          </div>
          <div class="mt-3 flex justify-end">
            <button
              class="rounded-md bg-indigo-600 px-4 py-2 text-xs font-medium text-white shadow-sm hover:bg-indigo-500 disabled:opacity-50"
              disabled={creatingCommitment}
              type="submit"
              >{creatingCommitment
                ? "Creating..."
                : "Create commitment"}</button
            >
          </div>
        </form>
      {/if}

      {#if commitmentsLoading}
        <div class="px-5 py-6 text-center text-xs text-gray-400">
          Loading commitments...
        </div>
      {:else if openCommitments.length === 0}
        <div class="px-5 py-6 text-center text-sm text-gray-400">
          No open commitments.
        </div>
      {:else}
        {#each openCommitments as commitment}
          <div
            class="border-b border-gray-100 px-5 py-3.5 last:border-b-0"
            id={`commitment-card-${commitment.id}`}
          >
            <div class="flex items-start justify-between gap-2">
              <div class="min-w-0 flex-1">
                <h3 class="text-sm font-medium text-gray-900">
                  {commitment.title || commitment.id}
                </h3>
                <p class="mt-0.5 text-xs text-gray-500">
                  {actorName(commitment.owner)} · Due {commitment.due_at
                    ? formatTimestamp(commitment.due_at)
                    : "—"}
                </p>
              </div>
              <div class="flex shrink-0 items-center gap-2">
                <span
                  class={`rounded-md px-2 py-0.5 text-xs font-medium ${statusBadgeClass(commitment.status)}`}
                  >{commitment.status}</span
                >
                <button
                  class="rounded-md px-2 py-1 text-xs text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600"
                  onclick={() =>
                    editingCommitmentId === commitment.id
                      ? cancelCommitmentEdit()
                      : beginCommitmentEdit(commitment)}
                  type="button"
                >
                  {editingCommitmentId === commitment.id
                    ? "Cancel commitment edit"
                    : "Edit commitment"}
                </button>
              </div>
            </div>

            {#if (commitment.definition_of_done ?? []).length > 0}
              <ul class="mt-2 space-y-0.5 text-xs text-gray-600">
                {#each commitment.definition_of_done ?? [] as item}
                  <li class="flex items-start gap-2">
                    <span
                      class="mt-1 h-1.5 w-1.5 shrink-0 rounded-full bg-gray-300"
                    ></span>
                    {item}
                  </li>
                {/each}
              </ul>
            {/if}

            {#if (commitment.links ?? []).length > 0}
              <div class="mt-2 flex flex-wrap gap-1.5 text-xs">
                {#each commitment.links ?? [] as refValue}<RefLink
                    {refValue}
                    {threadId}
                  />{/each}
              </div>
            {/if}

            {#if editingCommitmentId === commitment.id && editCommitmentDraft}
              <form
                class="mt-3 rounded-lg border border-gray-100 bg-gray-50 p-4"
                onsubmit={(event) => {
                  event.preventDefault();
                  void saveCommitmentEdit(commitment.id);
                }}
              >
                {#if editCommitmentError}
                  <div
                    class="mb-3 rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700"
                  >
                    {editCommitmentError}
                  </div>
                {/if}
                {#if editCommitmentNotice}
                  <div
                    class="mb-3 rounded-lg bg-emerald-50 px-3 py-2 text-xs text-emerald-700"
                  >
                    {editCommitmentNotice}
                  </div>
                {/if}
                <div class="grid gap-3 sm:grid-cols-2">
                  <label class="text-xs font-medium text-gray-600 sm:col-span-2"
                    >Commitment title <input
                      aria-label="Commitment title"
                      bind:value={editCommitmentDraft.title}
                      class="mt-1.5 w-full rounded-md border border-gray-200 bg-white px-3 py-2 text-sm"
                      required
                      type="text"
                    /></label
                  >
                  <label class="text-xs font-medium text-gray-600"
                    >Owner <select
                      aria-label="Owner"
                      bind:value={editCommitmentDraft.owner}
                      class="mt-1.5 w-full rounded-md border border-gray-200 bg-white px-2.5 py-2 text-sm"
                      required
                      ><option disabled value="">Select</option
                      >{#each $actorRegistry as actor}<option value={actor.id}
                          >{actor.display_name || actor.id}</option
                        >{/each}</select
                    ></label
                  >
                  <label class="text-xs font-medium text-gray-600"
                    >Due at (ISO timestamp) <input
                      aria-label="Due at (ISO timestamp)"
                      bind:value={editCommitmentDraft.due_at}
                      class="mt-1.5 w-full rounded-md border border-gray-200 bg-white px-2.5 py-2 text-sm"
                      placeholder="2026-03-12T00:00:00.000Z"
                      required
                      type="text"
                    /></label
                  >
                  <label class="text-xs font-medium text-gray-600"
                    >Commitment status <select
                      aria-label="Commitment status"
                      bind:value={editCommitmentDraft.status}
                      class="mt-1.5 w-full rounded-md border border-gray-200 bg-white px-2.5 py-2 text-sm"
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
                    >Status evidence ref (typed ref) <input
                      aria-label="Status evidence ref (typed ref)"
                      bind:value={editCommitmentDraft.statusRefInput}
                      class="mt-1.5 w-full rounded-md border border-gray-200 bg-white px-3 py-2 text-sm"
                      placeholder="artifact:receipt-123 or event:decision-456"
                      type="text"
                    /></label
                  >
                  <label class="text-xs font-medium text-gray-600 sm:col-span-2"
                    >Completion criteria (one per line) <textarea
                      bind:value={editCommitmentDraft.definitionOfDoneInput}
                      class="mt-1.5 w-full rounded-md border border-gray-200 bg-white px-3 py-2 text-sm"
                      rows="2"
                    ></textarea></label
                  >
                  <label class="text-xs font-medium text-gray-600 sm:col-span-2"
                    >Links (one per line) <textarea
                      bind:value={editCommitmentDraft.linksInput}
                      class="mt-1.5 w-full rounded-md border border-gray-200 bg-white px-3 py-2 text-sm"
                      rows="2"
                    ></textarea></label
                  >
                </div>
                <div class="mt-3 flex gap-2">
                  <button
                    class="rounded-md bg-indigo-600 px-4 py-2 text-xs font-medium text-white shadow-sm hover:bg-indigo-500 disabled:opacity-50"
                    disabled={savingCommitmentEdit}
                    type="submit"
                    >{savingCommitmentEdit
                      ? "Saving..."
                      : "Save commitment"}</button
                  >
                  <button
                    class="rounded-md px-3 py-2 text-xs font-medium text-gray-500 hover:bg-gray-100"
                    onclick={cancelCommitmentEdit}
                    type="button">Cancel</button
                  >
                </div>
              </form>
            {/if}
          </div>
        {/each}
      {/if}
    </section>
  {/if}

  {#if activeTab === "work"}
    <div class="mt-4 grid gap-4 xl:grid-cols-2">
      <!-- Work Order -->
      <section
        class="rounded-xl border border-gray-200/80 bg-white p-5 shadow-sm"
      >
        <h2 class="text-sm font-medium text-gray-900">New Work Order</h2>
        <p class="mt-0.5 text-[13px] text-gray-500">
          Create a new work order for this thread.
        </p>
        {#if workOrderPrefillNotice}
          <div
            class="mt-3 flex items-center gap-2 rounded-lg bg-indigo-50 px-3 py-2 text-xs text-indigo-700"
          >
            <svg
              class="h-3.5 w-3.5 shrink-0"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
              />
            </svg>
            {workOrderPrefillNotice}
          </div>
        {/if}
        {#if workOrderErrors.length > 0}
          <ul
            class="mt-3 list-inside list-disc rounded-lg bg-red-50 px-4 py-2.5 text-xs text-red-700"
          >
            {#each workOrderErrors as e}<li>{e}</li>{/each}
          </ul>
        {/if}
        {#if workOrderNotice}
          <div
            class="mt-3 flex items-center gap-2 rounded-lg bg-emerald-50 px-3 py-2 text-xs text-emerald-700"
          >
            <svg
              class="h-3.5 w-3.5 shrink-0"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"
              />
            </svg>
            {workOrderNotice}
          </div>
        {/if}
        {#if workOrderDraft}
          <form
            class="mt-4 grid gap-4"
            onsubmit={(event) => {
              event.preventDefault();
              void submitWorkOrder();
            }}
          >
            <label class="text-xs font-medium text-gray-600"
              >Objective <textarea
                aria-label="Work order objective"
                bind:value={workOrderDraft.objective}
                class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm transition-colors focus:bg-white"
                placeholder="What should be accomplished?"
                rows="2"
              ></textarea></label
            >
            {#if firstFieldError(workOrderFieldErrors, "objective")}
              <p class="-mt-2 text-xs text-red-700">
                {firstFieldError(workOrderFieldErrors, "objective")}
              </p>
            {/if}
            <label class="text-xs font-medium text-gray-600"
              >Constraints <span class="font-normal text-gray-400"
                >one per line</span
              >
              <textarea
                aria-label="Constraints (comma/newline separated)"
                bind:value={workOrderDraft.constraintsInput}
                class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm transition-colors focus:bg-white"
                rows="2"
              ></textarea></label
            >
            {#if firstFieldError(workOrderFieldErrors, "constraints")}
              <p class="-mt-2 text-xs text-red-700">
                {firstFieldError(workOrderFieldErrors, "constraints")}
              </p>
            {/if}
            <div class="text-xs font-medium text-gray-600">
              Context references
              <GuidedTypedRefsInput
                addButtonLabel="Add ref to context"
                addInputLabel="Add context ref"
                addInputPlaceholder="artifact:artifact-123 or event:event-456"
                advancedHint="Paste typed refs separated by commas or new lines. This is for advanced/manual entry."
                advancedLabel="Advanced raw context refs"
                advancedToggleLabel="Use advanced raw context input"
                bind:value={workOrderDraft.contextRefsInput}
                fieldError={firstFieldError(
                  workOrderFieldErrors,
                  "context_refs",
                )}
                helperText="Choose related artifacts/events with quick picks or add typed refs manually."
                hideAdvancedToggleLabel="Hide advanced raw context input"
                suggestions={workOrderContextSuggestions}
                textareaAriaLabel="Context refs (typed refs, comma/newline separated)"
              />
            </div>
            <label class="text-xs font-medium text-gray-600"
              >Acceptance criteria <span class="font-normal text-gray-400"
                >one per line</span
              >
              <textarea
                aria-label="Acceptance criteria (comma/newline separated)"
                bind:value={workOrderDraft.acceptanceCriteriaInput}
                class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm transition-colors focus:bg-white"
                rows="2"
              ></textarea></label
            >
            {#if firstFieldError(workOrderFieldErrors, "acceptance_criteria")}
              <p class="-mt-2 text-xs text-red-700">
                {firstFieldError(workOrderFieldErrors, "acceptance_criteria")}
              </p>
            {/if}
            <label class="text-xs font-medium text-gray-600"
              >Definition of done <span class="font-normal text-gray-400"
                >one per line</span
              >
              <textarea
                aria-label="Work order definition of done (comma/newline separated)"
                bind:value={workOrderDraft.definitionOfDoneInput}
                class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm transition-colors focus:bg-white"
                rows="2"
              ></textarea></label
            >
            {#if firstFieldError(workOrderFieldErrors, "definition_of_done")}
              <p class="-mt-2 text-xs text-red-700">
                {firstFieldError(workOrderFieldErrors, "definition_of_done")}
              </p>
            {/if}
            <div class="flex justify-end">
              <button
                class="rounded-md bg-indigo-600 px-4 py-2 text-xs font-medium text-white shadow-sm hover:bg-indigo-500 disabled:opacity-50"
                disabled={creatingWorkOrder}
                type="submit"
                >{creatingWorkOrder
                  ? "Creating..."
                  : "Create work order"}</button
              >
            </div>
          </form>
        {/if}
        {#if createdWorkOrder}
          <div
            class="mt-3 flex items-center gap-2 rounded-lg border border-gray-100 bg-gray-50 px-4 py-3"
          >
            <svg
              class="h-4 w-4 text-emerald-500"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"
              />
            </svg>
            <p class="text-xs text-gray-600">
              Created: <a
                class="font-medium text-indigo-600 hover:text-indigo-700"
                href={`/artifacts/${createdWorkOrder.id}`}
                >{createdWorkOrder.id}</a
              >
              {#if createdWorkOrder.summary}
                <span class="text-gray-500"> · {createdWorkOrder.summary}</span>
              {/if}
            </p>
          </div>
        {/if}
      </section>

      <!-- Receipt -->
      <section
        class="rounded-xl border border-gray-200/80 bg-white p-5 shadow-sm"
      >
        <h2 class="text-sm font-medium text-gray-900">Add Receipt</h2>
        <p class="mt-0.5 text-[13px] text-gray-500">
          Submit a receipt tied to an existing work order.
        </p>
        {#if workOrdersError}
          <div
            class="mt-3 rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-700"
          >
            {workOrdersError}
          </div>
        {/if}
        {#if receiptErrors.length > 0}
          <ul
            class="mt-3 list-inside list-disc rounded-lg bg-red-50 px-4 py-2.5 text-xs text-red-700"
          >
            {#each receiptErrors as e}<li>{e}</li>{/each}
          </ul>
        {/if}
        {#if receiptNotice}
          <div
            class="mt-3 flex items-center gap-2 rounded-lg bg-emerald-50 px-3 py-2 text-xs text-emerald-700"
          >
            <svg
              class="h-3.5 w-3.5 shrink-0"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"
              />
            </svg>
            {receiptNotice}
          </div>
        {/if}
        {#if receiptDraft}
          <form
            class="mt-4 grid gap-4"
            onsubmit={(event) => {
              event.preventDefault();
              void submitReceipt();
            }}
          >
            <label class="text-xs font-medium text-gray-600"
              >Work order <select
                aria-label="Work order id"
                bind:value={receiptDraft.workOrderId}
                class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-2 text-sm transition-colors focus:bg-white"
                required
                ><option value="">Select work order</option
                >{#each workOrderArtifacts as wo}<option value={wo.id}
                    >{wo.summary || wo.id}</option
                  >{/each}</select
              ></label
            >
            {#if firstFieldError(receiptFieldErrors, "work_order_id")}
              <p class="-mt-2 text-xs text-red-700">
                {firstFieldError(receiptFieldErrors, "work_order_id")}
              </p>
            {/if}
            <p
              class="-mt-1 rounded-md border border-amber-100 bg-amber-50 px-3 py-2 text-xs text-amber-700"
            >
              {#if selectedReceiptWorkOrder}
                Completing work order:
                <span class="font-medium"
                  >{selectedReceiptWorkOrder.summary ||
                    selectedReceiptWorkOrder.id}</span
                >
              {:else}
                Select the work order this receipt completes before submission.
              {/if}
            </p>
            <div class="text-xs font-medium text-gray-600">
              Outputs
              <GuidedTypedRefsInput
                addButtonLabel="Add output ref"
                addInputLabel="Add receipt output ref"
                addInputPlaceholder="artifact:artifact-output-123"
                advancedHint="Paste typed refs separated by commas or new lines. This is for advanced/manual entry."
                advancedLabel="Advanced raw output refs"
                advancedToggleLabel="Use advanced raw output input"
                bind:value={receiptDraft.outputsInput}
                fieldError={firstFieldError(receiptFieldErrors, "outputs")}
                helperText="Reference the artifacts or URLs produced by this work."
                hideAdvancedToggleLabel="Hide advanced raw output input"
                suggestions={receiptOutputSuggestions}
                textareaAriaLabel="Receipt outputs (typed refs, comma/newline separated)"
              />
            </div>
            <div class="text-xs font-medium text-gray-600">
              Verification evidence
              <GuidedTypedRefsInput
                addButtonLabel="Add evidence ref"
                addInputLabel="Add receipt evidence ref"
                addInputPlaceholder="artifact:artifact-test-log-456"
                advancedHint="Paste typed refs separated by commas or new lines. This is for advanced/manual entry."
                advancedLabel="Advanced raw verification evidence refs"
                advancedToggleLabel="Use advanced raw verification evidence input"
                bind:value={receiptDraft.verificationEvidenceInput}
                fieldError={firstFieldError(
                  receiptFieldErrors,
                  "verification_evidence",
                )}
                helperText="Attach proof that validates the output (tests, logs, reviews, or decisions)."
                hideAdvancedToggleLabel="Hide advanced raw verification evidence input"
                suggestions={receiptEvidenceSuggestions}
                textareaAriaLabel="Receipt verification evidence (typed refs, comma/newline separated)"
              />
            </div>
            <label class="text-xs font-medium text-gray-600"
              >Changes summary <textarea
                aria-label="Receipt changes summary"
                bind:value={receiptDraft.changesSummary}
                class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm transition-colors focus:bg-white"
                placeholder="What changed?"
                rows="2"
              ></textarea></label
            >
            {#if firstFieldError(receiptFieldErrors, "changes_summary")}
              <p class="-mt-2 text-xs text-red-700">
                {firstFieldError(receiptFieldErrors, "changes_summary")}
              </p>
            {/if}
            <label class="text-xs font-medium text-gray-600"
              >Known gaps <span class="font-normal text-gray-400"
                >one per line</span
              >
              <textarea
                aria-label="Receipt known gaps (comma/newline separated)"
                bind:value={receiptDraft.knownGapsInput}
                class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm transition-colors focus:bg-white"
                rows="2"
              ></textarea></label
            >
            <div class="flex justify-end">
              <button
                class="rounded-md bg-indigo-600 px-4 py-2 text-xs font-medium text-white shadow-sm hover:bg-indigo-500 disabled:opacity-50"
                disabled={creatingReceipt || workOrderArtifacts.length === 0}
                type="submit"
                >{creatingReceipt ? "Submitting..." : "Submit receipt"}</button
              >
            </div>
          </form>
        {/if}
        {#if createdReceipt}
          <div
            class="mt-3 flex items-center gap-2 rounded-lg border border-gray-100 bg-gray-50 px-4 py-3"
          >
            <svg
              class="h-4 w-4 text-emerald-500"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"
              />
            </svg>
            <p class="text-xs text-gray-600">
              Submitted: <a
                class="font-medium text-indigo-600 hover:text-indigo-700"
                href={`/artifacts/${createdReceipt.id}`}>{createdReceipt.id}</a
              >
              {#if createdReceipt.summary}
                <span class="text-gray-500"> · {createdReceipt.summary}</span>
              {/if}
            </p>
          </div>
        {/if}
      </section>
    </div>
  {/if}

  {#if activeTab === "timeline"}
    <!-- Timeline -->
    <div class="mt-4">
      {#if timelineLoading}
        <div
          class="flex items-center justify-center gap-2 py-8 text-sm text-gray-400"
        >
          <svg class="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle
              class="opacity-25"
              cx="12"
              cy="12"
              r="10"
              stroke="currentColor"
              stroke-width="4"
            ></circle>
            <path
              class="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            ></path>
          </svg>
          Loading timeline...
        </div>
      {:else if timelineError}
        <div
          class="flex items-start gap-2 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700"
        >
          <svg
            class="mt-0.5 h-4 w-4 shrink-0 text-red-400"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="2"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L4.082 16.5c-.77.833.192 2.5 1.732 2.5z"
            />
          </svg>
          {timelineError}
        </div>
      {:else if timelineView.length === 0}
        <div class="py-8 text-center text-sm text-gray-400">No events yet</div>
      {:else}
        <div class="relative space-y-0">
          <!-- Timeline line -->
          <div
            class="absolute left-[11px] top-3 bottom-3 w-px bg-gray-200"
          ></div>

          {#each timelineView as event}
            <article
              class="group relative flex gap-3 py-2.5"
              id={`event-${event.id}`}
            >
              <!-- Timeline dot -->
              <div
                class="relative z-10 mt-1.5 flex h-[23px] w-[23px] shrink-0 items-center justify-center"
              >
                <span
                  class="h-2 w-2 rounded-full {event.rawType === 'decision_made'
                    ? 'bg-indigo-500'
                    : event.rawType === 'exception_raised'
                      ? 'bg-red-400'
                      : event.rawType === 'message_posted'
                        ? 'bg-blue-400'
                        : 'bg-gray-300'}"
                ></span>
              </div>

              <div
                class="min-w-0 flex-1 rounded-lg border border-transparent px-3 py-2 transition-colors hover:border-gray-100 hover:bg-gray-50/50"
              >
                <div class="flex items-start justify-between gap-3">
                  <div class="min-w-0 flex-1">
                    <p class="text-sm text-gray-900">{event.summary}</p>
                    <p class="mt-0.5 text-xs text-gray-400">
                      {actorName(event.actor_id)} ·
                      <span class="text-gray-300">{event.typeLabel}</span>
                      · {formatTimestamp(event.ts) || "—"}
                    </p>
                  </div>
                  <button
                    class="shrink-0 rounded-md px-2 py-1 text-xs text-gray-400 opacity-0 transition-all hover:bg-gray-100 hover:text-gray-600 group-hover:opacity-100"
                    onclick={() => setReplyTarget(event.id)}
                    type="button">Reply</button
                  >
                </div>

                {#if event.changedFields.length > 0}
                  <div class="mt-1.5 flex flex-wrap gap-1 text-xs">
                    {#each event.changedFields as field}
                      <span
                        class="rounded-md bg-gray-100 px-1.5 py-0.5 text-gray-500"
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
                      >Unknown event details</summary
                    >
                    <pre
                      class="mt-1 overflow-auto rounded-md bg-gray-100 p-2.5 text-[11px] text-gray-600">{JSON.stringify(
                        event.payload ?? {},
                        null,
                        2,
                      )}</pre>
                  </details>
                {/if}
              </div>
            </article>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
{/if}
