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
  import ProvenanceBadge from "$lib/components/ProvenanceBadge.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import UnknownObjectPanel from "$lib/components/UnknownObjectPanel.svelte";
  import { coreClient } from "$lib/coreClient";
  import {
    buildThreadPatch,
    parseListInput,
    serializeListInput,
  } from "$lib/threadPatch";
  import { validateReceiptDraft } from "$lib/receiptUtils";
  import { toTimelineView } from "$lib/timelineUtils";
  import { parseRef } from "$lib/typedRefs";
  import { validateWorkOrderDraft } from "$lib/workOrderUtils";

  $: threadId = $page.params.threadId;
  $: actorName = (actorId) => lookupActorDisplayName(actorId, $actorRegistry);
  $: openCommitmentIds = Array.isArray(snapshot?.open_commitments)
    ? snapshot.open_commitments
    : [];
  $: commitmentMap = new Map(
    commitments.map((commitment) => [commitment.id, commitment]),
  );
  $: openCommitments = openCommitmentIds.map((commitmentId) => {
    const commitment = commitmentMap.get(commitmentId);
    if (commitment) {
      return commitment;
    }

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
  });

  let snapshot = null;
  let snapshotLoading = false;
  let snapshotError = "";

  let commitments = [];
  let commitmentsLoading = false;
  let commitmentsError = "";
  let createCommitmentDraft = null;
  let creatingCommitment = false;
  let createCommitmentError = "";
  let createCommitmentNotice = "";
  let editingCommitmentId = "";
  let editCommitmentDraft = null;
  let editCommitmentError = "";
  let editCommitmentNotice = "";
  let commitmentConflictWarning = "";
  let savingCommitmentEdit = false;

  let timeline = [];
  let timelineLoading = false;
  let timelineError = "";

  let workOrderDraft = null;
  let creatingWorkOrder = false;
  let workOrderErrors = [];
  let workOrderNotice = "";
  let createdWorkOrder = null;
  let workOrderArtifacts = [];
  let workOrdersLoading = false;
  let workOrdersError = "";
  let workOrderPrefillNotice = "";
  let appliedWorkOrderPrefillKey = "";

  let receiptDraft = null;
  let creatingReceipt = false;
  let receiptErrors = [];
  let receiptNotice = "";
  let createdReceipt = null;

  let messageText = "";
  let replyToEventId = "";
  let postingMessage = false;
  let postMessageError = "";

  let editOpen = false;
  let editDraft = null;
  let savingEdit = false;
  let editError = "";
  let editNotice = "";
  let conflictWarning = "";

  onMount(async () => {
    await ensureActorRegistry();
    createCommitmentDraft = blankCreateCommitmentDraft();
    workOrderDraft = blankWorkOrderDraft();
    receiptDraft = blankReceiptDraft();
    await loadThreadDetail(threadId);
  });

  $: timelineView = toTimelineView(timeline, { threadId });
  $: canPost = Boolean(messageText.trim()) && !postingMessage;
  $: workOrderShouldPrefill =
    $page.url.searchParams.get("compose") === "work-order";
  $: workOrderPrefillRefs = $page.url.searchParams
    .getAll("context_ref")
    .map((value) => String(value).trim())
    .filter(Boolean);
  $: workOrderPrefillKey = `${threadId}|${$page.url.searchParams.toString()}`;
  $: if (
    workOrderDraft &&
    workOrderShouldPrefill &&
    workOrderPrefillKey !== appliedWorkOrderPrefillKey
  ) {
    const existingRefs = parseListInput(workOrderDraft.contextRefsInput ?? "");
    const mergedRefs = Array.from(
      new Set([`thread:${threadId}`, ...workOrderPrefillRefs, ...existingRefs]),
    );

    workOrderDraft = {
      ...workOrderDraft,
      contextRefsInput: mergedRefs.join("\n"),
    };
    workOrderPrefillNotice = "Composer prefilled from review context.";
    appliedWorkOrderPrefillKey = workOrderPrefillKey;
  }
  $: if (createCommitmentDraft && !createCommitmentDraft.owner) {
    const fallbackOwnerId = defaultCommitmentOwner();
    if (fallbackOwnerId) {
      createCommitmentDraft = {
        ...createCommitmentDraft,
        owner: fallbackOwnerId,
      };
    }
  }

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
    return {
      title: thread.title ?? "",
      type: thread.type ?? "case",
      status: thread.status ?? "active",
      priority: thread.priority ?? "p2",
      cadence: thread.cadence ?? "weekly",
      next_check_in_at: thread.next_check_in_at ?? "",
      current_summary: thread.current_summary ?? "",
      tagsInput: serializeListInput(thread.tags ?? []),
      nextActionsInput: serializeListInput(thread.next_actions ?? []),
      keyArtifactsInput: serializeListInput(thread.key_artifacts ?? []),
    };
  }

  function beginEdit() {
    if (!snapshot) {
      return;
    }

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
      cadence: editDraft.cadence,
      next_check_in_at: editDraft.next_check_in_at || null,
      tags: parseListInput(editDraft.tagsInput),
      current_summary: editDraft.current_summary.trim(),
      next_actions: parseListInput(editDraft.nextActionsInput),
      key_artifacts: parseListInput(editDraft.keyArtifactsInput),
    };
  }

  function normalizeKeyArtifactRef(rawValue) {
    const normalized = String(rawValue ?? "").trim();
    if (!normalized) {
      return "";
    }

    const parsed = parseRef(normalized);
    if (parsed.prefix && parsed.value) {
      return normalized;
    }

    return `artifact:${normalized}`;
  }

  function toCommitmentEditDraft(commitment) {
    return {
      title: commitment.title ?? "",
      owner: commitment.owner ?? defaultCommitmentOwner(),
      due_at: commitment.due_at ?? "",
      status: commitment.status ?? "open",
      definitionOfDoneInput: serializeCommitmentListInput(
        commitment.definition_of_done ?? [],
      ),
      linksInput: serializeCommitmentListInput(commitment.links ?? []),
      statusRefInput: "",
    };
  }

  function statusBadgeClass(status) {
    if (status === "done") {
      return "bg-emerald-100 text-emerald-800";
    }

    if (status === "blocked") {
      return "bg-amber-100 text-amber-800";
    }

    if (status === "canceled") {
      return "bg-rose-100 text-rose-800";
    }

    return "bg-slate-100 text-slate-700";
  }

  function statusRequirementText(status) {
    if (status === "done") {
      return "Required: artifact:<receipt_id> or event:<decision_event_id>.";
    }

    if (status === "canceled") {
      return "Required: event:<decision_event_id>.";
    }

    return "";
  }

  function statusProvenance(commitment) {
    const sources = commitment?.provenance?.by_field?.status;
    if (!Array.isArray(sources) || sources.length === 0) {
      return null;
    }

    return {
      sources,
    };
  }

  async function loadOpenCommitments(commitmentIds = []) {
    commitmentsLoading = true;
    commitmentsError = "";

    if (!Array.isArray(commitmentIds) || commitmentIds.length === 0) {
      commitments = [];
      commitmentsLoading = false;
      return;
    }

    try {
      const loaded = await Promise.all(
        commitmentIds.map(async (commitmentId) => {
          try {
            const response = await coreClient.getCommitment(commitmentId);
            return response.commitment ?? null;
          } catch {
            return null;
          }
        }),
      );

      commitments = loaded.filter(Boolean);
      if (loaded.some((item) => !item)) {
        commitmentsError =
          "Some commitment snapshots could not be loaded. Showing available data.";
      }
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      commitmentsError = `Failed to load commitments: ${reason}`;
      commitments = [];
    } finally {
      commitmentsLoading = false;
    }
  }

  async function loadWorkOrders(targetThreadId) {
    workOrdersLoading = true;
    workOrdersError = "";

    try {
      const response = await coreClient.listArtifacts({
        kind: "work_order",
        thread_id: targetThreadId,
      });
      workOrderArtifacts = response.artifacts ?? [];
      if (receiptDraft && !receiptDraft.workOrderId && workOrderArtifacts[0]) {
        receiptDraft = {
          ...receiptDraft,
          workOrderId: workOrderArtifacts[0].id,
        };
      }
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      workOrdersError = `Failed to load work orders: ${reason}`;
      workOrderArtifacts = [];
    } finally {
      workOrdersLoading = false;
    }
  }

  async function saveEdit() {
    if (!snapshot || !editDraft) {
      return;
    }

    savingEdit = true;
    editError = "";
    editNotice = "";

    try {
      const keyArtifactRefs = parseListInput(editDraft.keyArtifactsInput);
      const invalidKeyArtifactRefs = keyArtifactRefs.filter((refValue) => {
        const parsed = parseRef(normalizeKeyArtifactRef(refValue));
        return !parsed.prefix || !parsed.value;
      });

      if (invalidKeyArtifactRefs.length > 0) {
        editError = `Key artifacts must be typed refs (<prefix>:<value>). Invalid: ${invalidKeyArtifactRefs.join(", ")}`;
        return;
      }

      const draftSnapshot = buildDraftSnapshotFromEdit();
      const patch = buildThreadPatch(snapshot, draftSnapshot);

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
      const reason = error instanceof Error ? error.message : String(error);

      if (error?.status === 409) {
        conflictWarning =
          "Thread was updated elsewhere. Snapshot has been reloaded; reapply your changes before saving.";
        editOpen = false;
        editDraft = null;
        await loadSnapshot(threadId);
      } else {
        editError = `Failed to save snapshot: ${reason}`;
      }
    } finally {
      savingEdit = false;
    }
  }

  async function ensureActorRegistry() {
    if ($actorRegistry.length > 0) {
      return;
    }

    try {
      const response = await coreClient.listActors();
      actorRegistry.set(response.actors ?? []);
    } catch {
      // Thread detail still renders with actor IDs if actor registry cannot be loaded.
    }
  }

  async function loadThreadDetail(targetThreadId) {
    const loadedSnapshot = await loadSnapshot(targetThreadId);

    await Promise.all([
      loadTimeline(targetThreadId),
      loadOpenCommitments(loadedSnapshot?.open_commitments ?? []),
      loadWorkOrders(targetThreadId),
      ensureActorRegistry(),
    ]);
  }

  async function loadSnapshot(targetThreadId) {
    snapshotLoading = true;
    snapshotError = "";

    try {
      const response = await coreClient.getThread(targetThreadId);
      snapshot = response.thread ?? null;
      return snapshot;
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      snapshotError = `Failed to load thread snapshot: ${reason}`;
      snapshot = null;
      return null;
    } finally {
      snapshotLoading = false;
    }
  }

  async function reloadSnapshotAndCommitments() {
    const reloadedSnapshot = await loadSnapshot(threadId);
    await loadOpenCommitments(reloadedSnapshot?.open_commitments ?? []);
  }

  async function createCommitment() {
    if (!createCommitmentDraft) {
      return;
    }

    creatingCommitment = true;
    createCommitmentError = "";
    createCommitmentNotice = "";

    try {
      const title = createCommitmentDraft.title.trim();
      const owner = createCommitmentDraft.owner.trim();
      const dueAt = createCommitmentDraft.due_at.trim();

      if (!title || !owner || !dueAt) {
        createCommitmentError = "Title, owner, and due_at are required.";
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
          provenance: {
            sources: ["actor_statement:ui"],
          },
        },
      });

      createCommitmentDraft = blankCreateCommitmentDraft();
      createCommitmentNotice = "Commitment created.";
      await reloadSnapshotAndCommitments();
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      createCommitmentError = `Failed to create commitment: ${reason}`;
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
    if (!original || !editCommitmentDraft) {
      return;
    }

    savingCommitmentEdit = true;
    editCommitmentError = "";
    editCommitmentNotice = "";
    commitmentConflictWarning = "";

    try {
      const draftSnapshot = {
        title: editCommitmentDraft.title.trim(),
        owner: editCommitmentDraft.owner.trim(),
        due_at: editCommitmentDraft.due_at.trim(),
        status: editCommitmentDraft.status,
        definition_of_done: parseCommitmentListInput(
          editCommitmentDraft.definitionOfDoneInput,
        ),
        links: parseCommitmentListInput(editCommitmentDraft.linksInput),
      };
      const patch = buildCommitmentPatch(original, draftSnapshot);

      if (Object.keys(patch).length === 0) {
        editCommitmentNotice = "No commitment changes to save.";
        return;
      }

      const refs = [];
      if (Object.prototype.hasOwnProperty.call(patch, "status")) {
        const validation = validateCommitmentStatusTransition(
          patch.status,
          editCommitmentDraft.statusRefInput,
        );

        if (!validation.valid) {
          editCommitmentError = validation.error;
          return;
        }

        const typedRef = String(
          editCommitmentDraft.statusRefInput ?? "",
        ).trim();
        if (typedRef) {
          refs.push(typedRef);
        }
      }

      const payload = {
        patch,
        if_updated_at: original.updated_at,
      };

      if (refs.length > 0) {
        payload.refs = refs;
      }

      await coreClient.updateCommitment(commitmentId, payload);
      editCommitmentNotice = "Commitment updated.";
      cancelCommitmentEdit();
      commitmentConflictWarning = "";
      await reloadSnapshotAndCommitments();
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      if (error?.status === 409) {
        commitmentConflictWarning =
          "Commitment was updated elsewhere. Reloaded latest snapshot, please reapply changes.";
        cancelCommitmentEdit();
        await reloadSnapshotAndCommitments();
      } else {
        editCommitmentError = `Failed to update commitment: ${reason}`;
      }
    } finally {
      savingCommitmentEdit = false;
    }
  }

  async function loadTimeline(targetThreadId) {
    timelineLoading = true;
    timelineError = "";

    try {
      const response = await coreClient.listThreadTimeline(targetThreadId);
      timeline = response.events ?? [];
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      timelineError = `Failed to load timeline: ${reason}`;
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
      if (replyToEventId) {
        refs.push(`event:${replyToEventId}`);
      }

      await coreClient.createEvent({
        event: {
          type: "message_posted",
          thread_id: threadId,
          refs,
          summary: `Message: ${messageText.trim().slice(0, 100)}`,
          payload: {
            text: messageText.trim(),
          },
          provenance: {
            sources: ["actor_statement:ui"],
          },
        },
      });

      messageText = "";
      replyToEventId = "";
      await loadTimeline(threadId);
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      postMessageError = `Failed to post message: ${reason}`;
    } finally {
      postingMessage = false;
    }
  }

  async function submitWorkOrder() {
    if (!workOrderDraft || !snapshot) {
      return;
    }

    creatingWorkOrder = true;
    workOrderErrors = [];
    workOrderNotice = "";

    const validation = validateWorkOrderDraft(workOrderDraft, { threadId });
    if (!validation.valid) {
      workOrderErrors = validation.errors;
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
          summary: validation.normalized.objective,
          refs: [`thread:${threadId}`],
        },
        packet: {
          work_order_id: workOrderId,
          thread_id: threadId,
          objective: validation.normalized.objective,
          constraints: validation.normalized.constraints,
          context_refs: validation.normalized.context_refs,
          acceptance_criteria: validation.normalized.acceptance_criteria,
          definition_of_done: validation.normalized.definition_of_done,
        },
      });

      createdWorkOrder = response.artifact ?? null;
      workOrderNotice = "Work order created.";
      workOrderDraft = blankWorkOrderDraft();
      await Promise.all([loadTimeline(threadId), loadWorkOrders(threadId)]);
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      workOrderErrors = [`Failed to create work order: ${reason}`];
    } finally {
      creatingWorkOrder = false;
    }
  }

  async function submitReceipt() {
    if (!receiptDraft || !snapshot) {
      return;
    }

    creatingReceipt = true;
    receiptErrors = [];
    receiptNotice = "";

    const validation = validateReceiptDraft(receiptDraft, { threadId });
    if (!validation.valid) {
      receiptErrors = validation.errors;
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
          summary: validation.normalized.changes_summary.slice(0, 120),
          refs: [
            `thread:${threadId}`,
            `artifact:${validation.normalized.work_order_id}`,
          ],
        },
        packet: {
          receipt_id: receiptId,
          work_order_id: validation.normalized.work_order_id,
          thread_id: threadId,
          outputs: validation.normalized.outputs,
          verification_evidence: validation.normalized.verification_evidence,
          changes_summary: validation.normalized.changes_summary,
          known_gaps: validation.normalized.known_gaps,
        },
      });

      createdReceipt = response.artifact ?? null;
      receiptNotice = "Receipt submitted.";
      receiptDraft = blankReceiptDraft();
      await Promise.all([loadTimeline(threadId), loadWorkOrders(threadId)]);
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      receiptErrors = [`Failed to submit receipt: ${reason}`];
    } finally {
      creatingReceipt = false;
    }
  }
