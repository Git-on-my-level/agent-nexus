<script>
  import StateError from "$lib/components/state/StateError.svelte";
  import ReceiptSignal from "./ReceiptSignal.svelte";
  import {
    decisionConsequence,
    decisionFields,
    decisionPayload,
    decisionSummary,
    isNexusOwned,
    label,
    proposerLabel,
    receiptSignal,
    safeSourceHref,
    sentenceCase,
    stripDecisionPrefix,
  } from "$lib/pm/presentation.js";

  let {
    selected = null,
    taskTitle = "",
    work = null,
    action = null,
    workHref = "",
    busy = false,
    busyWith = "",
    actionError = "",
    actorLabel = (id) => id,
    currentActorId = "",
    answer = $bindable(""),
    choice = $bindable(""),
    onAnswer,
    onDeliver,
    onReconcile,
    onRefreshReceipt,
  } = $props();

  let summary = $derived(decisionSummary(selected, taskTitle));
  let proposal = $derived(
    decisionPayload(selected)
      ? ""
      : sentenceCase(stripDecisionPrefix(selected?.instruction)),
  );
  let consequence = $derived(decisionConsequence(selected, work));
  let fields = $derived(decisionFields(selected));
  // Core says whether a delivery path exists for this action's scope; an
  // older core omits the flag, in which case the server remains the judge.
  let undeliverable = $derived(action?.deliverable === false);
  let proposer = $derived(
    proposerLabel(selected, { actorLabel, currentActorId }),
  );
  // Only the approver may deliver, and only a handoff that left core can be
  // read back (a preflight failure has no sent attempt).
  let isApprover = $derived(
    !currentActorId ||
      !selected?.actor_id ||
      selected.actor_id === currentActorId,
  );
  let handedOff = $derived(
    Boolean(action?.attempts?.some((attempt) => attempt?.sent_at)),
  );
  let delivered = $derived(
    Boolean(action) && !["pending", "pending_delivery"].includes(action.status),
  );
  // Core's read-back names the phase by key; the reader knows it by label.
  function readableDetail(text) {
    return String(text ?? "").replace(
      /\bphase: ([a-z_]+)\b/g,
      (match, key) => `phase: ${label(key)}`,
    );
  }
  let noteMissing = $derived(!answer.trim());
  // Core derives can_answer for the current reader; an older core omits it,
  // in which case the server is the judge and the form stays available.
  let cannotAnswer = $derived(selected?.can_answer === false);
  let targetPhase = $derived(String(selected?.payload?.phase ?? "").trim());
  let unappliable = $derived(
    selected?.scope === "work.phase" &&
      !targetPhase &&
      work &&
      isNexusOwned(work),
  );

  // Approve and Decline are the two verbs; each submits the form with its
  // choice. The note is required by core (the answer text is the record).
  function decide(event, value) {
    event.preventDefault();
    choice = value;
    if (busy || noteMissing) return;
    onAnswer?.(event);
  }
</script>

