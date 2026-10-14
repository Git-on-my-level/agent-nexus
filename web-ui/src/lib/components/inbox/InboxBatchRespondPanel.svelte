<script>
  import { page } from "$app/stores";

  import { NEEDS_CONTEXT_OUTCOME } from "$lib/askDelivery.js";
  import { durableDraft } from "$lib/actions/durableDraft.js";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";

  /**
   * One filed ask batch, one decision pass.
   *
   * A batch inbox item carries N related asks (see POST /asks/batch). This
   * panel renders one section per ask and a single submit: core records all
   * N answers atomically, so there is no partial state to reason about. Each
   * section keeps its own durable draft, so a reload or tab close returns the
   * reader to exactly what they had written.
   *
   * Per section the reader either answers (free text, or a suggestion from
   * the asking agent) or sends that ask back for context. Sections left
   * empty keep the whole submit disabled — a batch answers every member or
   * nothing.
   *
   * Sending is the caller's job (`onSend(answers)`); it queues the batch
   * behind the same undo toast as a single reply.
   */
  let {
    batch = null,
    itemKey = "",
    busy = false,
    /** `{token, drafts}` handed back by an Undo: re-fills sections in place. */
    restore = null,
    onSend,
  } = $props();

  let sections = $derived(Array.isArray(batch?.asks) ? batch.asks : []);

  /** The text per section, keyed by ask ref. */
  let replies = $state({});
  /** Sections sent back for context instead of answered. */
  let contextAsked = $state({});

  let ready = $derived(
    sections.length > 0 &&
      sections.every((section) => {
        const ref = String(section?.request_event_ref ?? "");
        return String(replies[ref] ?? "").trim().length > 0;
      }),
  );

  let blocked = $derived(busy);

  /*
   * A different batch item must not inherit answers written for the previous
   * membership. Plain bookkeeping keeps this from re-running when the reader
   * types: only the item key decides a reset. Text restores through the
   * durable drafts under each section's own key.
   */
  let decidedFor = null;
  $effect(() => {
    if (itemKey === decidedFor) return;
    decidedFor = itemKey;
    replies = Object.fromEntries(
      sections.map((section) => [String(section?.request_event_ref ?? ""), ""]),
    );
    contextAsked = Object.fromEntries(
      sections.map((section) => [
        String(section?.request_event_ref ?? ""),
        false,
      ]),
    );
  });

  /*
   * An Undo of a batch send hands every section's text back. The token makes
   * repeated undos of identical drafts visible too; the outcome rides along so
   * a section the reader had sent back for context reopens in that state.
   */
  let restoredToken = null;
  $effect(() => {
    if (!restore || restore.token === restoredToken) return;
    restoredToken = restore.token;
    const drafts = restore.drafts ?? {};
    const outcomes = restore.outcomes ?? {};
    for (const section of sections) {
      const ref = String(section?.request_event_ref ?? "");
      if (typeof drafts[ref] === "string") {
        replies[ref] = drafts[ref];
      }
      if (typeof outcomes[ref] === "string") {
        contextAsked[ref] = outcomes[ref] === NEEDS_CONTEXT_OUTCOME;
      }
    }
  });

  function sectionDraftKey(ref) {
    return `inbox:${itemKey}:reply:${ref}`;
  }

  function fill(ref, proposal) {
    if (blocked) return;
    replies[ref] = String(proposal);
  }

  function toggleContext(ref) {
    if (blocked) return;
    contextAsked[ref] = !contextAsked[ref];
  }

  function submit(event) {
    event.preventDefault();
    if (!ready || blocked) return;
    const answers = sections.map((section) => {
      const ref = String(section?.request_event_ref ?? "");
      const text = String(replies[ref] ?? "").trim();
      return {
        ask_ref: ref,
        response_text: text,
        outcome: contextAsked[ref] ? NEEDS_CONTEXT_OUTCOME : "answered",
      };
    });
    onSend?.(answers, {
      itemId: itemKey,
      drafts: { ...replies },
    });
    // Core answered everything or nothing; clear the sections the same way a
    // single reply clears its composer. An Undo hands the drafts back.
    replies = Object.fromEntries(
      sections.map((section) => [String(section?.request_event_ref ?? ""), ""]),
    );
    contextAsked = Object.fromEntries(
      sections.map((section) => [
        String(section?.request_event_ref ?? ""),
        false,
      ]),
    );
  }
