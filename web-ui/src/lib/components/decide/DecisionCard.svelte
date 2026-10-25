<script>
  import AskPresentation from "./AskPresentation.svelte";
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
    discardInboxResponse,
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
  import SignalBadge from "$lib/components/pm/SignalBadge.svelte";
  import { formatShortcut } from "$lib/keyboardHints.js";

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
  /** Decide's detail pane: the old Inbox reading pane, no card chrome. */
  let pane = $derived(variant === "pane");
  /**
   * Decide on a phone: one card in a scrollable stack. The whole decision is
   * on the card (a long body folds behind "Show more"), suggestions are full
   * width tap targets and one tap answers; Reply opens a composer that pins
   * above the bottom nav and the on-screen keyboard.
   */
  let stack = $derived(variant === "stack");
  /** Variants that read the whole decision rather than a summary row. */
  let rich = $derived(pane || full || stack);
  let bodyEl = $state(null);
  let bodyOpen = $state(false);
  let bodyLong = $state(false);
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
  // Core no longer has this item (404): no retry can succeed.
  let gone = $derived(failure?.status === 404);
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
      decision.requestedBy?.kind === "agent" ? "original" : "none";
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
    pinned = false;
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

  /*
   * Choosing a suggestion, as the old Inbox did. Nothing is highlighted on
   * arrival; the recommendation wears a badge, not a selection. A click is a
   * deliberate act on one option, so it sends after one short flash. A number
   * key is one keystroke from its neighbour, so the first press arms and the
   * same key again sends; Escape disarms.
   */
  const MAX_KEYED = 5;
  const FLASH_MS = 220;
  let armed = $state(-1);
  let flashing = $state(-1);
  let flashTimer;
  let replyEl = $state(null);

  // A different decision in the same pane starts with nothing armed.
  // Effect bookkeeping, deliberately not reactive.
  const armedFor = { ref: "" };
  $effect(() => {
    const key = decision.ref;
    if (key !== armedFor.ref) {
      armedFor.ref = key;
      armed = -1;
      flashing = -1;
      clearTimeout(flashTimer);
    }
  });

  let canChoose = $derived(
    !answered &&
      !elsewhere &&
      !confirming &&
      !(pmAnswer && (pmSent || pmQueued)),
  );

  function reducedMotion() {
    // No matchMedia (a test DOM) also sends at once.
    if (typeof window === "undefined" || !window.matchMedia) return true;
    return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  }

  function commit(index) {
    if (!canChoose || flashing !== -1) return;
    const proposal = decision.proposals[index];
    if (proposal === undefined) return;
    armed = index;
    if (reducedMotion()) {
      void choose(proposal, index);
      return;
    }
    flashing = index;
    clearTimeout(flashTimer);
    flashTimer = setTimeout(() => {
      flashing = -1;
      void choose(proposal, index);
    }, FLASH_MS);
  }

  // A send still flashing when the card goes away is dropped, not sent: the
  // reader has moved on (J, another row) and must not answer the old one.
  $effect(() => () => clearTimeout(flashTimer));

  /** Number key `index` (zero-based): arm, or send when already armed. */
  export function pressProposal(index) {
    if (!canChoose || index < 0 || index >= decision.proposals.length)
      return false;
    if (armed === index) commit(index);
    else armed = index;
    return true;
  }

  /** Escape: true when there was a highlight to clear. */
  export function clearChoice() {
    if (confirming) {
      confirming = "";
      return true;
    }
    if (armed === -1) return false;
    armed = -1;
    return true;
  }

  /** `r`: open the composer and put the caret in it. */
  export async function focusReply() {
    if (structured || pmAnswer || elsewhere || answered) return false;
    replying = true;
    await Promise.resolve();
    replyEl?.focus();
    return true;
  }

  export function copy() {
    void copyRef();
  }

  // A stacked card folds a long body; measure whether there is more to show.
  $effect(() => {
    if (!stack || !bodyEl || bodyOpen) return;
    const measure = () =>
      (bodyLong = bodyEl.scrollHeight > bodyEl.clientHeight + 4);
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(bodyEl);
    return () => observer.disconnect();
  });

  /*
   * Phone: while the reader is typing a reply, the composer (field, Send,
   * Cancel) is a bar fixed to the bottom of the screen, directly above the
   * bottom nav or the on-screen keyboard, whichever reaches higher. Both are
   * measured — the nav's real height, and the keyboard as the part of the
   * layout viewport the visual viewport has lost — so nothing depends on how
   * a browser treats a scroller's padding. The field being typed into is
   * inside the bar, so the bar can never cover it. The form keeps the bar's
   * height in flow, so the card does not jump when it pins.
   */
  const PHONE_QUERY = "(max-width: 1023px)";
  let pinned = $state(false);
  let pinBottom = $state(0);
  let barSpace = $state(0);
  let barEl = $state(null);

  function onPhone() {
    return (
      typeof window !== "undefined" &&
      Boolean(window.matchMedia?.(PHONE_QUERY).matches)
    );
  }

  function measurePin() {
    const viewport = window.visualViewport;
    const keyboard = viewport
      ? Math.max(0, window.innerHeight - viewport.height - viewport.offsetTop)
      : 0;
    const nav = document.querySelector(".shell-bottom-nav");
    const navHeight =
      nav && getComputedStyle(nav).display !== "none"
        ? nav.getBoundingClientRect().height
        : 0;
    pinBottom = Math.round(Math.max(navHeight, keyboard));
  }

  function onComposerFocusIn() {
    if (pinned || !(stack || full) || !onPhone()) return;
    barSpace = barEl?.offsetHeight ?? 0;
    measurePin();
    pinned = true;
  }

  function onComposerFocusOut(event) {
    if (event.currentTarget.contains(event.relatedTarget)) return;
    pinned = false;
  }

  /**
   * A tap on Send or Cancel must not blur the field and unpin the bar.
   * `mousedown`, not `pointerdown`: cancelling a pointerdown suppresses the
   * compatibility click a touch tap relies on, so Send would never submit.
   */
  function keepFocus(event) {
    if (pinned) event.preventDefault();
  }

  $effect(() => {
    if (!pinned) return;
    const viewport = window.visualViewport;
    window.addEventListener("resize", measurePin);
    viewport?.addEventListener("resize", measurePin);
    viewport?.addEventListener("scroll", measurePin);
    return () => {
      window.removeEventListener("resize", measurePin);
      viewport?.removeEventListener("resize", measurePin);
      viewport?.removeEventListener("scroll", measurePin);
    };
  });

  function onReplyKeydown(event) {
    if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      send(reply);
    } else if (event.key === "Escape" && !reply.trim()) {
      event.preventDefault();
      replying = false;
      event.currentTarget.blur();
    }
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
  class="decision min-w-0 {pane
    ? 'space-y-4 p-4 sm:p-5'
    : stack || full
      ? `space-y-3 rounded-md border bg-panel px-4 py-4 ${focused ? 'border-accent' : 'border-line-subtle'}`
      : `rounded-md border bg-panel px-3 py-3 sm:px-4 ${focused ? 'border-accent' : 'border-line-subtle'}`}"
  data-decision-ref={decision.ref}
  data-decision-kind={decision.kind}
  data-variant={variant}
  tabindex={pane ? -1 : 0}
  onkeydown={onKeydown}
  aria-label={decision.title}
