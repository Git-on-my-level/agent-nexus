<script>
  import { page } from "$app/stores";
  import { durableDraft } from "$lib/actions/durableDraft.js";
  import { copyText } from "$lib/clipboard.js";
  import {
    DECISION_KIND_LABELS,
    ageSeconds,
    decisionDeadline,
    formatSpan,
  } from "$lib/brief.js";
  import {
    captureInboxResponseBinding,
    editInboxResponse,
    inboxResponseFailures,
    inboxResponseOverlay,
    inboxUnsentResponses,
    queueInboxResponse,
    resendInboxResponse,
    retryInboxResponse,
  } from "$lib/inboxResponseQueue.js";
  import {
    enqueueCurationUngroup,
    enqueuePmDecisionAnswer,
    mutationOutbox,
  } from "$lib/mutationOutbox.js";
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  import { refreshBrief } from "$lib/briefStore.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import RefLink from "$lib/components/RefLink.svelte";

  /**
   * The one rendering of a decision: Decide's list, its permalink and the
   * initiative page all use this component (end-state S7).
   *
   * Proposals are buttons, recommendation first; one tap answers. The answer
   * waits behind the undo toast (`inboxResponseQueue.js`, a client-held
   * delayed send through the durable outbox), never a server retraction.
   * Reply… opens a composer whose draft is durable under the same key on
   * every surface, so text typed in the list survives opening the permalink.
   * Copy ref is on every row, and `c` copies it while the row has focus.
   */
  let {
    decision,
    variant = "row",
    workspace = "",
    now = Date.now(),
    focused = false,
    onAnswered = undefined,
    /**
     * For an account-level page answering a row from another workspace: that
     * row's slugs (links) and a response binding (`{ send, scope, identity }`,
     * as `captureInboxResponseBinding()` returns) that sends to it. Default:
     * the workspace in the route.
     */
    organizationSlug = "",
    workspaceSlug = "",
    binding = null,
  } = $props();

  let href = $derived(
    bindWorkspaceHref(
      organizationSlug || $page.params.organization,
      workspaceSlug || $page.params.workspace,
    ),
  );
  let itemId = $derived(decision.inboxItemId);
  let draftKey = $derived(`inbox:${itemId}:reply`);
  let full = $derived(variant === "full");
  let replying = $state(false);
  let reply = $state("");
  let copied = $state(false);
  let copyTimer;

  let deadline = $derived(decisionDeadline(decision, now));
  let age = $derived(ageSeconds(decision.createdAt, now));
  let unsent = $derived(
    $inboxUnsentResponses.find(
      (entry) => entry.itemId === itemId && entry.state !== "attention",
    ),
  );
  let overlay = $derived($inboxResponseOverlay[itemId]);
  let failure = $derived($inboxResponseFailures[itemId]);
  /** Core refused the default notify target: the requester is unreachable. */
  let unnotifiable = $derived(failure?.code === "notification_target_required");
  let answered = $derived(
    Boolean(overlay && overlay.status !== "failed") || Boolean(unsent),
  );

  /**
   * How this kind is answered. Asks, reviews, access requests and curation
   * proposals (`pm_proposal`, an ask whose first proposal is "do it") are
   * inbox items: POST /inbox/{id}/respond. A proposal that carries a PM
   * decision record answers through the durable PM decision route instead.
   * A host enrollment is approved on Access, whose routes need the
   * enrollment record, so it opens there.
   */
  let structured = $derived(decision.kind === "access");
  let pmAnswer = $derived(
    decision.kind === "pm_proposal" && Boolean(decision.pmDecision),
  );
  let pmTarget = $derived(
    decision.pmDecision ? `pm-decision:${decision.pmDecision.id}` : "",
  );
  /** An unconfirmed PM answer stays in the outbox across reloads. */
  let pmQueued = $derived(
    Boolean(pmTarget) &&
      ($mutationOutbox ?? []).some((entry) => entry.target === pmTarget),
  );
  let elsewhere = $derived(decision.kind === "host");
  let elsewhereHref = $derived(href("/access"));
  /** Approving access hands over authority, so it asks once more. */
  let confirming = $state("");
  let pmSent = $state(false);
  let saveError = $state("");

  let batchTarget = $derived(
    decision.batch ? `curation-batch:${decision.batch.key}` : "",
  );
  let ungroupQueued = $derived(
    Boolean(batchTarget) &&
      ($mutationOutbox ?? []).some((entry) => entry.target === batchTarget),
  );
  let ungroupError = $state("");

  async function ungroup() {
    ungroupError = "";
    try {
      // Persisted before it is sent; a storage failure stops the send.
      await enqueueCurationUngroup({
        key: decision.batch.key,
        target: batchTarget,
        href: href("/decide"),
        title: "Ungroup asks",
        preview: `Ungroup ${decision.batch.count} grouped asks`,
      });
    } catch {
      ungroupError =
        "Couldn’t save this on your device, so it was not sent. Try again.";
    }
  }

  $effect(() => {
    if (!batchTarget) return;
    const confirmed = (event) => {
      if (event.detail?.target === batchTarget) {
        void refreshBrief();
        onAnswered?.(decision);
      }
    };
    window.addEventListener("anx:mutation-confirmed", confirmed);
    return () =>
      window.removeEventListener("anx:mutation-confirmed", confirmed);
  });

  function outcomeFor(proposal) {
    if (!structured) return "answered";
    return /^(deny|reject|decline|refuse)/i.test(proposal)
      ? "rejected"
      : "approved";
  }

  function send(text, outcome = "answered") {
    const body = String(text ?? "").trim();
    if (!body || answered) return;
    // Core wakes only an agent requester; a human or unknown-kind requester
    // reads the answer from their asks, so "original" would be a 409 dead end.
    const notifyMode =
      decision.requestedBy?.kind === "human" ? "none" : "original";
    const who =
      notifyMode === "original" ? decision.requestedBy?.display || "" : "";
    queueInboxResponse({
      itemId,
      item: { id: itemId, kind: decision.kind, title: decision.title },
      binding: binding ?? captureInboxResponseBinding(),
      crossWorkspace: Boolean(binding),
      request: { response_text: body, outcome, notify_mode: notifyMode },
      message: who ? `Sent to ${who}` : "Answer sent",
      restore: { origin: "decide", reply, chosen: body },
    });
    replying = false;
    onAnswered?.(decision);
  }

  async function choose(proposal, index) {
    if (pmAnswer) {
      saveError = "";
      try {
        // Persisted before it is sent; a storage failure stops the send.
        await enqueuePmDecisionAnswer({
          decisionId: decision.pmDecision.id,
          revision: decision.pmDecision.revision,
          approve: index === 0,
          text: proposal,
          target: pmTarget,
          href: href(`/decide/${encodeURIComponent(itemId)}`),
          title: decision.title,
          preview: proposal,
        });
        pmSent = true;
        onAnswered?.(decision);
      } catch {
        saveError =
          "Couldn’t save your answer on this device, so it was not sent.";
      }
      return;
    }
    const outcome = outcomeFor(proposal);
    if (structured && outcome === "approved" && confirming !== proposal) {
      confirming = proposal;
      return;
    }
    confirming = "";
    send(proposal, outcome);
  }

  function sendReply(event) {
    event.preventDefault();
    send(reply);
  }

  async function copyRef() {
    if (await copyText(decision.ref)) {
      copied = true;
      clearTimeout(copyTimer);
      copyTimer = setTimeout(() => (copied = false), 1500);
    }
  }

  function editable(target) {
    return (
      target instanceof HTMLElement &&
      (target.isContentEditable ||
        ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName))
    );
  }

  function onKeydown(event) {
    if (
      event.key === "c" &&
      !event.metaKey &&
      !event.ctrlKey &&
      !event.altKey &&
      !editable(event.target)
    ) {
      event.preventDefault();
      void copyRef();
    }
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
<article
  class="decision rounded-md border border-line bg-panel px-3 py-3 sm:px-4 {focused
    ? 'ring-2 ring-accent'
    : ''}"
  data-decision-ref={decision.ref}
  data-decision-kind={decision.kind}
  data-variant={variant}
  tabindex="0"
  onkeydown={onKeydown}
  aria-label={decision.title}
