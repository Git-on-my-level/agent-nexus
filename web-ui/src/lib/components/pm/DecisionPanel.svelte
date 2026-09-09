<script>
  import SignalBadge from "./SignalBadge.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import { receiptSignal, safeSourceHref } from "$lib/pm/presentation.js";

  let {
    selected = null,
    action = null,
    workHref = "",
    busy = false,
    actionError = "",
    answer = $bindable(""),
    choice = $bindable(""),
    onAnswer,
    onDeliver,
    onReconcile,
    onRefreshReceipt,
  } = $props();

  function recordAnswer(event) {
    event.preventDefault();
    onAnswer?.(event);
  }
</script>

{#if selected}
  <div class="space-y-6 p-4 sm:p-5">
    <header>
      {#if workHref}
        <a class="ui-prose-link font-mono text-micro" href={workHref}
          >{selected.work_ref}</a
        >
      {:else}
        <span class="font-mono text-micro text-fg-muted"
          >{selected.work_ref}</span
        >
      {/if}
      <h2
        class="mt-1.5 whitespace-pre-wrap break-words text-subtitle font-semibold text-fg"
      >
        {selected.instruction}
      </h2>
      <dl class="mt-3 flex flex-wrap gap-x-6 gap-y-1 text-micro text-fg-muted">
        <div class="flex gap-1.5">
          <dt>Scope</dt>
          <dd class="break-words text-fg">{selected.scope || "—"}</dd>
        </div>
        <div class="flex gap-1.5">
          <dt>Source revision</dt>
          <dd class="break-all font-mono text-fg">
            {selected.target_revision || "—"}
          </dd>
        </div>
      </dl>
    </header>
    {#if selected.status === "awaiting_answer"}
      <form
        class="space-y-3 border-t border-line-subtle pt-4"
        onsubmit={recordAnswer}
      >
        <fieldset>
          <legend class="text-meta font-semibold text-fg">Your decision</legend>
          <div class="mt-2 flex flex-wrap gap-4 text-meta text-fg">
            <label class="flex items-center gap-2"
              ><input
                type="radio"
                name="approval"
                value="approve"
                bind:group={choice}
                required
              />Authorize this scope</label
            >
            <label class="flex items-center gap-2"
              ><input
                type="radio"
                name="approval"
                value="reject"
                bind:group={choice}
                required
              />Decline</label
            >
          </div>
        </fieldset>
        <label class="block text-micro text-fg-muted"
          >Exact response<textarea
            class="ui-input mt-1"
            bind:value={answer}
            rows="4"
            required
            maxlength="16000"
            placeholder="State your decision and any boundaries…"
          ></textarea></label
        >
        <div class="flex flex-wrap items-center gap-3">
          <button
            class="ui-btn-primary"
            type="submit"
            disabled={busy || !choice || !answer.trim()}
            >{busy ? "Recording answer…" : "Record decision"}</button
          >
          <span class="text-micro text-fg-subtle"
            >Recording does not deliver. Delivery rechecks the source revision.</span
          >
        </div>
      </form>
    {:else}
      <section class="border-t border-line-subtle pt-4">
        <h3 class="text-meta font-semibold text-fg">Recorded answer</h3>
        <p class="mt-2 whitespace-pre-wrap break-words text-meta text-fg-muted">
          {selected.answer || "No answer text recorded"}
        </p>
        {#if selected.answered_by}
          <p class="mt-2 text-micro text-fg-subtle">
            Answered by {selected.answered_by}
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
          {#if state.primary}
            <SignalBadge tone={state.tone}>{state.label}</SignalBadge>
          {:else}
            <details class="text-micro text-fg-muted">
              <summary class="cursor-pointer">{state.label}</summary>
            </details>
          {/if}
          {#if !verified}
            <span class="text-micro text-fg-subtle"
              >Outcome not independently verified</span
            >
          {/if}
        </div>
        {#if action.receipt?.detail}
          <p class="mt-2 whitespace-pre-wrap break-words text-meta text-fg">
            {action.receipt.detail}
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
        {#if action.receipt?.external_id}
          <p class="mt-2 break-words font-mono text-micro text-fg-muted">
            {action.receipt.external_id}
          </p>
        {/if}
        {#if action.receipt?.evidence_refs?.length}
          <ul class="mt-2 space-y-1 font-mono text-micro text-fg-muted">
            {#each action.receipt.evidence_refs as ref}
              <li class="break-all">{ref}</li>
            {/each}
          </ul>
        {/if}
        <div class="mt-4 flex flex-wrap items-center gap-2">
          {#if action.status === "pending_delivery"}
            <button class="ui-btn-primary" onclick={onDeliver} disabled={busy}
              >{busy
                ? "Requesting delivery…"
                : "Deliver approved instruction"}</button
            >
          {/if}
          <button class="ui-btn-secondary" onclick={onReconcile} disabled={busy}
            >{busy ? "Checking receipt…" : "Check source receipt"}</button
          >
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
                attempts: action.attempts || [],
              },
              null,
              2,
            )}</pre>
        </details>
      {:else}
        <p class="mt-2 text-meta text-fg-muted">
          {selected.status === "awaiting_answer"
            ? "Nothing is authorized yet."
            : "No delivery receipt yet."}
        </p>
      {/if}
    </section>
  </div>
{/if}
