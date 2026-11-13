<script>
  /**
   * A batch decision. A filed batch whose questions carry their own
   * proposals is answered here as numbered questions: the shared context
   * stated once on top, each question with its options (recommendation
   * marked) and a free-text "other", and one submit. Questions left
   * unanswered stay open: the submit is partial and core keeps the item with
   * only those. Anything else (a curation batch, an older payload) is the
   * grouped list answered by the card's own proposals, with Ungroup when the
   * group is a curation batch.
   */
  import AskPresentation from "./AskPresentation.svelte";
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  import { durableDraft } from "$lib/actions/durableDraft.js";
  import { refreshBrief } from "$lib/briefStore.js";
  import { batchAnswers } from "$lib/decideBatch.js";
  import {
    captureInboxResponseBinding,
    inboxResponseOverlay,
    onInboxResponseCommitted,
    queueInboxResponse,
  } from "$lib/inboxResponseQueue.js";
  import {
    enqueueCurationUngroup,
    mutationOutbox,
  } from "$lib/mutationOutbox.js";

  let {
    decision,
    itemId,
    rich = false,
    disabled = false,
    href,
    organizationSlug,
    workspaceSlug,
    binding = null,
    onAnswered = undefined,
  } = $props();

  let batch = $derived(decision.batch);
  let questions = $derived(batch.questions ? batch.members : []);
  let context = $derived(batch.context);

  /** Chosen proposal indexes and own words, per ask ref. */
  let choices = $state({});
  let other = $state({});
  let body = $derived(batchAnswers(questions, choices, other));
  let answeredCount = $derived(body?.answers.length ?? 0);

  function number(member, index) {
    return member.position || index + 1;
  }

  function toggle(member, index) {
    if (disabled) return;
    const current = choices[member.eventRef] ?? [];
    const next = current.includes(index)
      ? current.filter((value) => value !== index)
      : member.multiSelect
        ? [...current, index]
        : [index];
    choices = { ...choices, [member.eventRef]: next };
  }

  function submit() {
    if (!body || disabled) return;
    const partial = body.partial === true;
    const left = questions.length - body.answers.length;
    const answeredDecision = decision;
    queueInboxResponse({
      itemId,
      item: { id: itemId, kind: decision.kind, title: decision.title },
      binding: binding ?? captureInboxResponseBinding(),
      crossWorkspace: Boolean(binding),
      request: body,
      message: partial
        ? `${body.answers.length} answered; ${left} still open`
        : "Answers sent",
      restore: {
        origin: "decide",
        drafts: Object.fromEntries(
          body.answers.map((answer) => [answer.ask_ref, answer.response_text]),
        ),
      },
    });
    if (!partial) onAnswered?.(answeredDecision);
  }

  // After a partial answer core keeps the item open with the rest: drop the
  // "answered" overlay as soon as it commits and reload the remaining asks.
  $effect(() => {
    if (!batch.questions) return;
    const id = itemId;
    return onInboxResponseCommitted((committedId, result) => {
      if (committedId !== id || !(Number(result?.open_ask_count) > 0)) return;
      inboxResponseOverlay.update((overlay) => {
        const next = { ...overlay };
        delete next[id];
        return next;
      });
      choices = {};
      void refreshBrief();
    });
  });

  let batchTarget = $derived(`curation-batch:${batch.key}`);
  let ungroupQueued = $derived(
    ($mutationOutbox ?? []).some((entry) => entry.target === batchTarget),
  );
  let ungroupError = $state("");

  async function ungroup() {
    ungroupError = "";
    try {
      // Persisted before it is sent; a storage failure stops the send.
      await enqueueCurationUngroup({
        key: batch.key,
        target: batchTarget,
        href: href("/decide"),
        title: "Ungroup asks",
        preview: `Ungroup ${batch.count} grouped asks`,
      });
    } catch {
      ungroupError =
        "Couldn’t save this on your device, so it was not sent. Try again.";
    }
  }

  $effect(() => {
    const target = batchTarget;
    const confirmed = (event) => {
      if (event.detail?.target === target) {
        void refreshBrief();
        onAnswered?.(decision);
      }
    };
    window.addEventListener("anx:mutation-confirmed", confirmed);
    return () =>
      window.removeEventListener("anx:mutation-confirmed", confirmed);
  });
</script>

