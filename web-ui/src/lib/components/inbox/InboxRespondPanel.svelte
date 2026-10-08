<script>
  import { onDestroy, tick } from "svelte";

  import {
    ACCESS_APPROVE_OUTCOME,
    ACCESS_DENY_OUTCOME,
    describeGrantAuthority,
    grantConfirmTitle,
  } from "$lib/accessGrant.js";
  import {
    MAX_KEYED_PROPOSALS,
    PROPOSAL_FLASH_MS,
    prefersReducedMotion,
    proposalKeyAction,
  } from "$lib/inboxProposalChoice.js";
  import { formatShortcut } from "$lib/keyboardHints.js";

  /**
   * How the reader answers an ask, review or escalation. Shared by the Inbox
   * pane and the standalone item page so both behave the same.
   *
   * Suggested responses: nothing is highlighted on arrival, including the
   * requester's recommendation — it wears a badge, not a selection. Clicking
   * an option flashes it once and sends it (with undo). A number key selects
   * first and sends on the same key again, because 1 and 2 are one keystroke
   * apart and the response is already on its way by the time a mistake is
   * visible. `pressProposalKey` and `clearProposalChoice` are how the pages'
   * keyboard handlers reach that state; both surfaces call the same two.
   *
   * Sending is the caller's job (`onSend(text, outcome)`); it queues the response
   * behind the undo toast.
   *
   * An access request (`access` set) is not an ordinary review. Core accepts
   * only approved or rejected on those items, so a freeform reply, a canned
   * proposal and Acknowledge all fail with `invalid_request` and leave the
   * request pending — the reader would think they had answered. Approving
   * also hands over real authority, so it asks the same second question the
   * Access page asks, in the same words, and only of a person
   * (`canDecideAccess`), since core accepts that decision from nobody else.
   */
  let {
    kind = "",
    access = null,
    canDecideAccess = false,
    proposals = [],
    /**
     * Which item these suggestions belong to. The panel is reused as the
     * reader moves between items, and the highlight must not survive the move:
     * two items can carry the same suggestions, so their text cannot stand in
     * for their identity.
     */
    itemKey = "",
    draft = $bindable(""),
    chosen = "",
    busy = false,
    replyId = "inbox-reply",
    replyLabel = "Reply",
    placeholder = "Reply…",
    tall = false,
    sendLabel = "Send reply",
    onSend,
    onAcknowledge = null,
    extras = null,
    after = null,
  } = $props();

  const MAX_KEYED = MAX_KEYED_PROPOSALS;
  let isReview = $derived(String(kind ?? "").toLowerCase() === "review");
  let isAccess = $derived(Boolean(access?.requestId));
  let keyable = $derived(proposals.length && !isAccess);

  /** The highlighted suggestion, or -1 while nothing is chosen. */
  let armed = $state(-1);
  /** The suggestion list, so a key press can move focus onto what it chose. */
  let proposalListEl = $state(null);
  /** The suggestion mid-flash. Also the guard against a double send. */
  let flashing = $state(-1);
  let flashTimer = null;

  /*
   * An Undo hands the reader's own earlier choice back, and that choice is
   * worth re-highlighting — it is the one thing the reader did pick. Anything
   * else (a new item, a cleared draft) starts with nothing selected.
   */
  let restoredIndex = $derived.by(() => {
    const restored = String(chosen ?? "").trim();
    if (!restored) return -1;
    return proposals.findIndex(
      (proposal) => String(proposal ?? "").trim() === restored,
    );
  });
  $effect(() => {
    // Identity and the restored choice only: `proposals` is rebuilt on every
    // live tick, and depending on the array itself would clear the highlight
    // under a reader who is still deciding.
    void itemKey;
    const index = restoredIndex;
    clearTimeout(flashTimer);
    flashTimer = null;
    armed = index;
    flashing = -1;
  });
  onDestroy(() => clearTimeout(flashTimer));

  /** Flash the chosen option once, then send it. */
  function commit(index) {
    const proposal = proposals[index];
    if (!proposal || busy || flashing >= 0) return;
    armed = index;
    flashing = index;
    const finish = () => {
      flashTimer = null;
      flashing = -1;
      // Nothing is selected once it is sent: a stray extra press of the same
      // key then re-selects rather than sending the same answer twice.
      armed = -1;
      send(proposal);
    };
    // Reduced motion keeps the confirmation, drops the animation and the wait.
    if (prefersReducedMotion()) {
      finish();
      return;
    }
    flashTimer = setTimeout(finish, PROPOSAL_FLASH_MS);
  }

  /**
   * A number key: select, or send what is already selected.
   *
   * @param {number} number the 1-based key the reader pressed
   * @returns {boolean} true when the key did something here
   */
  export function pressProposalKey(number) {
    if (!keyable || busy) return false;
    const index = Number(number) - 1;
    const action = proposalKeyAction({
      index,
      armed,
      count: Math.min(proposals.length, MAX_KEYED),
    });
    if (action === "ignore") return false;
    if (action === "send") {
      commit(index);
      return true;
    }
    armed = index;
    /*
     * Focus follows the selection. A reader using a screen reader otherwise
     * gets no sign that the key did anything, and focusing the option they
     * chose also makes Enter the same confirmation the second press is.
     */
    proposalListEl
      ?.querySelector(`[data-inbox-proposal="${index + 1}"]`)
      ?.focus?.({ preventScroll: true });
    return true;
  }

  /**
   * Escape, while something is highlighted.
   *
   * @returns {boolean} true when a highlight was cleared, so the caller knows
   *   whether Escape still belongs to whatever else is listening for it.
   */
  export function clearProposalChoice() {
    if (armed < 0 || flashing >= 0) return false;
    armed = -1;
    return true;
  }

  // The confirmation belongs to one request, not to the panel. This component
  // is reused as the reader moves between inbox items, so a boolean would
  // stay true under the next request and put its Grant button one click away
  // from a reader who never saw its authority text.
  let confirmingRequestId = $state("");
  let confirming = $derived(
    isAccess && confirmingRequestId === access.requestId,
  );
  let confirmEl = $state(null);

  let who = $derived(access?.requesterLabel || "This agent");
  let authority = $derived(
    describeGrantAuthority({ who, grant: access?.grant }),
  );

  function send(text, outcome = "answered") {
    const body = String(text ?? "").trim();
    if (!body || busy) return;
    onSend?.(body, outcome);
  }

  async function startConfirm() {
    if (!isAccess) return;
    confirmingRequestId = access.requestId;
    // Clicking Approve unmounts the focused button, and the panel below it
    // carries the authority the reader is about to hand over.
    await tick();
    confirmEl?.focus?.();
  }

  /** Decide the request the confirmation was opened for, or nothing. */
  function decide(requestId, text, outcome) {
    if (!canDecideAccess || requestId !== access?.requestId) return;
    send(text, outcome);
  }