>
  <header class="space-y-1.5">
    <p
      class="flex flex-wrap items-center gap-x-2 gap-y-1 text-micro text-fg-muted"
    >
      <span class="ui-label mb-0"
        >{DECISION_KIND_LABELS[decision.kind] ?? decision.kind}</span
      >
      {#if decision.requestedBy}
        <span aria-hidden="true">·</span>
        <span data-author-chip title={decision.requestedBy.id}
          >from <span class="text-fg">{decision.requestedBy.display}</span
          ></span
        >
      {/if}
      {#if workspace}<span aria-hidden="true">·</span><span>{workspace}</span
        >{/if}
      {#if age !== null}
        <span aria-hidden="true">·</span>
        <span class="tabular-nums">waiting {formatSpan(age)}</span>
      {/if}
      {#if deadline}
        <span aria-hidden="true">·</span>
        <span
          class={deadline.overdue ? "font-medium text-danger-text" : ""}
          data-source={`computed:${deadline.field}`}>{deadline.text}</span
        >
      {/if}
      {#if decision.priority}
        <SignalBadge
          tone={decision.priority === "p0" || decision.priority === "p1"
            ? "warn"
            : "neutral"}
          ><span data-source="computed:decisions.priority"
            >{decision.priority.toUpperCase()}</span
          ></SignalBadge
        >
      {/if}
    </p>
    <h3
      class="{rich
        ? 'text-subtitle'
        : 'text-body font-medium'} text-fg [overflow-wrap:anywhere]"
      data-decision-title
    >
      {#if full || pane}
        {decision.title}
      {:else}
        <a
          class="hover:underline"
          href={href(`/decide/${encodeURIComponent(itemId)}`)}
          data-decision-link>{decision.title}</a
        >
      {/if}
    </h3>
    {#if decision.blocksCount}
      <p
        class="text-meta text-fg-muted [overflow-wrap:anywhere]"
        data-source="computed:decisions.blocks"
      >
        Blocks {decision.blocksCount}{#if decision.blocks.length}: {decision.blocks
            .map((block) => block.title || block.ref)
            .join(", ")}{/if}
      </p>
    {/if}
  </header>

  {#if decision.situation || decision.impact || decision.recommendation || decision.alternatives?.length || decision.if_no_answer || decision.details || decision.evidence?.length}
    <AskPresentation
      {decision}
      organizationSlug={organizationSlug || $page.params.organization}
      workspaceSlug={workspaceSlug || $page.params.workspace}
    />
  {:else if decision.body}
    <div
      class="text-meta leading-relaxed text-fg [overflow-wrap:anywhere] {rich
        ? ''
        : 'mt-2 line-clamp-3'} {stack && !bodyOpen ? 'decision-fold' : ''}"
      class:decision-fold--faded={stack && !bodyOpen && bodyLong}
      bind:this={bodyEl}
      data-decision-body
    >
      <MarkdownRenderer
        source={decision.body}
        class="markdown-rendered--ask"
        inline={!rich}
        organizationSlug={organizationSlug || $page.params.organization}
        workspaceSlug={workspaceSlug || $page.params.workspace}
      />
    </div>
    {#if stack && (bodyLong || bodyOpen)}
      <button
        type="button"
        class="-mt-1 inline-flex min-h-11 items-center text-meta font-medium text-accent-text"
        aria-expanded={bodyOpen}
        data-decision-more
        onclick={() => (bodyOpen = !bodyOpen)}
        >{bodyOpen ? "Show less" : "Show more"}</button
      >
    {/if}
  {/if}

  {#if decision.curation}
    <div
      class="rounded-md border border-line-subtle bg-bg-soft px-3 py-2 text-meta text-fg-muted {rich
        ? ''
        : 'mt-2'}"
      data-decision-curation
    >
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
    <div
      class="rounded-md border border-line-subtle bg-bg-soft px-3 py-2 text-meta text-fg-muted {rich
        ? ''
        : 'mt-2'}"
      data-decision-batch
    >
      <div class="flex flex-wrap items-center gap-2">
        <span class="font-medium text-fg" data-decision-batch-count
          >{decision.batch.count} grouped asks</span
        >
        <span class="text-micro">One answer closes all of them.</span>
        {#if ungroupQueued}
          <MutationSendStatus target={batchTarget} />
        {:else if decision.batch.ungroupable !== false}
          <button
            type="button"
            class="ml-auto text-micro text-fg-muted hover:text-fg hover:underline"
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
              <details data-decision-batch-member>
                <summary>{member.title || member.ref}</summary>
                <AskPresentation
                  decision={member}
                  organizationSlug={organizationSlug ||
                    $page.params.organization}
                  workspaceSlug={workspaceSlug || $page.params.workspace}
                />
              </details>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  {/if}

  {#if saveError}
    <p class="text-meta text-danger-text" role="alert">{saveError}</p>
  {/if}

  {#if failure}
    <div
      class="flex flex-wrap items-center gap-2 rounded-md border border-danger bg-danger-soft px-2 py-1.5 text-meta {rich
        ? ''
        : 'mt-2'}"
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
      {:else if gone}
        <span data-decision-gone
          >This question no longer exists, so your answer can’t be sent.</span
        >
        <button
          type="button"
          class="ui-prose-link"
          data-decision-copy-answer
          onclick={() => void copyText(failure.response_text || "")}
          >Copy answer</button
        >
        <button
          type="button"
          class="ui-prose-link"
          data-decision-discard
          onclick={() => {
            discardInboxResponse(itemId);
            void refreshBrief();
          }}>Discard</button
        >
      {:else}
        <span>Couldn’t send your answer. It is saved on this device.</span>
        <button
          type="button"
          class="ui-prose-link"
          onclick={() => retryInboxResponse(itemId)}>Retry</button
        >
      {/if}
      {#if !gone}
        <button
          type="button"
          class="ui-prose-link"
          onclick={() => {
            replying = true;
            editInboxResponse(itemId);
          }}>Edit</button
        >
      {/if}
    </div>
  {/if}

  <div class={rich ? "space-y-3" : "mt-3 space-y-2"}>
    {#if pmAnswer && (pmSent || pmQueued)}
      {#if pmQueued}
        <MutationSendStatus target={pmTarget} />
      {:else}
        <p class="text-meta text-fg-muted" data-decision-answered>
          Answer sent to your PM.
        </p>
      {/if}
    {:else if elsewhere}
      <a class="ui-btn-secondary" href={elsewhereHref} data-decision-open
        >Review in Access</a
      >
    {:else if confirming}
      <div
        class="space-y-2 rounded-md border border-line bg-bg-soft px-3 py-2.5"
        role="group"
        aria-label="Confirm this grant"
      >
        <p
          class="text-meta font-medium text-fg"
          role="status"
          data-decision-confirm
        >
          Approving grants this access now.
        </p>
        <div class="flex flex-wrap gap-2">
          <button
            type="button"
            class="ui-btn-primary"
            onclick={() => choose(confirming, 0)}>Approve access</button
          >
          <button
            type="button"
            class="ui-btn-secondary"
            onclick={() => (confirming = "")}>Cancel</button
          >
        </div>
      </div>
    {:else if answered}
      <p class="text-meta text-fg-muted" data-decision-answered>
        {overlay?.status === "pending"
          ? "Answer waiting to send…"
          : "Answered"}{#if overlay?.response_text}: “{overlay.response_text}”{/if}
      </p>
    {:else if decision.proposals.length}
      <div role="group" aria-labelledby={`decide-suggested-${itemId}`}>
        {#if pane || full}
          <p class="ui-label" id={`decide-suggested-${itemId}`}>
            Suggested responses
          </p>
        {:else}
          <p class="sr-only" id={`decide-suggested-${itemId}`}>
            Suggested responses
          </p>
        {/if}
        <ul class={stack ? "space-y-2" : "space-y-1.5"}>
          {#each decision.proposals as proposal, index (index)}
            {@const selected = armed === index}
            <li>
              <button
                type="button"
                class="proposal flex min-h-11 w-full items-center gap-2.5 rounded-md border px-3 py-2 text-left text-fg transition-colors disabled:opacity-60 lg:min-h-0 lg:items-start {stack
                  ? 'text-body'
                  : 'text-meta'} {selected
                  ? 'border-accent bg-accent-soft ring-1 ring-accent'
                  : 'border-line-subtle bg-bg-soft hover:border-line hover:bg-panel-hover'}"
                class:proposal--sending={flashing === index}
                disabled={flashing !== -1 && flashing !== index}
                data-decision-proposal={index + 1}
                data-recommended={index === 0 ? "true" : undefined}
                data-decision-armed={selected ? "true" : undefined}
                aria-pressed={selected ? "true" : "false"}
                aria-keyshortcuts={(pane || full) && index < MAX_KEYED
                  ? String(index + 1)
                  : undefined}
                onclick={() => commit(index)}
              >
                {#if (pane || full) && index < MAX_KEYED}
                  <kbd
                    class="mt-px hidden w-4 shrink-0 rounded-sm border border-line text-center font-mono text-micro leading-4 text-fg-muted sm:inline-block"
                    >{index + 1}</kbd
                  >
                {/if}
                <span
                  class="min-w-0 flex-1 whitespace-pre-wrap [overflow-wrap:anywhere]"
                  >{proposal}</span
                >
                {#if index === 0 && decision.proposals.length > 1}
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

    <!-- The composer stays mounted (hidden) so its durable draft restores and
         keeps saving whether or not it is open. -->
    <form
      class="space-y-2"
      class:hidden={!(replying || full || pane) ||
        answered ||
        structured ||
        pmAnswer ||
        elsewhere}
      onsubmit={sendReply}
      onfocusin={onComposerFocusIn}
      onfocusout={onComposerFocusOut}
      style:min-height={pinned ? `${barSpace}px` : undefined}
      data-anx-save-scope
    >
      <label
        class={pane || full ? "ui-label" : "sr-only"}
        for={`decide-reply-${itemId}`}>Reply</label
      >
      <div
        class="space-y-2"
        class:decide-composer-bar--pinned={pinned}
        style:bottom={pinned ? `${pinBottom}px` : undefined}
        bind:this={barEl}
        data-decision-composer
        data-pinned={pinned ? "true" : undefined}
      >
        <textarea
          id={`decide-reply-${itemId}`}
          class="ui-input {pane || full ? 'min-h-20' : 'min-h-16'}"
          placeholder={pane || full ? "Write your own answer…" : "Reply…"}
          bind:value={reply}
          bind:this={replyEl}
          use:durableDraft={draftKey}
          onkeydown={onReplyKeydown}
          aria-keyshortcuts="R"
          data-decision-reply
        ></textarea>
        <div class="flex flex-wrap items-center gap-2">
          <button
            type="submit"
            class="ui-btn-primary max-lg:min-h-11"
            title={`Send reply (${formatShortcut("Enter")})`}
            onmousedown={keepFocus}
            disabled={!reply.trim()}>Send reply</button
          >
          {#if stack}
            <button
              type="button"
              class="ui-btn-secondary max-lg:min-h-11"
              data-decision-reply-cancel
              onmousedown={keepFocus}
              onclick={() => {
                replying = false;
                pinned = false;
              }}>Cancel</button
            >
          {/if}
        </div>
      </div>
    </form>
  </div>
  <div class="flex flex-wrap items-center gap-2 {rich ? '' : 'mt-2'}">
    {#if !(pane || full) && !(stack && replying) && !answered && !structured && !pmAnswer && !elsewhere && !confirming}
      <button
        type="button"
        class="inline-flex items-center text-meta text-fg-muted hover:text-fg max-lg:min-h-11 {stack
          ? 'max-lg:rounded-md max-lg:border max-lg:border-line max-lg:px-4 max-lg:font-medium max-lg:text-fg'
          : ''}"
        aria-expanded={replying}
        data-decision-reply-toggle
        onclick={() => {
          replying = !replying;
          if (replying && stack) void focusReply();
        }}>{stack ? "Reply" : "Reply…"}</button
      >
    {/if}
    <button
      type="button"
      class="ml-auto inline-flex items-center text-micro text-fg-muted hover:text-fg max-lg:min-h-11 max-lg:px-1"
      title="Copy ref (c)"
      data-decision-copy-ref
      onclick={copyRef}>{copied ? "Copied" : "Copy ref"}</button
    >
  </div>
</article>

<style>
  /* A stacked card's long body folds to about eight lines. */
  .decision-fold {
    max-height: 10.5rem;
    overflow: hidden;
  }
  .decision-fold--faded {
    mask-image: linear-gradient(to bottom, black 70%, transparent);
  }
  /* Phone, while typing: the composer bar above the nav or keyboard. */
  .decide-composer-bar--pinned {
    position: fixed;
    left: 0;
    right: 0;
    z-index: 30;
    padding: 0.5rem 1rem;
    border-top: 1px solid var(--line);
    background: var(--panel);
    box-shadow: 0 -8px 16px -10px rgb(0 0 0 / 0.45);
  }
  /*
   * One pulse, 220ms, so a click reads as "this one, going out" rather than a
   * row that vanished. Reduced motion sends without it.
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
    animation: proposal-send 220ms ease-out 1;
  }
  @media (prefers-reduced-motion: reduce) {
    .proposal--sending {
      animation: none;
    }
  }
</style>