{#if batch.questions}
  <section
    class="space-y-3 {rich ? '' : 'mt-2'}"
    data-decision-batch
    data-decision-batch-questions
    aria-label="{questions.length} questions"
  >
    {#if context}
      <div
        class="space-y-2 rounded-md border border-line-subtle bg-bg-soft px-3 py-2 text-meta text-fg [overflow-wrap:anywhere]"
        data-decision-batch-context
      >
        {#if context.situation}<p class="whitespace-pre-wrap">
            {context.situation}
          </p>{/if}
        {#if context.impact}<p class="whitespace-pre-wrap">
            <span class="font-medium">Why it matters:</span>
            {context.impact}
          </p>{/if}
        {#if context.details || context.evidence.length}
          <details data-decision-batch-details>
            <summary class="text-fg-muted">Details and evidence</summary>
            {#if context.details}<p class="mt-1 whitespace-pre-wrap">
                {context.details}
              </p>{/if}
            {#if context.evidence.length}
              <ul class="mt-1 list-disc pl-4">
                {#each context.evidence as link (link.url)}
                  <li>
                    <a
                      class="ui-prose-link"
                      href={link.url}
                      rel="noopener noreferrer"
                      target="_blank">{link.label || link.url}</a
                    >
                  </li>
                {/each}
              </ul>
            {/if}
          </details>
        {/if}
      </div>
    {/if}
    <ol class="space-y-3">
      {#each questions as member, index (member.eventRef)}
        {@const picked = choices[member.eventRef] ?? []}
        <li data-decision-batch-question={number(member, index)}>
          <fieldset class="space-y-1.5" {disabled}>
            <legend
              class="text-body font-medium text-fg [overflow-wrap:anywhere]"
              >{number(member, index)}. {member.title}{#if member.multiSelect}
                <span class="text-micro font-normal text-fg-muted"
                  >(pick any)</span
                >{/if}</legend
            >
            {#if member.note}<p class="text-meta text-fg-muted">
                {member.note}
              </p>{/if}
            {#if member.situation || member.impact || member.recommendation?.why || member.alternatives?.length || member.details}
              <details data-decision-batch-member>
                <summary class="text-micro text-fg-muted"
                  >More about this</summary
                >
                <AskPresentation
                  decision={member}
                  allowRevisionToggle
                  {organizationSlug}
                  {workspaceSlug}
                />
              </details>
            {/if}
            <ul class="space-y-1.5">
              {#each member.proposals as proposal, option (option)}
                {@const chosen = picked.includes(option)}
                <li>
                  <button
                    type="button"
                    class="flex min-h-11 w-full items-center gap-2.5 rounded-md border px-3 py-2 text-left text-meta text-fg lg:min-h-0 {chosen
                      ? 'border-accent bg-accent-soft ring-1 ring-accent'
                      : 'border-line-subtle bg-bg-soft hover:border-line hover:bg-panel-hover'}"
                    data-decision-batch-option={`${number(member, index)}${String.fromCharCode(97 + option)}`}
                    data-recommended={option === 0 ? "true" : undefined}
                    aria-pressed={chosen ? "true" : "false"}
                    onclick={() => toggle(member, option)}
                  >
                    <span
                      class="w-4 shrink-0 font-mono text-micro text-fg-muted"
                      >{String.fromCharCode(97 + option)}</span
                    >
                    <span class="min-w-0 flex-1 [overflow-wrap:anywhere]"
                      >{proposal}</span
                    >
                    {#if option === 0 && member.proposals.length > 1}
                      <span
                        class="shrink-0 rounded-sm bg-accent-soft px-1.5 text-micro font-medium text-accent-text"
                        >Recommended</span
                      >
                    {/if}
                  </button>
                </li>
              {/each}
            </ul>
            <label class="sr-only" for={`batch-other-${itemId}-${index}`}
              >Other answer to question {number(member, index)}</label
            >
            <input
              id={`batch-other-${itemId}-${index}`}
              class="ui-input"
              placeholder="Other: answer in your own words"
              bind:value={other[member.eventRef]}
              use:durableDraft={`inbox:${itemId}:reply:${member.eventRef}`}
              data-decision-batch-other={number(member, index)}
            />
          </fieldset>
        </li>
      {/each}
    </ol>
    <div class="flex flex-wrap items-center gap-2">
      <button
        type="button"
        class="ui-btn-primary max-lg:min-h-11"
        disabled={disabled || !body}
        onclick={submit}
        data-decision-batch-submit
        >{body?.partial
          ? `Submit ${answeredCount} of ${questions.length}`
          : "Submit answers"}</button
      >
      <span class="text-micro text-fg-muted" data-decision-batch-progress
        >{answeredCount} of {questions.length} answered{#if body?.partial}
          · the rest stay open{/if}</span
      >
    </div>
  </section>
{:else}
  <div
    class="rounded-md border border-line-subtle bg-bg-soft px-3 py-2 text-meta text-fg-muted {rich
      ? ''
      : 'mt-2'}"
    data-decision-batch
  >
    <div class="flex flex-wrap items-center gap-2">
      <span class="font-medium text-fg" data-decision-batch-count
        >{batch.count} grouped asks</span
      >
      <span class="text-micro">One answer closes all of them.</span>
      {#if ungroupQueued}
        <MutationSendStatus target={batchTarget} />
      {:else if batch.ungroupable !== false}
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
    {#if batch.members.length}
      <ul class="mt-1 list-disc pl-4" data-decision-batch-members>
        {#each batch.members as member, index (member.ref || index)}
          <li class="[overflow-wrap:anywhere]">
            <details data-decision-batch-member>
              <summary>{member.title || member.ref}</summary>
              <AskPresentation
                decision={member}
                allowRevisionToggle
                {organizationSlug}
                {workspaceSlug}
              />
            </details>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
{/if}