</script>

<div class="space-y-4">
  {#if isAccess && !canDecideAccess}
    <!-- Core takes this decision from a person only, so an agent principal
         is told who can decide rather than shown controls that 403. -->
    <p class="text-meta text-fg-muted" data-inbox-access-human-only>
      A person has to decide this request. Signed in as an agent, you can read
      it but not approve or deny it.
    </p>
  {:else if isAccess}
    <div data-inbox-access-decision>
      {#if confirming}
        <div
          class="space-y-2 rounded-md border border-line bg-bg-soft px-3 py-2.5"
          data-inbox-access-confirm
          role="group"
          aria-label="Confirm this grant"
          tabindex="-1"
          bind:this={confirmEl}
        >
          <p class="text-meta font-medium text-fg">
            {grantConfirmTitle({ who, grant: access?.grant })}
          </p>
          <p class="text-micro text-fg-muted">{authority}</p>
          <div class="flex flex-wrap gap-2 pt-0.5">
            <button
              class="ui-btn-primary"
              type="button"
              disabled={busy}
              onclick={() =>
                decide(
                  confirmingRequestId,
                  `Approved ${access?.grant || "the grant"}.`,
                  ACCESS_APPROVE_OUTCOME,
                )}>Grant administration</button
            >
            <button
              class="ui-btn-secondary"
              type="button"
              disabled={busy}
              onclick={() => (confirmingRequestId = "")}>Cancel</button
            >
          </div>
        </div>
      {:else}
        <div class="flex flex-wrap gap-2">
          <button
            class="ui-btn-secondary"
            type="button"
            disabled={busy}
            onclick={startConfirm}>Approve…</button
          >
          <button
            class="ui-btn-secondary"
            type="button"
            disabled={busy}
            onclick={() =>
              decide(
                access?.requestId,
                "Denied the request.",
                ACCESS_DENY_OUTCOME,
              )}>Deny request</button
          >
        </div>
      {/if}
      <p class="mt-2 text-micro text-fg-subtle">
        An access request takes a decision, not a reply.
      </p>
    </div>
  {:else if isReview}
    <div class="flex flex-wrap gap-2">
      <button
        class="ui-btn-secondary"
        type="button"
        disabled={busy}
        onclick={() => send("Approved.", "approved")}>Approve</button
      >
      <button
        class="ui-btn-secondary"
        type="button"
        disabled={busy}
        onclick={() => send("Rejected.", "rejected")}>Reject</button
      >
    </div>
  {/if}

  {#if proposals.length && !isAccess}
    <div role="group" aria-labelledby={`${replyId}-suggested`}>
      <p class="ui-label" id={`${replyId}-suggested`}>Suggested responses</p>
      <ul class="space-y-1.5" bind:this={proposalListEl}>
        {#each proposals as proposal, index (index)}
          {@const recommended = index === 0}
          {@const selected = armed === index}
          <li>
            <button
              class="proposal flex w-full items-start gap-2.5 rounded-md border px-3 py-2 text-left text-meta text-fg transition-colors disabled:opacity-60 {selected
                ? 'border-accent bg-accent-soft ring-1 ring-accent'
                : 'border-line bg-bg-soft hover:border-line-strong hover:bg-panel-hover'}"
              class:proposal--sending={flashing === index}
              type="button"
              disabled={busy}
              data-inbox-proposal={index < MAX_KEYED ? index + 1 : undefined}
              data-inbox-proposal-armed={selected ? "true" : undefined}
              data-inbox-proposal-sending={flashing === index
                ? "true"
                : undefined}
              aria-keyshortcuts={index < MAX_KEYED
                ? String(index + 1)
                : undefined}
              aria-pressed={selected ? "true" : "false"}
              onclick={() => commit(index)}
            >
              {#if index < MAX_KEYED}
                <kbd
                  class="mt-px hidden w-4 shrink-0 rounded-sm border border-line-strong text-center font-mono text-micro leading-4 text-fg-muted sm:inline-block"
                  >{index + 1}</kbd
                >
              {/if}
              <!-- Canned replies quote ids and addresses verbatim. -->
              <span
                class="min-w-0 flex-1 whitespace-pre-wrap [overflow-wrap:anywhere]"
                >{proposal}</span
              >
              {#if recommended}
                <span
                  class="shrink-0 rounded-sm bg-accent-soft px-1.5 text-micro font-medium text-accent-text"
                  >Recommended</span
                >
              {/if}
            </button>
          </li>
        {/each}
      </ul>
    </div>
  {/if}

  <!-- Core rejects every other outcome on an access-backed item, so the
       freeform reply and Acknowledge are not offered there at all. -->
  {#if !isAccess}
    <form
      class="space-y-2"
      onsubmit={(event) => {
        event.preventDefault();
        send(draft);
      }}
    >
      <label class="ui-label" for={replyId}>{replyLabel}</label>
      <textarea
        id={replyId}
        class="ui-input {tall ? 'min-h-[160px]' : 'min-h-20'}"
        bind:value={draft}
        {placeholder}
        data-inbox-shortcut="reply"
        aria-keyshortcuts="R"
      ></textarea>
      {@render extras?.()}
      <div class="flex flex-wrap items-center gap-2">
        <button
          class="ui-btn-primary"
          type="submit"
          disabled={busy || !draft.trim()}
          title={`${sendLabel} (${formatShortcut("Enter")})`}
          >{sendLabel}</button
        >
        {#if onAcknowledge}
          <button
            class="ui-btn-secondary"
            type="button"
            disabled={busy}
            data-inbox-shortcut="done"
            aria-keyshortcuts="E"
            title="Acknowledge (E)"
            onclick={() => onAcknowledge()}>Acknowledge</button
          >
        {/if}
        {@render after?.()}
      </div>
    </form>
  {:else}
    <!-- The access decision stands alone; `after` still carries the caller's
         own affordances (Open item, shortcut hints). -->
    <div class="flex flex-wrap items-center gap-2">{@render after?.()}</div>
  {/if}
</div>

<style>
  /*
   * One pulse, 220ms, so a click reads as "this one, going out" rather than a
   * row that vanished. Reduced motion sends without it — the delay is skipped
   * in script, and the animation is dropped here as well.
   */
  @keyframes proposal-send {
    0% {
      background: var(--accent-soft);
    }
    45% {
      background: var(--accent-solid);
      color: white;
    }
    100% {
      background: var(--accent-soft);
    }
  }
  .proposal--sending {
    animation: proposal-send var(--proposal-flash, 220ms) ease-out 1;
  }
  @media (prefers-reduced-motion: reduce) {
    .proposal--sending {
      animation: none;
    }
  }
</style>