</script>

<form class="space-y-4" onsubmit={submit} data-inbox-batch-panel>
  <p class="text-micro text-fg-muted" data-inbox-batch-count>
    {sections.length}
    {sections.length === 1 ? "ask" : "asks"} in this batch — one reply per ask, all
    recorded together on submit.
  </p>
  {#each sections as section, index (section.request_event_ref)}
    {@const ref = String(section?.request_event_ref ?? "")}
    <section
      class="space-y-2 rounded-md border border-line bg-bg-soft px-3 py-3"
      data-inbox-batch-section={ref}
    >
      <div class="flex items-baseline justify-between gap-2">
        <h4 class="text-meta font-medium text-fg" data-inbox-batch-title>
          <span class="text-fg-muted">{index + 1}.</span>
          {section.title}
        </h4>
        {#if section.delivery === "none"}
          <span
            class="shrink-0 rounded-sm border border-line px-1.5 text-micro text-fg-muted"
            title="This ask asked to be recorded without waking its agent"
            >no wake</span
          >
        {/if}
      </div>
      {#if section.body}
        <div data-inbox-batch-body>
          <MarkdownRenderer
            source={section.body}
            class="markdown-rendered--ask text-meta leading-relaxed text-fg [overflow-wrap:anywhere]"
            organizationSlug={$page.params.organization}
            workspaceSlug={$page.params.workspace}
          />
        </div>
      {/if}
      {#if Array.isArray(section.response_proposals) && section.response_proposals.length && !contextAsked[ref]}
        <div
          class="flex flex-wrap gap-1.5"
          role="group"
          aria-label="Suggested responses for {section.title}"
        >
          {#each section.response_proposals as proposal, proposalIndex (proposalIndex)}
            <button
              class="rounded-md border border-line bg-bg-soft px-2.5 py-1 text-left text-meta text-fg transition-colors hover:border-line-strong hover:bg-panel-hover disabled:opacity-60"
              type="button"
              disabled={blocked}
              data-inbox-batch-proposal={ref}
              onclick={() => fill(ref, proposal)}
            >
              {#if proposalIndex === 0}
                <span
                  class="mr-1.5 rounded-sm bg-accent-soft px-1 text-micro font-medium text-accent-text"
                  >Recommended</span
                >
              {/if}
              <span class="whitespace-pre-wrap [overflow-wrap:anywhere]"
                >{proposal}</span
              >
            </button>
          {/each}
        </div>
      {/if}
      <textarea
        class="ui-input min-h-16"
        bind:value={replies[ref]}
        disabled={blocked}
        use:durableDraft={sectionDraftKey(ref)}
        placeholder={contextAsked[ref]
          ? "What is missing before this ask can be answered?"
          : `Reply to “${section.title}”…`}
        data-inbox-batch-reply={ref}
        aria-label={contextAsked[ref]
          ? `What is missing for ${section.title}`
          : `Reply to ${section.title}`}
      ></textarea>
      <div>
        <button
          class="text-micro text-fg-muted hover:text-fg disabled:opacity-60"
          type="button"
          disabled={blocked}
          aria-pressed={contextAsked[ref] ? "true" : "false"}
          data-inbox-batch-context={ref}
          onclick={() => toggleContext(ref)}
        >
          {contextAsked[ref]
            ? "Will answer after all (undo)"
            : "Send back for context"}
        </button>
      </div>
    </section>
  {/each}
  <div class="flex flex-wrap items-center gap-2">
    <button
      class="ui-btn-primary"
      type="submit"
      disabled={blocked || !ready}
      data-inbox-batch-submit
      title="Record every answer in this batch"
    >
      Send {sections.length}
      {sections.length === 1 ? "answer" : "answers"}
    </button>
    <p class="text-micro text-fg-subtle" data-inbox-batch-submit-hint>
      Nothing is sent until every ask has a reply.
    </p>
  </div>
</form>
