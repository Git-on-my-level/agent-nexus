<script>
  import { page } from "$app/stores";
  import { projectPath } from "$lib/projectPaths";
  import { threadDetailStore } from "$lib/threadDetailStore";
  import {
    applyWorkOrderContextPrefill,
    buildWorkOrderContextSuggestions,
    mergeContextRefsInput,
    parseWorkOrderListInput,
    removeContextRefsFromInput,
    validateWorkOrderDraft,
  } from "$lib/workOrderUtils";
  import { validateReceiptDraft } from "$lib/receiptUtils";

  let { threadId, onWorkOrderSubmit, onReceiptSubmit } = $props();

  let workOrders = $derived($threadDetailStore.workOrders);
  let workOrdersLoading = $derived($threadDetailStore.workOrdersLoading);
  let workOrdersError = $derived($threadDetailStore.workOrdersError);
  let snapshot = $derived($threadDetailStore.snapshot);
  let documents = $derived($threadDetailStore.documents);
  let timeline = $derived($threadDetailStore.timeline);

  let workOrderShouldPrefill = $derived(
    $page.url.searchParams.get("compose") === "work-order",
  );
  let projectSlug = $derived($page.params.project);
  let workOrderPrefillRefs = $derived(
    $page.url.searchParams
      .getAll("context_ref")
      .map((v) => String(v).trim())
      .filter(Boolean),
  );
  let workOrderPrefillKey = $derived(
    workOrderShouldPrefill && workOrderPrefillRefs.length > 0
      ? `${threadId}|${workOrderPrefillRefs.join("|")}`
      : "",
  );

  let workOrderDraft = $state(null);
  let creatingWorkOrder = $state(false);
  let workOrderErrors = $state([]);
  let workOrderNotice = $state("");
  let createdWorkOrder = $state(null);
  let workOrderAppliedPrefillKey = $state("");

  let receiptDraft = $state(null);
  let creatingReceipt = $state(false);
  let receiptErrors = $state([]);
  let receiptNotice = $state("");
  let createdReceipt = $state(null);

  let workOrderPrefillNotice = $state("");
  let workOrderContextSuggestions = $derived(
    buildWorkOrderContextSuggestions({
      threadId,
      snapshot,
      documents,
      timeline,
    }),
  );
  let selectedContextRefs = $derived(
    new Set(parseWorkOrderListInput(workOrderDraft?.contextRefsInput ?? "")),
  );
  let selectedSuggestedRefCount = $derived(
    workOrderContextSuggestions.filter((suggestion) =>
      selectedContextRefs.has(suggestion.ref),
    ).length,
  );

  function projectHref(pathname = "/") {
    return projectPath(projectSlug, pathname);
  }

  function createRequestKey(prefix) {
    const randomUUID = globalThis.crypto?.randomUUID?.bind(globalThis.crypto);
    if (randomUUID) {
      return `${prefix}-${randomUUID()}`;
    }
    return `${prefix}-${Date.now().toString(36)}-${Math.trunc(performance.now?.() ?? 0).toString(36)}`;
  }

  function blankWorkOrderDraft() {
    return {
      objective: "",
      constraintsInput: "",
      contextRefsInput: `thread:${threadId}`,
      acceptanceCriteriaInput: "",
      definitionOfDoneInput: "",
      requestKey: createRequestKey("work-order"),
    };
  }

  function blankReceiptDraft() {
    return {
      workOrderId: workOrders[0]?.id ?? "",
      outputsInput: "",
      verificationEvidenceInput: "",
      changesSummary: "",
      knownGapsInput: "",
      requestKey: createRequestKey("receipt"),
    };
  }

  $effect(() => {
    if (!workOrderDraft) {
      workOrderDraft = blankWorkOrderDraft();
    }
  });

  $effect(() => {
    if (!workOrderDraft) {
      return;
    }

    const prefill = applyWorkOrderContextPrefill({
      currentInput: workOrderDraft.contextRefsInput,
      threadId,
      prefillRefs: workOrderPrefillRefs,
      prefillKey: workOrderPrefillKey,
      appliedPrefillKey: workOrderAppliedPrefillKey,
    });

    if (!prefill.applied) {
      if (!workOrderPrefillKey && workOrderPrefillNotice) {
        workOrderPrefillNotice = "";
      }
      return;
    }

    workOrderDraft = {
      ...workOrderDraft,
      contextRefsInput: prefill.nextInput,
    };
    workOrderAppliedPrefillKey = prefill.nextAppliedPrefillKey;
    workOrderPrefillNotice = "Composer prefilled from review context.";
  });

  function updateWorkOrderContextRefs(nextInput) {
    if (!workOrderDraft) return;
    workOrderDraft = {
      ...workOrderDraft,
      contextRefsInput: nextInput,
    };
  }

  function toggleSuggestedContextRef(ref) {
    if (!workOrderDraft) return;
    const nextInput = selectedContextRefs.has(ref)
      ? removeContextRefsFromInput(workOrderDraft.contextRefsInput, [ref], {
          threadId,
        })
      : mergeContextRefsInput(workOrderDraft.contextRefsInput, [ref], {
          threadId,
        });
    updateWorkOrderContextRefs(nextInput);
  }

  function addAllSuggestedContextRefs() {
    if (!workOrderDraft || workOrderContextSuggestions.length === 0) return;
    updateWorkOrderContextRefs(
      mergeContextRefsInput(
        workOrderDraft.contextRefsInput,
        workOrderContextSuggestions.map((suggestion) => suggestion.ref),
        { threadId },
      ),
    );
  }

  function removeAllSuggestedContextRefs() {
    if (!workOrderDraft || workOrderContextSuggestions.length === 0) return;
    updateWorkOrderContextRefs(
      removeContextRefsFromInput(
        workOrderDraft.contextRefsInput,
        workOrderContextSuggestions.map((suggestion) => suggestion.ref),
        { threadId },
      ),
    );
  }

  $effect(() => {
    if (!receiptDraft) {
      receiptDraft = blankReceiptDraft();
    }
  });

  async function handleSubmitWorkOrder() {
    if (!workOrderDraft) return;
    creatingWorkOrder = true;
    workOrderErrors = [];
    workOrderNotice = "";
    const v = validateWorkOrderDraft(workOrderDraft, { threadId });
    if (!v.valid) {
      workOrderErrors = v.errors;
      creatingWorkOrder = false;
      return;
    }
    try {
      const response = await onWorkOrderSubmit(
        threadId,
        {
          kind: "work_order",
          thread_id: threadId,
          summary: v.normalized.objective,
          refs: [`thread:${threadId}`],
        },
        {
          thread_id: threadId,
          ...v.normalized,
        },
        workOrderDraft.requestKey,
      );
      const createdArtifact = response?.artifact ?? {};
      createdWorkOrder = {
        id: String(createdArtifact.id ?? "").trim(),
        summary:
          String(createdArtifact.summary ?? v.normalized.objective).trim() ||
          v.normalized.objective,
      };
      workOrderNotice = "Work order created.";
      workOrderDraft = blankWorkOrderDraft();
      workOrderAppliedPrefillKey = "";
    } catch (error) {
      workOrderErrors = [
        `Failed to create work order: ${error instanceof Error ? error.message : String(error)}`,
      ];
    } finally {
      creatingWorkOrder = false;
    }
  }

  async function handleSubmitReceipt() {
    if (!receiptDraft) return;
    creatingReceipt = true;
    receiptErrors = [];
    receiptNotice = "";
    const v = validateReceiptDraft(receiptDraft, { threadId });
    if (!v.valid) {
      receiptErrors = v.errors;
      creatingReceipt = false;
      return;
    }
    try {
      const response = await onReceiptSubmit(
        threadId,
        {
          kind: "receipt",
          thread_id: threadId,
          summary: v.normalized.changes_summary.slice(0, 120),
          refs: [
            `thread:${threadId}`,
            `artifact:${v.normalized.work_order_id}`,
          ],
        },
        {
          work_order_id: v.normalized.work_order_id,
          thread_id: threadId,
          ...v.normalized,
        },
        receiptDraft.requestKey,
      );
      const createdArtifact = response?.artifact ?? {};
      createdReceipt = {
        id: String(createdArtifact.id ?? "").trim(),
        summary:
          String(
            createdArtifact.summary ??
              v.normalized.changes_summary.slice(0, 120),
          ).trim() || v.normalized.changes_summary.slice(0, 120),
      };
      receiptNotice = "Receipt submitted.";
      receiptDraft = blankReceiptDraft();
    } catch (error) {
      receiptErrors = [
        `Failed to submit receipt: ${error instanceof Error ? error.message : String(error)}`,
      ];
    } finally {
      creatingReceipt = false;
    }
  }
