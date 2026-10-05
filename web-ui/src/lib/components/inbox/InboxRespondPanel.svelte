<script>
  import { tick } from "svelte";

  import {
    ACCESS_APPROVE_OUTCOME,
    ACCESS_DENY_OUTCOME,
    describeGrantAuthority,
    grantConfirmTitle,
  } from "$lib/accessGrant.js";
  import { formatShortcut } from "$lib/keyboardHints.js";

  /**
   * How the reader answers an ask, review or escalation. Shared by the Inbox
   * pane and the standalone item page so both behave the same: choosing a
   * suggested response selects it and sends it (with undo); the first
   * suggestion is the requester's recommendation.
   *
   * Sending is the caller's job (`onSend(text, outcome)`); it queues the response
   * behind the undo toast.
   *
   * An access request (`access` set) is not an ordinary review. Core accepts
   * only approved or rejected on those items, so a freeform reply, a canned
   * proposal and Acknowledge all fail with `invalid_request` and leave the
   * request pending — the reader would think they had answered. Approving
   * also hands over real authority, so it asks the same second question the
   * Access page asks, in the same words.
   */
  let {
    kind = "",
    access = null,
    proposals = [],
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

  const MAX_KEYED = 5;
  let isReview = $derived(String(kind ?? "").toLowerCase() === "review");
  let isAccess = $derived(Boolean(access?.requestId));

  let confirming = $state(false);
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
    confirming = true;
    // Clicking Approve unmounts the focused button, and the panel below it
    // carries the authority the reader is about to hand over.
    await tick();
    confirmEl?.focus?.();
  }
</script>

<div class="space-y-4">
  {#if isAccess}
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
                send(
                  `Approved ${access?.grant || "the grant"}.`,
                  ACCESS_APPROVE_OUTCOME,
                )}>Grant administration</button
            >
            <button
              class="ui-btn-secondary"
              type="button"
              disabled={busy}
              onclick={() => (confirming = false)}>Cancel</button
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
            onclick={() => send("Denied the request.", ACCESS_DENY_OUTCOME)}
            >Deny request</button
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
      <ul class="space-y-1.5">
        {#each proposals as proposal, index (index)}
          {@const recommended = index === 0}
          {@const selected = chosen && chosen.trim() === proposal.trim()}
          <li>
            <button
              class="flex w-full items-start gap-2.5 rounded-md border px-3 py-2 text-left text-meta text-fg transition-colors disabled:opacity-60 {selected
                ? 'border-accent bg-accent-soft ring-1 ring-accent'
                : recommended
                  ? 'border-accent-solid bg-accent-soft hover:border-accent'
                  : 'border-line bg-bg-soft hover:border-line-strong hover:bg-panel-hover'}"
              type="button"
              disabled={busy}
              data-inbox-proposal={index < MAX_KEYED ? index + 1 : undefined}
              aria-keyshortcuts={index < MAX_KEYED
                ? String(index + 1)
                : undefined}
              aria-pressed={selected ? "true" : "false"}
              onclick={() => send(proposal)}
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