</script>

<h1 class="text-2xl font-semibold">Thread Detail: {threadId}</h1>

{#if snapshotLoading}
  <p class="mt-4 rounded-md bg-white p-3 text-sm text-slate-700 shadow-sm">
    Loading thread snapshot...
  </p>
{:else if snapshotError}
  <p
    class="mt-4 rounded-md border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800"
  >
    {snapshotError}
  </p>
{:else if !snapshot}
  <p class="mt-4 rounded-md bg-white p-3 text-sm text-slate-700 shadow-sm">
    Thread not found.
  </p>
{:else}
  <section
    class="mt-4 rounded-lg border border-slate-200 bg-white p-4 shadow-sm"
  >
    <div class="flex flex-wrap items-center justify-between gap-2">
      <h2 class="text-sm font-semibold uppercase tracking-wide text-slate-500">
        Snapshot
      </h2>
      <button
        class="rounded-md border border-slate-300 bg-white px-3 py-1 text-xs font-semibold text-slate-700 hover:bg-slate-100"
        on:click={editOpen ? cancelEdit : beginEdit}
        type="button"
      >
        {editOpen ? "Cancel editing" : "Edit snapshot"}
      </button>
    </div>

    {#if conflictWarning}
      <p
        class="mt-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800"
      >
        {conflictWarning}
      </p>
    {/if}

    {#if editNotice}
      <p
        class="mt-3 rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2 text-xs text-emerald-800"
      >
        {editNotice}
      </p>
    {/if}

    <dl class="mt-3 grid gap-3 text-sm md:grid-cols-2">
      <div>
        <dt class="text-xs uppercase tracking-wide text-slate-500">title</dt>
        <dd class="font-medium text-slate-900">{snapshot.title}</dd>
      </div>
      <div>
        <dt class="text-xs uppercase tracking-wide text-slate-500">type</dt>
        <dd class="text-slate-800">{snapshot.type}</dd>
      </div>
      <div>
        <dt class="text-xs uppercase tracking-wide text-slate-500">status</dt>
        <dd class="text-slate-800">{snapshot.status}</dd>
      </div>
      <div>
        <dt class="text-xs uppercase tracking-wide text-slate-500">priority</dt>
        <dd class="text-slate-800">{snapshot.priority}</dd>
      </div>
      <div>
        <dt class="text-xs uppercase tracking-wide text-slate-500">cadence</dt>
        <dd class="text-slate-800">{snapshot.cadence}</dd>
      </div>
      <div>
        <dt class="text-xs uppercase tracking-wide text-slate-500">
          next check-in
        </dt>
        <dd class="text-slate-800">{snapshot.next_check_in_at || "none"}</dd>
      </div>
      <div>
        <dt class="text-xs uppercase tracking-wide text-slate-500">
          updated by
        </dt>
        <dd class="text-slate-800">{actorName(snapshot.updated_by)}</dd>
      </div>
      <div>
        <dt class="text-xs uppercase tracking-wide text-slate-500">
          updated at
        </dt>
        <dd class="text-slate-800">{snapshot.updated_at || "unknown"}</dd>
      </div>
    </dl>

    <div class="mt-3">
      <p class="text-xs uppercase tracking-wide text-slate-500">tags</p>
      <div class="mt-1 flex flex-wrap gap-2 text-xs">
        {#if (snapshot.tags ?? []).length === 0}
          <span class="text-slate-500">none</span>
        {:else}
          {#each snapshot.tags ?? [] as tag}
            <span class="rounded bg-slate-100 px-2 py-1">{tag}</span>
          {/each}
        {/if}
      </div>
    </div>

    <div class="mt-3">
      <p class="text-xs uppercase tracking-wide text-slate-500">
        current summary
      </p>
      <p class="mt-1 text-sm text-slate-800">{snapshot.current_summary}</p>
    </div>

    <div class="mt-3">
      <p class="text-xs uppercase tracking-wide text-slate-500">next actions</p>
      <ul class="mt-1 list-disc space-y-1 pl-5 text-sm text-slate-800">
        {#each snapshot.next_actions ?? [] as action}
          <li>{action}</li>
        {/each}
      </ul>
    </div>

    <div class="mt-3">
      <p class="text-xs uppercase tracking-wide text-slate-500">
        key artifacts
      </p>
      <ul class="mt-1 list-disc space-y-1 pl-5 text-sm text-slate-800">
        {#if (snapshot.key_artifacts ?? []).length === 0}
          <li>none</li>
        {:else}
          {#each snapshot.key_artifacts ?? [] as artifactId}
            <li>
              <RefLink
                refValue={normalizeKeyArtifactRef(artifactId)}
                {threadId}
              />
            </li>
          {/each}
        {/if}
      </ul>
    </div>

    <div class="mt-3">
      <p class="text-xs uppercase tracking-wide text-slate-500">
        open commitments
      </p>
      {#if commitmentsLoading}
        <p class="mt-1 text-sm text-slate-600">Loading commitments...</p>
      {:else}
        <ul class="mt-1 list-disc space-y-1 pl-5 text-sm text-slate-800">
          {#if openCommitments.length === 0}
            <li>none</li>
          {:else}
            {#each openCommitments as commitment}
              <li id={`commitment-${commitment.id}`}>
                <span class="font-medium"
                  >{commitment.title || commitment.id}</span
                >
                <span
                  class={`ml-2 rounded px-2 py-0.5 text-[11px] font-semibold ${statusBadgeClass(
                    commitment.status,
                  )}`}
                >
                  {commitment.status}
                </span>
                <span class="ml-2 text-xs text-slate-600">
                  due: {commitment.due_at || "unknown"}
                </span>
              </li>
            {/each}
          {/if}
        </ul>
      {/if}

      {#if commitmentsError}
        <p class="mt-2 text-xs text-amber-700">{commitmentsError}</p>
      {/if}
    </div>

    <div class="mt-3">
      <ProvenanceBadge provenance={snapshot.provenance ?? { sources: [] }} />
    </div>

    <div class="mt-3">
      <UnknownObjectPanel
        objectData={snapshot}
        title="Raw Thread Snapshot JSON"
      />
    </div>

    {#if editOpen && editDraft}
      <form
        class="mt-4 rounded-md border border-slate-200 bg-slate-50 p-3"
        on:submit|preventDefault={saveEdit}
      >
        {#if editError}
          <p
            class="mb-2 rounded-md border border-rose-200 bg-rose-50 px-2 py-1 text-xs text-rose-800"
          >
            {editError}
          </p>
        {/if}

        <div class="grid gap-3 md:grid-cols-2">
          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600"
          >
            Title
            <input
              bind:value={editDraft.title}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
              required
              type="text"
            />
          </label>

          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600"
          >
            Type
            <select
              bind:value={editDraft.type}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
            >
              <option value="case">case</option>
              <option value="process">process</option>
              <option value="relationship">relationship</option>
              <option value="initiative">initiative</option>
              <option value="incident">incident</option>
              <option value="other">other</option>
            </select>
          </label>

          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600"
          >
            Status
            <select
              bind:value={editDraft.status}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
            >
              <option value="active">active</option>
              <option value="paused">paused</option>
              <option value="closed">closed</option>
            </select>
          </label>

          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600"
          >
            Priority
            <select
              bind:value={editDraft.priority}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
            >
              <option value="p0">p0</option>
              <option value="p1">p1</option>
              <option value="p2">p2</option>
              <option value="p3">p3</option>
            </select>
          </label>

          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600"
          >
            Cadence
            <select
              bind:value={editDraft.cadence}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
            >
              <option value="reactive">reactive</option>
              <option value="daily">daily</option>
              <option value="weekly">weekly</option>
              <option value="monthly">monthly</option>
              <option value="custom">custom</option>
            </select>
          </label>

          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600"
          >
            Next check-in (ISO timestamp)
            <input
              bind:value={editDraft.next_check_in_at}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
              placeholder="2026-03-10T00:00:00.000Z"
              type="text"
            />
          </label>

          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600 md:col-span-2"
          >
            Tags (comma/newline separated)
            <textarea
              bind:value={editDraft.tagsInput}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
              rows="2"
            ></textarea>
          </label>

          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600 md:col-span-2"
          >
            Current summary
            <textarea
              bind:value={editDraft.current_summary}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
              rows="3"
            ></textarea>
          </label>

          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600 md:col-span-2"
          >
            Next actions (comma/newline separated)
            <textarea
              bind:value={editDraft.nextActionsInput}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
              rows="3"
            ></textarea>
          </label>

          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600 md:col-span-2"
          >
            Key artifacts (typed refs, comma/newline separated)
            <textarea
              bind:value={editDraft.keyArtifactsInput}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
              rows="3"
            ></textarea>
          </label>
        </div>

        <div class="mt-3 flex flex-wrap gap-2">
          <button
            class="rounded-md bg-slate-900 px-3 py-1.5 text-xs font-semibold text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-60"
            disabled={savingEdit}
            type="submit"
          >
            {savingEdit ? "Saving..." : "Save snapshot changes"}
          </button>
          <button
            class="rounded-md border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-100"
            on:click={cancelEdit}
            type="button"
          >
            Cancel
          </button>
        </div>
      </form>
    {/if}
  </section>
{/if}

{#if snapshot}
  <section
    id="work-order-composer"
    class="mt-6 rounded-lg border border-slate-200 bg-white p-4 shadow-sm"
  >
    <div class="flex flex-wrap items-center justify-between gap-2">
      <h2 class="text-sm font-semibold uppercase tracking-wide text-slate-500">
        Commitments
      </h2>
      <p class="text-xs text-slate-500">
        `thread.open_commitments` is read-only and maintained by core.
      </p>
    </div>

    <form
      class="mt-3 rounded-md border border-slate-200 bg-slate-50 p-3"
      on:submit|preventDefault={createCommitment}
    >
      <p class="text-xs font-semibold uppercase tracking-wide text-slate-600">
        Create commitment
      </p>

      {#if createCommitmentError}
        <p
          class="mt-2 rounded-md border border-rose-200 bg-rose-50 px-2 py-1 text-xs text-rose-800"
        >
          {createCommitmentError}
        </p>
      {/if}

      {#if createCommitmentNotice}
        <p
          class="mt-2 rounded-md border border-emerald-200 bg-emerald-50 px-2 py-1 text-xs text-emerald-800"
        >
          {createCommitmentNotice}
        </p>
      {/if}

      <div class="mt-2 grid gap-3 md:grid-cols-2">
        <label
          class="text-xs font-semibold uppercase tracking-wide text-slate-600 md:col-span-2"
        >
          Commitment title
          <input
            bind:value={createCommitmentDraft.title}
            class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
            required
            type="text"
          />
        </label>

        <label
          class="text-xs font-semibold uppercase tracking-wide text-slate-600"
        >
          Owner
          <select
            bind:value={createCommitmentDraft.owner}
            class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
            required
          >
            <option disabled value="">Select owner</option>
            {#each $actorRegistry as actor}
              <option value={actor.id}>
                {actor.display_name || actor.id} ({actor.id})
              </option>
            {/each}
          </select>
        </label>

        <label
          class="text-xs font-semibold uppercase tracking-wide text-slate-600"
        >
          Due at (ISO timestamp)
          <input
            bind:value={createCommitmentDraft.due_at}
            class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
            placeholder="2026-03-15T00:00:00.000Z"
            required
            type="text"
          />
        </label>

        <label
          class="text-xs font-semibold uppercase tracking-wide text-slate-600 md:col-span-2"
        >
          Definition of done (comma/newline separated)
          <textarea
            bind:value={createCommitmentDraft.definitionOfDoneInput}
            class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
            rows="3"
          ></textarea>
        </label>

        <label
          class="text-xs font-semibold uppercase tracking-wide text-slate-600 md:col-span-2"
        >
          Links (typed refs, comma/newline separated)
          <textarea
            bind:value={createCommitmentDraft.linksInput}
            class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
            rows="3"
          ></textarea>
        </label>
      </div>

      <div class="mt-3">
        <button
          class="rounded-md bg-slate-900 px-3 py-1.5 text-xs font-semibold text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-60"
          disabled={creatingCommitment}
          type="submit"
        >
          {creatingCommitment ? "Creating..." : "Create commitment"}
        </button>
      </div>
    </form>

    {#if commitmentConflictWarning}
      <p
        class="mt-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800"
      >
        {commitmentConflictWarning}
      </p>
    {/if}

    <div class="mt-4 space-y-3">
      {#if openCommitments.length === 0}
        <p class="rounded-md bg-slate-50 px-3 py-2 text-sm text-slate-700">
          No open commitments.
        </p>
      {:else}
        {#each openCommitments as commitment}
          <article
            class="rounded-md border border-slate-200 bg-white p-3"
            id={`commitment-card-${commitment.id}`}
          >
            <div class="flex flex-wrap items-start justify-between gap-2">
              <div>
                <h3 class="text-sm font-semibold text-slate-900">
                  {commitment.title || commitment.id}
                </h3>
                <p class="mt-1 text-xs text-slate-600">id: {commitment.id}</p>
                <p class="mt-1 text-xs text-slate-600">
                  owner: {actorName(commitment.owner)}
                </p>
                <p class="mt-1 text-xs text-slate-600">
                  due: {commitment.due_at || "unknown"}
                </p>
              </div>
              <div class="flex flex-wrap items-center gap-2">
                <span
                  class={`rounded px-2 py-1 text-xs font-semibold ${statusBadgeClass(
                    commitment.status,
                  )}`}
                >
                  status: {commitment.status}
                </span>
                <button
                  class="rounded-md border border-slate-300 bg-white px-2 py-1 text-xs text-slate-700 hover:bg-slate-100"
                  on:click={() =>
                    editingCommitmentId === commitment.id
                      ? cancelCommitmentEdit()
                      : beginCommitmentEdit(commitment)}
                  type="button"
                >
                  {editingCommitmentId === commitment.id
                    ? "Cancel edit"
                    : "Edit commitment"}
                </button>
              </div>
            </div>

            {#if statusProvenance(commitment)}
              <div
                class="mt-2 rounded-md border border-slate-200 bg-slate-50 p-2"
              >
                <p
                  class="text-[11px] font-semibold uppercase tracking-wide text-slate-600"
                >
                  Status provenance
                </p>
                <div class="mt-1">
                  <ProvenanceBadge provenance={statusProvenance(commitment)} />
                </div>
              </div>
            {/if}

            {#if (commitment.definition_of_done ?? []).length > 0}
              <div class="mt-2">
                <p
                  class="text-[11px] font-semibold uppercase tracking-wide text-slate-500"
                >
                  definition of done
                </p>
                <ul
                  class="mt-1 list-disc space-y-1 pl-5 text-xs text-slate-700"
                >
                  {#each commitment.definition_of_done ?? [] as item}
                    <li>{item}</li>
                  {/each}
                </ul>
              </div>
            {/if}

            {#if (commitment.links ?? []).length > 0}
              <div class="mt-2 flex flex-wrap gap-2 text-xs">
                {#each commitment.links ?? [] as refValue}
                  <span class="rounded bg-slate-100 px-2 py-1">
                    <RefLink {refValue} {threadId} />
                  </span>
                {/each}
              </div>
            {/if}

            {#if editingCommitmentId === commitment.id && editCommitmentDraft}
              <form
                class="mt-3 rounded-md border border-slate-200 bg-slate-50 p-3"
                on:submit|preventDefault={() =>
                  saveCommitmentEdit(commitment.id)}
              >
                {#if editCommitmentError}
                  <p
                    class="mb-2 rounded-md border border-rose-200 bg-rose-50 px-2 py-1 text-xs text-rose-800"
                  >
                    {editCommitmentError}
                  </p>
                {/if}

                {#if editCommitmentNotice}
                  <p
                    class="mb-2 rounded-md border border-emerald-200 bg-emerald-50 px-2 py-1 text-xs text-emerald-800"
                  >
                    {editCommitmentNotice}
                  </p>
                {/if}

                <div class="grid gap-3 md:grid-cols-2">
                  <label
                    class="text-xs font-semibold uppercase tracking-wide text-slate-600 md:col-span-2"
                  >
                    Commitment title
                    <input
                      bind:value={editCommitmentDraft.title}
                      class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
                      required
                      type="text"
                    />
                  </label>

                  <label
                    class="text-xs font-semibold uppercase tracking-wide text-slate-600"
                  >
                    Owner
                    <select
                      bind:value={editCommitmentDraft.owner}
                      class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
                      required
                    >
                      <option disabled value="">Select owner</option>
                      {#each $actorRegistry as actor}
                        <option value={actor.id}>
                          {actor.display_name || actor.id} ({actor.id})
                        </option>
                      {/each}
                    </select>
                  </label>

                  <label
                    class="text-xs font-semibold uppercase tracking-wide text-slate-600"
                  >
                    Due at (ISO timestamp)
                    <input
                      bind:value={editCommitmentDraft.due_at}
                      class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
                      required
                      type="text"
                    />
                  </label>

                  <label
                    class="text-xs font-semibold uppercase tracking-wide text-slate-600"
                  >
                    Commitment status
                    <select
                      bind:value={editCommitmentDraft.status}
                      class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
                    >
                      <option value="open">open</option>
                      <option value="blocked">blocked</option>
                      <option value="done">done</option>
                      <option value="canceled">canceled</option>
                    </select>
                  </label>

                  <div class="text-xs text-slate-600">
                    {#if statusRequirementText(editCommitmentDraft.status)}
                      <p class="font-semibold text-amber-800">
                        {statusRequirementText(editCommitmentDraft.status)}
                      </p>
                    {:else}
                      <p>
                        Status changes to open/blocked do not require a ref.
                      </p>
                    {/if}
                  </div>

                  <label
                    class="text-xs font-semibold uppercase tracking-wide text-slate-600 md:col-span-2"
                  >
                    Status evidence ref (typed ref)
                    <input
                      bind:value={editCommitmentDraft.statusRefInput}
                      class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
                      placeholder="artifact:receipt-123 or event:decision-456"
                      type="text"
                    />
                  </label>

                  <label
                    class="text-xs font-semibold uppercase tracking-wide text-slate-600 md:col-span-2"
                  >
                    Definition of done (comma/newline separated)
                    <textarea
                      bind:value={editCommitmentDraft.definitionOfDoneInput}
                      class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
                      rows="3"
                    ></textarea>
                  </label>

                  <label
                    class="text-xs font-semibold uppercase tracking-wide text-slate-600 md:col-span-2"
                  >
                    Links (typed refs, comma/newline separated)
                    <textarea
                      bind:value={editCommitmentDraft.linksInput}
                      class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
                      rows="3"
                    ></textarea>
                  </label>
                </div>

                <div class="mt-3 flex flex-wrap gap-2">
                  <button
                    class="rounded-md bg-slate-900 px-3 py-1.5 text-xs font-semibold text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-60"
                    disabled={savingCommitmentEdit}
                    type="submit"
                  >
                    {savingCommitmentEdit ? "Saving..." : "Save commitment"}
                  </button>
                  <button
                    class="rounded-md border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-100"
                    on:click={cancelCommitmentEdit}
                    type="button"
                  >
                    Cancel
                  </button>
                </div>
              </form>
            {/if}
          </article>
        {/each}
      {/if}
    </div>
  </section>
{/if}

{#if snapshot}
  <section
    class="mt-6 rounded-lg border border-slate-200 bg-white p-4 shadow-sm"
  >
    <h2 class="text-sm font-semibold uppercase tracking-wide text-slate-500">
      Work Order Composer
    </h2>
    <p class="mt-1 text-xs text-slate-600">
      Creates a `work_order` packet artifact and emits a `work_order_created`
      event.
    </p>

    {#if workOrderPrefillNotice}
      <p
        class="mt-3 rounded-md border border-sky-200 bg-sky-50 px-3 py-2 text-xs text-sky-800"
      >
        {workOrderPrefillNotice}
      </p>
    {/if}

    {#if workOrderErrors.length > 0}
      <ul
        class="mt-3 list-disc space-y-1 rounded-md border border-rose-200 bg-rose-50 px-4 py-2 text-xs text-rose-800"
      >
        {#each workOrderErrors as errorLine}
          <li>{errorLine}</li>
        {/each}
      </ul>
    {/if}

    {#if workOrderNotice}
      <p
        class="mt-3 rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2 text-xs text-emerald-800"
      >
        {workOrderNotice}
      </p>
    {/if}

    {#if workOrderDraft}
      <form
        class="mt-3 rounded-md border border-slate-200 bg-slate-50 p-3"
        on:submit|preventDefault={submitWorkOrder}
      >
        <div class="grid gap-3">
          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600"
          >
            Work order objective
            <textarea
              bind:value={workOrderDraft.objective}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
              rows="2"
            ></textarea>
          </label>

          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600"
          >
            Constraints (comma/newline separated)
            <textarea
              bind:value={workOrderDraft.constraintsInput}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
              rows="3"
            ></textarea>
          </label>

          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600"
          >
            Context refs (typed refs, comma/newline separated)
            <textarea
              bind:value={workOrderDraft.contextRefsInput}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
              rows="3"
            ></textarea>
          </label>

          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600"
          >
            Acceptance criteria (comma/newline separated)
            <textarea
              bind:value={workOrderDraft.acceptanceCriteriaInput}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
              rows="3"
            ></textarea>
          </label>

          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600"
          >
            Work order definition of done (comma/newline separated)
            <textarea
              bind:value={workOrderDraft.definitionOfDoneInput}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
              rows="3"
            ></textarea>
          </label>
        </div>

        <div class="mt-3">
          <button
            class="rounded-md bg-slate-900 px-3 py-1.5 text-xs font-semibold text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-60"
            disabled={creatingWorkOrder}
            type="submit"
          >
            {creatingWorkOrder ? "Creating..." : "Create work order"}
          </button>
        </div>
      </form>
    {/if}

    {#if createdWorkOrder}
      <div class="mt-4 rounded-md border border-slate-200 bg-white p-3">
        <p class="text-xs font-semibold uppercase tracking-wide text-slate-600">
          Latest created work order
        </p>
        <p class="mt-1 text-sm text-slate-800">
          artifact id:
          <a
            class="underline decoration-slate-300 underline-offset-2 hover:text-slate-700"
            href={`/artifacts/${createdWorkOrder.id}`}
          >
            {createdWorkOrder.id}
          </a>
        </p>
        <div class="mt-2 flex flex-wrap gap-2 text-xs">
          {#each createdWorkOrder.refs ?? [] as refValue}
            <span class="rounded bg-slate-100 px-2 py-1">
              <RefLink {refValue} {threadId} />
            </span>
          {/each}
        </div>
        <div class="mt-2">
          <UnknownObjectPanel
            objectData={createdWorkOrder}
            title="Raw Work Order Artifact JSON"
          />
        </div>
      </div>
    {/if}
  </section>
{/if}

{#if snapshot}
  <section
    class="mt-6 rounded-lg border border-slate-200 bg-white p-4 shadow-sm"
  >
    <h2 class="text-sm font-semibold uppercase tracking-wide text-slate-500">
      Add Receipt
    </h2>
    <p class="mt-1 text-xs text-slate-600">
      Submit a manual receipt packet tied to an existing work order.
    </p>

    {#if workOrdersLoading}
      <p class="mt-3 text-xs text-slate-600">
        Loading available work orders...
      </p>
    {/if}

    {#if workOrdersError}
      <p
        class="mt-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800"
      >
        {workOrdersError}
      </p>
    {/if}

    {#if receiptErrors.length > 0}
      <ul
        class="mt-3 list-disc space-y-1 rounded-md border border-rose-200 bg-rose-50 px-4 py-2 text-xs text-rose-800"
      >
        {#each receiptErrors as errorLine}
          <li>{errorLine}</li>
        {/each}
      </ul>
    {/if}

    {#if receiptNotice}
      <p
        class="mt-3 rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2 text-xs text-emerald-800"
      >
        {receiptNotice}
      </p>
    {/if}

    {#if receiptDraft}
      <form
        class="mt-3 rounded-md border border-slate-200 bg-slate-50 p-3"
        on:submit|preventDefault={submitReceipt}
      >
        <div class="grid gap-3">
          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600"
          >
            Work order id
            <select
              bind:value={receiptDraft.workOrderId}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
              required
            >
              <option value="">Select work order</option>
              {#each workOrderArtifacts as workOrder}
                <option value={workOrder.id}>
                  {workOrder.id} — {workOrder.summary || "work order"}
                </option>
              {/each}
            </select>
          </label>

          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600"
          >
            Receipt outputs (typed refs, comma/newline separated)
            <textarea
              bind:value={receiptDraft.outputsInput}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
              rows="3"
            ></textarea>
          </label>

          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600"
          >
            Receipt verification evidence (typed refs, comma/newline separated)
            <textarea
              bind:value={receiptDraft.verificationEvidenceInput}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
              rows="3"
            ></textarea>
          </label>

          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600"
          >
            Receipt changes summary
            <textarea
              bind:value={receiptDraft.changesSummary}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
              rows="3"
            ></textarea>
          </label>

          <label
            class="text-xs font-semibold uppercase tracking-wide text-slate-600"
          >
            Receipt known gaps (comma/newline separated)
            <textarea
              bind:value={receiptDraft.knownGapsInput}
              class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
              rows="3"
            ></textarea>
          </label>
        </div>

        <div class="mt-3">
          <button
            class="rounded-md bg-slate-900 px-3 py-1.5 text-xs font-semibold text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-60"
            disabled={creatingReceipt || workOrderArtifacts.length === 0}
            type="submit"
          >
            {creatingReceipt ? "Submitting..." : "Submit receipt"}
          </button>
        </div>
      </form>
    {/if}

    {#if createdReceipt}
      <div class="mt-4 rounded-md border border-slate-200 bg-white p-3">
        <p class="text-xs font-semibold uppercase tracking-wide text-slate-600">
          Latest submitted receipt
        </p>
        <p class="mt-1 text-sm text-slate-800">
          artifact id:
          <a
            class="underline decoration-slate-300 underline-offset-2 hover:text-slate-700"
            href={`/artifacts/${createdReceipt.id}`}
          >
            {createdReceipt.id}
          </a>
        </p>
        <div class="mt-2 flex flex-wrap gap-2 text-xs">
          {#each createdReceipt.refs ?? [] as refValue}
            <span class="rounded bg-slate-100 px-2 py-1">
              <RefLink {refValue} {threadId} />
            </span>
          {/each}
        </div>
        <div class="mt-2">
          <UnknownObjectPanel
            objectData={createdReceipt}
            title="Raw Receipt Artifact JSON"
          />
        </div>
      </div>
    {/if}
  </section>
{/if}

<section class="mt-6 rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
  <h2 class="text-sm font-semibold uppercase tracking-wide text-slate-500">
    Post Message
  </h2>

  {#if postMessageError}
    <p
      class="mt-2 rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-800"
    >
      {postMessageError}
    </p>
  {/if}

  <label
    class="mt-3 block text-xs font-semibold uppercase tracking-wide text-slate-600"
    for="message-text"
  >
    Message
  </label>
  <textarea
    bind:value={messageText}
    class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
    id="message-text"
    rows="3"
  ></textarea>

  <label
    class="mt-3 block text-xs font-semibold uppercase tracking-wide text-slate-600"
    for="reply-target"
  >
    Reply to event (optional)
  </label>
  <select
    bind:value={replyToEventId}
    class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
    id="reply-target"
  >
    <option value="">No reply target</option>
    {#each timelineView as event}
      <option value={event.id}>{event.id} — {event.summary}</option>
    {/each}
  </select>

  <div class="mt-3 flex flex-wrap items-center gap-2">
    <button
      class="rounded-md bg-slate-900 px-3 py-1.5 text-xs font-semibold text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-60"
      disabled={!canPost}
      on:click={postMessage}
      type="button"
    >
      {postingMessage ? "Posting..." : "Post message"}
    </button>
    {#if replyToEventId}
      <p class="text-xs text-slate-600">
        Reply target: <span class="font-mono">{replyToEventId}</span>
      </p>
      <button
        class="rounded-md border border-slate-300 bg-white px-2 py-1 text-xs text-slate-700 hover:bg-slate-100"
        on:click={clearReplyTarget}
        type="button"
      >
        Clear
      </button>
    {/if}
  </div>
</section>

<section class="mt-6 space-y-3">
  <h2 class="text-sm font-semibold uppercase tracking-wide text-slate-500">
    Timeline
  </h2>

  {#if timelineLoading}
    <p class="rounded-md bg-white p-3 text-sm text-slate-700 shadow-sm">
      Loading timeline...
    </p>
  {:else if timelineError}
    <p
      class="rounded-md border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800"
    >
      {timelineError}
    </p>
  {:else if timelineView.length === 0}
    <p class="rounded-md bg-white p-3 text-sm text-slate-700 shadow-sm">
      No timeline events for this thread yet.
    </p>
  {:else}
    {#each timelineView as event}
      <article
        class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm"
        id={`event-${event.id}`}
      >
        <p class="text-sm font-semibold text-slate-900">{event.summary}</p>
        <p class="mt-1 text-xs text-slate-600">
          type: {event.typeLabel}
          {#if !event.isKnownType}
            <span class="ml-1 font-mono text-slate-500"
              >({event.rawType || "unknown"})</span
            >
          {/if}
        </p>
        <p class="mt-1 text-xs text-slate-600">
          timestamp: {event.ts || "unknown"}
        </p>
        <p class="mt-1 text-xs text-slate-600">
          actor: {actorName(event.actor_id)}
        </p>

        {#if event.changedFields.length > 0}
          <div class="mt-2">
            <p
              class="text-xs font-semibold uppercase tracking-wide text-slate-500"
            >
              changed fields
            </p>
            <div class="mt-1 flex flex-wrap gap-2 text-xs">
              {#each event.changedFields as field}
                <span class="rounded bg-slate-100 px-2 py-1">{field}</span>
              {/each}
            </div>
          </div>
        {/if}

        <div class="mt-3 flex flex-wrap gap-2 text-xs">
          {#each event.refs as refValue}
            <span class="rounded bg-slate-100 px-2 py-1">
              <RefLink {refValue} {threadId} />
            </span>
          {/each}
        </div>

        <div class="mt-3">
          <ProvenanceBadge provenance={event.provenance ?? { sources: [] }} />
        </div>

        {#if !event.isKnownType}
          <div class="mt-2">
            <p
              class="text-xs font-semibold uppercase tracking-wide text-slate-500"
            >
              Unknown event details
            </p>
            <pre
              class="mt-1 overflow-auto rounded bg-slate-50 p-2 text-[11px] text-slate-700">{JSON.stringify(
                event.payload ?? {},
                null,
                2,
              )}</pre>
            <pre
              class="mt-1 overflow-auto rounded bg-slate-50 p-2 text-[11px] text-slate-700">{JSON.stringify(
                event.refs ?? [],
                null,
                2,
              )}</pre>
          </div>
        {/if}

        <div class="mt-3 flex flex-wrap gap-2">
          <button
            class="rounded-md border border-slate-300 bg-white px-2 py-1 text-xs text-slate-700 hover:bg-slate-100"
            on:click={() => setReplyTarget(event.id)}
            type="button"
          >
            Reply
          </button>
        </div>

        <div class="mt-3">
          <UnknownObjectPanel objectData={event} title="Raw Event JSON" />
        </div>
      </article>
    {/each}
  {/if}
</section>