{#if selected}
  <div class="space-y-6 p-4 sm:p-5">
    <header>
      <h2 class="text-subtitle font-semibold text-fg">
        {#if workHref}
          <a class="hover:text-accent-text" href={workHref}>{summary.title}</a>
        {:else}
          {summary.title}
        {/if}
      </h2>
      {#if targetPhase}
        <p class="mt-3 text-meta text-fg">
          <span class="text-fg-muted">Requested change:</span> move to
          <strong>{label(targetPhase)}</strong>
        </p>
      {/if}
      {#if proposal}
        <p class="ui-label mt-4">{proposer}</p>
        <p
          class="whitespace-pre-wrap break-words text-meta leading-relaxed text-fg"
        >
          {proposal}
        </p>
      {:else if fields.length}
        <p class="ui-label mt-4">{proposer}</p>
        <dl class="space-y-1 text-meta">
          {#each fields as row (row.label)}
            <div class="flex gap-2">
              <dt class="shrink-0 text-fg-muted">{row.label}</dt>
              <dd class="min-w-0 break-words text-fg">{row.value}</dd>
            </div>
          {/each}
        </dl>
      {/if}
      <details class="mt-3 text-micro text-fg-muted">
        <summary class="cursor-pointer">Technical details</summary>
        <dl class="mt-2 flex flex-wrap gap-x-6 gap-y-1">
          <div class="flex gap-1.5">
            <dt>Task ref</dt>
            <dd class="break-all font-mono text-fg">
              {selected.work_ref || "—"}
            </dd>
          </div>
          <div class="flex gap-1.5">
            <dt>Scope</dt>
            <dd class="break-words font-mono text-fg">
              {selected.scope || "—"}
            </dd>
          </div>
          <div class="flex gap-1.5">
            <dt>{work?.source?.revision ? "Source revision" : "Revision"}</dt>
            <dd class="break-all font-mono text-fg">
              {selected.target_revision || "—"}
            </dd>
          </div>
          <div class="flex gap-1.5">
            <dt>Decision id</dt>
            <dd class="break-all font-mono text-fg">{selected.id}</dd>
          </div>
        </dl>
        {#if decisionPayload(selected)}
          <pre
            class="mt-2 max-h-80 overflow-auto whitespace-pre-wrap break-words rounded bg-bg-soft p-3 font-mono">{decisionPayload(
              selected,
            )}</pre>
        {/if}
      </details>
    </header>
    {#if selected.status === "superseded" && selected.superseded_by}
      <section class="border-t border-line-subtle pt-4">
        <p class="text-meta text-fg">
          The PM replaced this proposal{#if selected.superseded_reason}: {selected.superseded_reason}{/if}.
          <a
            class="ui-prose-link"
            href={`?item=decision:${encodeURIComponent(selected.superseded_by)}`}
            >Open the replacement</a
          >
        </p>
      </section>
    {:else if selected.status === "awaiting_answer" && cannotAnswer}
      <section class="border-t border-line-subtle pt-4">
        <p class="text-meta text-fg">
          This decision is addressed to {actorLabel(selected.actor_id) ||
            "another person"}. Only they can answer it.
        </p>
      </section>
    {:else if selected.status === "awaiting_answer"}
      <form
        class="space-y-3 border-t border-line-subtle pt-4"
        onsubmit={(event) => decide(event, choice || "approve")}
      >
        <p class="text-meta text-fg">{consequence}</p>
        <label class="block text-micro text-fg-muted"
          >Your note (recorded with the decision)<textarea
            class="ui-input mt-1"
            bind:value={answer}
            rows="3"
            required
            maxlength="16000"
            placeholder="Why, and any boundaries the PM must respect…"
          ></textarea></label
        >
        <div class="flex flex-wrap items-center gap-2">
          <button
            class="ui-btn-primary"
            type="submit"
            onclick={(event) => decide(event, "approve")}
            disabled={busy || noteMissing || unappliable}
            >{busy && choice === "approve" ? "Approving…" : "Approve"}</button
          >
          <button
            class="ui-btn-secondary"
            type="button"
            onclick={(event) => decide(event, "reject")}
            disabled={busy || noteMissing}
            >{busy && choice === "reject" ? "Declining…" : "Decline"}</button
          >
          {#if noteMissing}
            <span class="text-micro text-fg-subtle">Add a note to decide.</span>
          {/if}
        </div>
      </form>
    {:else}
      <section class="border-t border-line-subtle pt-4">
        <h3 class="text-meta font-semibold text-fg">
          {selected.status === "superseded" ? "Declined" : "Approved"}
        </h3>
        <p class="mt-2 whitespace-pre-wrap break-words text-meta text-fg-muted">
          {selected.answer || "No note recorded"}
        </p>
        {#if selected.answered_by}
          <p class="mt-2 text-micro text-fg-subtle">
            By {actorLabel(selected.answered_by)}
          </p>
        {/if}
      </section>
    {/if}
    <section class="border-t border-line-subtle pt-4">
      <h3 class="text-meta font-semibold text-fg">Delivery and outcome</h3>
      {#if actionError}
        <StateError
          message={actionError}
          onretry={onRefreshReceipt}
          class="mt-2"
        />
      {/if}
      {#if action}
        {@const verified =
          action.status === "verified" &&
          action.receipt?.independently_verified === true}
        {@const state = receiptSignal(
          action.status === "verified" && !verified
            ? "source_reported"
            : action.status,
        )}
        <div class="mt-3 flex flex-wrap items-center gap-2">
          <ReceiptSignal signal={state} />
        </div>
        {#if action.reconciliation_conflict}
          <p
            class="mt-2 rounded-md bg-warn-soft px-3 py-2 text-meta text-warn-text"
          >
            The last read-back did not confirm this outcome. The status above is
            what was reported; the detail below is what the source showed.
          </p>
        {/if}
        {#if action.receipt?.detail}
          <p class="mt-2 whitespace-pre-wrap break-words text-meta text-fg">
            {readableDetail(action.receipt.detail)}
          </p>
        {/if}
        {#if safeSourceHref(action.receipt?.url)}
          <a
            class="ui-prose-link mt-2 inline-block text-meta"
            href={safeSourceHref(action.receipt.url)}
            target="_blank"
            rel="noreferrer">Open authoritative receipt ↗</a
          >
        {/if}
        {#if undeliverable && !delivered}
          <p class="mt-2 text-meta text-fg-muted">
            No delivery path is configured for this source yet. The approval is
            kept, and the request stays pending until one is.
          </p>
        {/if}
        <div class="mt-4 flex flex-wrap items-center gap-2">
          {#if action.status === "pending_delivery" && !undeliverable && isApprover}
            <button class="ui-btn-primary" onclick={onDeliver} disabled={busy}
              >{busy && busyWith === "deliver"
                ? "Requesting delivery…"
                : "Deliver approved instruction"}</button
            >
          {/if}
          {#if delivered && handedOff}
            <button
              class="ui-btn-secondary"
              onclick={onReconcile}
              disabled={busy}
              >{busy && busyWith === "reconcile"
                ? "Checking receipt…"
                : "Check source receipt"}</button
            >
          {/if}
        </div>
        <details class="mt-4 text-micro text-fg-muted">
          <summary class="cursor-pointer"
            >Authorization and attempts ({action.attempts?.length ||
              0})</summary
          >
          <pre
            class="mt-2 max-h-80 overflow-auto whitespace-pre-wrap break-words rounded bg-bg-soft p-3 font-mono">{JSON.stringify(
              {
                authorization_basis: action.authorization_basis,
                scope: action.scope,
                target_revision: action.target_revision,
                receipt_id: action.receipt?.external_id || undefined,
                evidence_refs: action.receipt?.evidence_refs || undefined,
                attempts: action.attempts || [],
              },
              null,
              2,
            )}</pre>
        </details>
      {:else}
        <p class="mt-2 text-meta text-fg-muted">
          {selected.status === "awaiting_answer"
            ? "Nothing happens until you decide."
            : "No delivery receipt yet."}
        </p>
      {/if}
    </section>
  </div>
{/if}
