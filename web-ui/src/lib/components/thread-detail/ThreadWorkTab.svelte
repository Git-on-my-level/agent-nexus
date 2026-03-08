<script>
  import { page } from "$app/stores";
  import { threadDetailStore } from "$lib/threadDetailStore";
  import { parseListInput } from "$lib/threadPatch";
  import {
    validateWorkOrderDraft,
    validateReceiptDraft,
  } from "$lib/workOrderUtils";

  let { threadId, onWorkOrderSubmit, onReceiptSubmit } = $props();

  let workOrders = $derived(threadDetailStore.workOrders);
  let workOrdersLoading = $derived(threadDetailStore.workOrdersLoading);
  let workOrdersError = $derived(threadDetailStore.workOrdersError);

  let workOrderShouldPrefill = $derived(
    $page.url.searchParams.get("compose") === "work-order",
  );
  let workOrderPrefillRefs = $derived(
    $page.url.searchParams
      .getAll("context_ref")
      .map((v) => String(v).trim())
      .filter(Boolean),
  );

  let workOrderDraft = $state(null);
  let creatingWorkOrder = $state(false);
  let workOrderErrors = $state([]);
  let workOrderNotice = $state("");
  let createdWorkOrder = $state(null);

  let receiptDraft = $state(null);
  let creatingReceipt = $state(false);
  let receiptErrors = $state([]);
  let receiptNotice = $state("");
  let createdReceipt = $state(null);

  let workOrderPrefillNotice = $state("");

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
      workOrderId: workOrders[0]?.id ?? "",
      outputsInput: "",
      verificationEvidenceInput: "",
      changesSummary: "",
      knownGapsInput: "",
    };
  }

  $effect(() => {
    if (workOrderShouldPrefill && workOrderDraft) {
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
    }
  });

  $effect(() => {
    if (!workOrderDraft) {
      workOrderDraft = blankWorkOrderDraft();
    }
  });

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
    const workOrderId = generateWorkOrderId();
    try {
      await onWorkOrderSubmit(
        threadId,
        {
          id: workOrderId,
          kind: "work_order",
          thread_id: threadId,
          summary: v.normalized.objective,
          refs: [`thread:${threadId}`],
        },
        {
          work_order_id: workOrderId,
          thread_id: threadId,
          ...v.normalized,
        },
      );
      createdWorkOrder = { id: workOrderId, summary: v.normalized.objective };
      workOrderNotice = "Work order created.";
      workOrderDraft = blankWorkOrderDraft();
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
    const receiptId = generateReceiptId();
    try {
      await onReceiptSubmit(
        threadId,
        {
          id: receiptId,
          kind: "receipt",
          thread_id: threadId,
          summary: v.normalized.changes_summary.slice(0, 120),
          refs: [
            `thread:${threadId}`,
            `artifact:${v.normalized.work_order_id}`,
          ],
        },
        {
          receipt_id: receiptId,
          work_order_id: v.normalized.work_order_id,
          thread_id: threadId,
          ...v.normalized,
        },
      );
      createdReceipt = {
        id: receiptId,
        summary: v.normalized.changes_summary.slice(0, 120),
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

{#if workOrderShouldPrefill}
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
          void handleSubmitWorkOrder();
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
{:else}
  <div class="mt-4 rounded-lg border border-gray-200 bg-white p-4">
    <h2 class="text-xs font-semibold uppercase tracking-wider text-gray-400">
      New Work Order
    </h2>
    <p class="mt-0.5 text-xs text-gray-500">
      Create a new work order for this thread.
    </p>
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
          void handleSubmitWorkOrder();
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
{/if}

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
  {#if workOrdersLoading}
    <p class="mt-2 text-xs text-gray-400">Loading work orders...</p>
  {:else if receiptDraft}
    <form
      class="mt-3 grid gap-3"
      onsubmit={(event) => {
        event.preventDefault();
        void handleSubmitReceipt();
      }}
    >
      <label class="text-xs font-medium text-gray-600"
        >Work order <select
          bind:value={receiptDraft.workOrderId}
          class="mt-1 w-full rounded border border-gray-200 px-2 py-1.5 text-sm"
          required
          ><option value="">Select work order</option
          >{#each workOrders as wo}<option value={wo.id}
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
        disabled={creatingReceipt || workOrders.length === 0}
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