>
  <header class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
    <h3
      class="min-w-0 flex-1 text-body font-medium text-fg [overflow-wrap:anywhere]"
      data-decision-title
    >
      {#if full}
        {decision.title}
      {:else}
        <a
          class="hover:underline"
          href={href(`/decide/${encodeURIComponent(itemId)}`)}
          data-decision-link>{decision.title}</a
        >
      {/if}
    </h3>
    <p class="flex flex-wrap items-center gap-x-1.5 text-meta text-fg-muted">
      {#if decision.kind !== "ask"}
        <span
          class="rounded-sm bg-bg-soft px-1.5 text-micro uppercase tracking-wide"
          >{DECISION_KIND_LABELS[decision.kind] ?? decision.kind}</span
        >
      {/if}
      {#if workspace}<span>{workspace}</span><span aria-hidden="true">·</span
        >{/if}
      {#if age !== null}<span>{formatSpan(age)}</span>{/if}
      {#if deadline}
        <span aria-hidden="true">·</span>
        <span
          class={deadline.overdue ? "font-medium text-danger-text" : ""}
          data-source={`computed:${deadline.field}`}>{deadline.text}</span
        >
      {/if}
      {#if decision.priority}
        <span
          class="rounded-sm border border-line px-1 text-micro"
          data-source="computed:decisions.priority">{decision.priority}</span
        >
      {/if}
    </p>
  </header>

  {#if decision.blocksCount || decision.requestedBy}
    <p class="mt-1 text-meta text-fg-muted [overflow-wrap:anywhere]">
      {#if decision.blocksCount}
        <span data-source="computed:decisions.blocks"
          >blocks {decision.blocksCount}{#if decision.blocks.length}: {decision.blocks
              .map((block) => block.title || block.ref)
              .join(", ")}{/if}</span
        >
      {/if}
      {#if decision.blocksCount && decision.requestedBy}<span
          aria-hidden="true"
        >
          ·
        </span>{/if}
      {#if decision.requestedBy}
        <span data-author-chip title={decision.requestedBy.id}
          >from {decision.requestedBy.display}</span
        >
      {/if}
    </p>
  {/if}

  {#if decision.body}
    <div
      class="mt-2 text-meta text-fg {full ? '' : 'line-clamp-3'}"
      data-decision-body
    >
      {#if full}
        <MarkdownRenderer content={decision.body} />
      {:else}
        {decision.body}
      {/if}
    </div>
  {/if}

  {#if decision.curation}
    <div class="mt-2 text-meta text-fg-muted" data-decision-curation>
      {#if decision.curation.action}<span class="font-medium text-fg"
          >Proposes: {decision.curation.action}</span
        >{/if}
      {#if decision.curation.targets.length}
        <ul class="mt-1 flex flex-wrap gap-1" data-decision-curation-targets>
          {#each decision.curation.targets as target (target.ref)}
            <li>
              <RefLink
                refValue={target.ref}
                labelHints={target.title ? { [target.ref]: target.title } : {}}
                humanize
                showRaw={false}
              />
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  {/if}

  {#if decision.batch}
    <div class="mt-2 text-meta text-fg-muted" data-decision-batch>
      <div class="flex flex-wrap items-center gap-2">
        <span class="font-medium text-fg" data-decision-batch-count
          >{decision.batch.count} grouped asks</span
        >
        {#if ungroupQueued}
          <MutationSendStatus target={batchTarget} />
        {:else if decision.batch.ungroupable !== false}
          <button
            type="button"
            class="min-h-11 rounded-md border border-line px-2 py-1 text-meta text-fg hover:bg-panel-hover sm:min-h-0"
            data-decision-ungroup
            onclick={ungroup}>Ungroup</button
          >
        {/if}
      </div>
      {#if ungroupError}
        <p class="mt-1 text-danger-text" role="alert">{ungroupError}</p>
      {/if}
      {#if decision.batch.members.length}
        <ul class="mt-1 list-disc pl-4" data-decision-batch-members>
          {#each decision.batch.members as member, index (member.ref || index)}
            <li class="[overflow-wrap:anywhere]">
              {member.title || member.ref}
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  {/if}

  {#if saveError}
    <p class="mt-2 text-meta text-danger-text" role="alert">{saveError}</p>
  {/if}

  {#if failure}
    <div
      class="mt-2 flex flex-wrap items-center gap-2 rounded-md border border-danger bg-danger-soft px-2 py-1.5 text-meta"
      role="alert"
      data-decision-failed
    >
      {#if unnotifiable}
        <span data-decision-unnotifiable
          >The requester can’t be notified. Your answer is saved on this device.</span
        >
        <button
          type="button"
          class="ui-prose-link"
          data-decision-answer-silently
          onclick={() => resendInboxResponse(itemId, { notify_mode: "none" })}
          >Answer without notifying</button
        >
      {:else}
        <span>Couldn’t send your answer. It is saved on this device.</span>
        <button
          type="button"
          class="ui-prose-link"
          onclick={() => retryInboxResponse(itemId)}>Retry</button
        >
      {/if}
      <button
        type="button"
        class="ui-prose-link"
        onclick={() => {
          replying = true;
          editInboxResponse(itemId);
        }}>Edit</button
      >
    </div>
  {/if}

  <div class="mt-3 flex flex-wrap items-center gap-2">
    {#if pmAnswer && (pmSent || pmQueued)}
      {#if pmQueued}
        <MutationSendStatus target={pmTarget} />
      {:else}
        <span class="text-meta text-fg-muted" data-decision-answered
          >Answer sent to your PM.</span
        >
      {/if}
    {:else if elsewhere}
      <a
        class="min-h-11 rounded-md border border-line px-3 py-1.5 text-meta font-medium text-fg hover:bg-panel-hover sm:min-h-0"
        href={elsewhereHref}
        data-decision-open>Review in Access</a
      >
    {:else if confirming}
      <span class="text-meta text-fg" role="status" data-decision-confirm
        >Approving grants this access now.</span
      >
      <button
        type="button"
        class="min-h-11 rounded-md border border-accent-solid bg-accent-solid px-3 py-1.5 text-meta font-medium text-white sm:min-h-0"
        onclick={() => choose(confirming, 0)}>Approve access</button
      >
      <button
        type="button"
        class="min-h-11 rounded-md border border-line px-3 py-1.5 text-meta text-fg sm:min-h-0"
        onclick={() => (confirming = "")}>Cancel</button
      >
    {:else if answered}
      <span class="text-meta text-fg-muted" data-decision-answered
        >{overlay?.status === "pending"
          ? "Answer waiting to send…"
          : "Answered"}{#if overlay?.response_text}: “{overlay.response_text}”{/if}</span
      >
    {:else}
      {#each decision.proposals as proposal, index (index)}
        <button
          type="button"
          class="min-h-11 rounded-md border px-3 py-1.5 text-meta font-medium transition-colors sm:min-h-0 {index ===
          0
            ? 'border-accent-solid bg-accent-solid text-white hover:bg-accent-hover'
            : 'border-line bg-panel text-fg hover:bg-panel-hover'}"
          data-decision-proposal={index + 1}
          data-recommended={index === 0 ? "true" : undefined}
          onclick={() => choose(proposal, index)}
          >{proposal}{#if index === 0 && decision.proposals.length > 1}<span
              class="ml-1 font-normal opacity-80">(recommended)</span
            >{/if}</button
        >
      {/each}
      {#if !structured && !pmAnswer}
        <button
          type="button"
          class="min-h-11 rounded-md border border-line px-3 py-1.5 text-meta text-fg hover:bg-panel-hover sm:min-h-0"
          aria-expanded={replying}
          data-decision-reply-toggle
          onclick={() => (replying = !replying)}>Reply…</button
        >
      {/if}
    {/if}
    <button
      type="button"
      class="ml-auto min-h-11 rounded-md px-2 py-1.5 text-meta text-fg-muted hover:bg-panel-hover hover:text-fg sm:min-h-0"
      title="Copy ref (c)"
      data-decision-copy-ref
      onclick={copyRef}>{copied ? "Copied" : "Copy ref"}</button
    >
  </div>

  <!-- The composer stays mounted (hidden) so its durable draft restores and
       keeps saving whether or not it is open. -->
  <form
    class="mt-2 space-y-2"
    class:hidden={!(replying || full) ||
      answered ||
      structured ||
      pmAnswer ||
      elsewhere}
    onsubmit={sendReply}
    data-anx-save-scope
  >
    <label class="sr-only" for={`decide-reply-${itemId}`}>Reply</label>
    <textarea
      id={`decide-reply-${itemId}`}
      class="w-full rounded-md border border-line bg-bg px-2 py-1.5 text-meta text-fg"
      rows={full ? 4 : 2}
      placeholder="Reply…"
      bind:value={reply}
      use:durableDraft={draftKey}
      data-decision-reply
    ></textarea>
    <div class="flex justify-end">
      <button
        type="submit"
        class="rounded-md border border-line px-3 py-1.5 text-meta font-medium text-fg hover:bg-panel-hover disabled:opacity-50"
        disabled={!reply.trim()}>Send reply</button
      >
    </div>
  </form>
</article>