</script>

<div
  class="mt-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] p-4"
>
  <h2
    class="text-[12px] font-semibold uppercase tracking-wider text-[var(--ui-text-muted)]"
  >
    New Work Order
  </h2>
  <p class="mt-0.5 text-[12px] text-[var(--ui-text-muted)]">
    Create a new work order for this thread.
  </p>
  {#if workOrderPrefillNotice}<p
      class="mt-2 rounded bg-indigo-500/10 px-3 py-1.5 text-[12px] text-indigo-400"
    >
      {workOrderPrefillNotice}
    </p>{/if}
  {#if workOrderErrors.length > 0}<ul
      class="mt-2 list-inside list-disc rounded bg-red-500/10 px-3 py-1.5 text-[12px] text-red-400"
    >
      {#each workOrderErrors as e}<li>{e}</li>{/each}
    </ul>{/if}
  {#if workOrderNotice}<p
      class="mt-2 rounded bg-emerald-500/10 px-3 py-1.5 text-[12px] text-emerald-400"
    >
      {workOrderNotice}
    </p>{/if}
  {#if workOrderDraft}
    <form
      class="mt-3 grid gap-3"
      onsubmit={(event) => {
        event.preventDefault();
        void handleSubmitWorkOrder();
      }}
    >
      <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
        >Work order objective <textarea
          bind:value={workOrderDraft.objective}
          class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
          rows="2"
        ></textarea></label
      >
      <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
        >Constraints (one per line) <textarea
          bind:value={workOrderDraft.constraintsInput}
          class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
          rows="2"
        ></textarea></label
      >

      <div
        class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] p-3"
      >
        <div class="flex flex-wrap items-start justify-between gap-2">
          <div>
            <p class="text-[12px] font-medium text-[var(--ui-text-muted)]">
              Suggested context refs
            </p>
            <p class="mt-0.5 text-[11px] text-[var(--ui-text-subtle)]">
              Pull from key artifacts, recent receipts and reviews, decisions,
              and thread-linked docs. You can still edit the raw typed refs
              below.
            </p>
          </div>
          <div class="flex flex-wrap gap-2">
            <button
              class="cursor-pointer rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2 py-1 text-[11px] text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border)] hover:text-[var(--ui-text)] disabled:cursor-default disabled:opacity-50"
              disabled={workOrderContextSuggestions.length === 0}
              onclick={addAllSuggestedContextRefs}
              type="button">Add all</button
            >
            <button
              class="cursor-pointer rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2 py-1 text-[11px] text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border)] hover:text-[var(--ui-text)] disabled:cursor-default disabled:opacity-50"
              disabled={selectedSuggestedRefCount === 0}
              onclick={removeAllSuggestedContextRefs}
              type="button">Remove suggested</button
            >
          </div>
        </div>

        {#if workOrderContextSuggestions.length === 0}
          <p class="mt-2 text-[12px] text-[var(--ui-text-muted)]">
            No suggested refs yet for this thread.
          </p>
        {:else}
          <div class="mt-2 flex flex-wrap gap-2">
            {#each workOrderContextSuggestions as suggestion}
              <button
                aria-pressed={selectedContextRefs.has(suggestion.ref)}
                class={`max-w-full cursor-pointer rounded border px-2.5 py-1 text-left text-[11px] transition-colors ${
                  selectedContextRefs.has(suggestion.ref)
                    ? "border-indigo-400 bg-indigo-500/10 text-indigo-200"
                    : "border-[var(--ui-border)] bg-[var(--ui-panel-muted)] text-[var(--ui-text-muted)] hover:bg-[var(--ui-border)] hover:text-[var(--ui-text)]"
                }`}
                onclick={() => toggleSuggestedContextRef(suggestion.ref)}
                title={suggestion.ref}
                type="button"
              >
                <span class="block truncate font-medium"
                  >{suggestion.title}</span
                >
                <span class="block truncate text-[10px] opacity-80">
                  {suggestion.source}: {suggestion.ref}
                  {#if suggestion.detail}
                    • {suggestion.detail}{/if}
                </span>
              </button>
            {/each}
          </div>
        {/if}
      </div>

      <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
        >Context references (one per line) <textarea
          bind:value={workOrderDraft.contextRefsInput}
          class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
          rows="4"
        ></textarea></label
      >
      <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
        >Acceptance criteria (one per line) <textarea
          bind:value={workOrderDraft.acceptanceCriteriaInput}
          class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
          rows="2"
        ></textarea></label
      >
      <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
        >Definition of done (one per line) <textarea
          bind:value={workOrderDraft.definitionOfDoneInput}
          class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
          rows="2"
        ></textarea></label
      >
      <button
        class="w-fit cursor-pointer rounded bg-indigo-600 px-4 py-1.5 text-[12px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
        disabled={creatingWorkOrder}
        type="submit"
        >{creatingWorkOrder ? "Creating..." : "Create work order"}</button
      >
    </form>
  {/if}
  {#if createdWorkOrder}
    <div
      class="mt-3 rounded-md border border-[var(--ui-border-subtle)] bg-[var(--ui-bg-soft)] p-3"
    >
      <p class="text-[12px] text-[var(--ui-text-muted)]">
        Created: <a
          class="text-indigo-400 underline"
          href={projectHref(`/artifacts/${createdWorkOrder.id}`)}
          >{createdWorkOrder.summary || createdWorkOrder.id}</a
        >
      </p>
    </div>
  {/if}
</div>

<div
  class="mt-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] p-4"
>
  <h2
    class="text-[12px] font-semibold uppercase tracking-wider text-[var(--ui-text-muted)]"
  >
    Add Receipt
  </h2>
  <p class="mt-0.5 text-[12px] text-[var(--ui-text-muted)]">
    Submit a receipt tied to an existing work order.
  </p>
  {#if workOrdersError}<p
      class="mt-2 rounded bg-amber-500/10 px-3 py-1.5 text-[12px] text-amber-400"
    >
      {workOrdersError}
    </p>{/if}
  {#if receiptErrors.length > 0}<ul
      class="mt-2 list-inside list-disc rounded bg-red-500/10 px-3 py-1.5 text-[12px] text-red-400"
    >
      {#each receiptErrors as e}<li>{e}</li>{/each}
    </ul>{/if}
  {#if receiptNotice}<p
      class="mt-2 rounded bg-emerald-500/10 px-3 py-1.5 text-[12px] text-emerald-400"
    >
      {receiptNotice}
    </p>{/if}
  {#if workOrdersLoading}
    <p class="mt-2 text-[12px] text-[var(--ui-text-muted)]">
      Loading work orders...
    </p>
  {:else if receiptDraft}
    <form
      class="mt-3 grid gap-3"
      onsubmit={(event) => {
        event.preventDefault();
        void handleSubmitReceipt();
      }}
    >
      <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
        >Work order <select
          bind:value={receiptDraft.workOrderId}
          class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2 py-1.5 text-[13px] text-[var(--ui-text)]"
          required
          ><option value="">Select work order</option
          >{#each workOrders as wo}<option value={wo.id}
              >{wo.summary || wo.id}</option
            >{/each}</select
        ></label
      >
      <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
        >Outputs (one per line) <textarea
          bind:value={receiptDraft.outputsInput}
          class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
          rows="2"
        ></textarea></label
      >
      <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
        >Verification evidence (one per line) <textarea
          bind:value={receiptDraft.verificationEvidenceInput}
          class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
          rows="2"
        ></textarea></label
      >
      <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
        >Changes summary <textarea
          bind:value={receiptDraft.changesSummary}
          class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
          rows="2"
        ></textarea></label
      >
      <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
        >Known gaps (one per line) <textarea
          bind:value={receiptDraft.knownGapsInput}
          class="mt-1 w-full rounded border border-[var(--ui-border)] bg-[var(--ui-panel-muted)] px-2.5 py-1.5 text-[13px] text-[var(--ui-text)]"
          rows="2"
        ></textarea></label
      >
      <button
        class="w-fit cursor-pointer rounded bg-indigo-600 px-4 py-1.5 text-[12px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
        disabled={creatingReceipt || workOrders.length === 0}
        type="submit"
        >{creatingReceipt ? "Submitting..." : "Submit receipt"}</button
      >
    </form>
  {/if}
  {#if createdReceipt}
    <div
      class="mt-3 rounded-md border border-[var(--ui-border-subtle)] bg-[var(--ui-bg-soft)] p-3"
    >
      <p class="text-[12px] text-[var(--ui-text-muted)]">
        Submitted: <a
          class="text-indigo-400 underline"
          href={projectHref(`/artifacts/${createdReceipt.id}`)}
          >{createdReceipt.summary || createdReceipt.id}</a
        >
      </p>
    </div>
  {/if}
</div>
