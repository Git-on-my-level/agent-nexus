<script>
  /**
   * The two ways a reader keeps a kind of ask from coming back:
   *
   *   "Always answer this way": tick it, answer, and that answer becomes a
   *   standing ruling once the answer is sent.
   *   "Not a decision": send the ask back, and optionally say it should
   *   never reach Decide again from this requester.
   *
   * Both write through the durable outbox. Offered on a single ask or
   * review only; a batch, an access request and a proposal have their own
   * answers.
   */
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  import { mutationOutbox } from "$lib/mutationOutbox.js";
  import {
    canRuleOn,
    enqueueNotADecision,
    notADecisionTarget,
  } from "$lib/standingRulings.js";

  let {
    decision,
    itemId,
    /** Bound: the reader wants this answer to always hold. */
    always = $bindable(false),
    /** The card is answered, confirming, or answering for another workspace. */
    disabled = false,
    href = "",
    onAnswered = undefined,
  } = $props();

  let eligible = $derived(canRuleOn(decision));
  let target = $derived(notADecisionTarget(itemId));
  let queued = $derived(
    ($mutationOutbox ?? []).some((entry) => entry.target === target),
  );
  let confirming = $state(false);
  let everyTime = $state(false);
  let saveError = $state("");
  let sent = $state(false);
  let who = $derived(decision.requestedBy?.display || "this requester");

  async function sendBack() {
    saveError = "";
    const answeredDecision = decision;
    try {
      // Persisted before it is sent; a storage failure stops the send.
      await enqueueNotADecision({
        itemId,
        askRef: decision.eventRef,
        always: everyTime,
        title: decision.title,
        href,
      });
      confirming = false;
      sent = true;
      onAnswered?.(answeredDecision);
    } catch {
      saveError =
        "Couldn’t save this on this device, so the ask was not sent back.";
    }
  }
</script>

{#if eligible && (queued || sent)}
  <div class="w-full" data-not-a-decision-status>
    {#if queued}
      <MutationSendStatus {target} />
    {:else}
      <p class="text-meta text-fg-muted">Sent back as not a decision.</p>
    {/if}
  </div>
{:else if eligible && !disabled}
  <label
    class="inline-flex items-center gap-1.5 text-meta text-fg-muted max-lg:min-h-11"
    title="Your answer becomes a standing ruling: this exact question is answered without reaching you, whoever asks. Revoke it under Standing rulings."
  >
    <input type="checkbox" bind:checked={always} data-decision-always />
    Always answer this way
  </label>
  {#if confirming}
    <div
      class="w-full space-y-2 rounded-md border border-line bg-bg-soft px-3 py-2.5"
      role="group"
      aria-label="Send back as not a decision"
      data-not-a-decision-confirm
    >
      <p class="text-meta text-fg">
        This goes back to {who} as information, not a choice. Nothing is answered.
      </p>
      <label class="flex items-center gap-1.5 text-meta text-fg">
        <input
          type="checkbox"
          bind:checked={everyTime}
          data-not-a-decision-always
        />
        Always, for this kind of ask from {who}
      </label>
      <div class="flex flex-wrap gap-2">
        <button
          type="button"
          class="ui-btn-primary"
          data-not-a-decision-send
          onclick={sendBack}>Send back</button
        >
        <button
          type="button"
          class="ui-btn-secondary"
          onclick={() => (confirming = false)}>Cancel</button
        >
      </div>
      {#if saveError}
        <p class="text-meta text-danger-text" role="alert">{saveError}</p>
      {/if}
    </div>
  {:else}
    <button
      type="button"
      class="inline-flex items-center text-meta text-fg-muted hover:text-fg max-lg:min-h-11"
      aria-expanded="false"
      data-not-a-decision
      onclick={() => (confirming = true)}>Not a decision</button
    >
  {/if}
{/if}
