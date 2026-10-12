<script>
  import { agentRegistry, findAgentSummary } from "$lib/actorSession";
  import AgentPresenceLine from "$lib/components/agents/AgentPresenceLine.svelte";
  import WorkSummary from "$lib/components/WorkSummary.svelte";
  import { subjectTitleRepeatsAsk } from "$lib/inboxItemText.js";
  import Time from "$lib/time/Time.svelte";

  /**
   * What the item is blocking and the latest progress note around it, so the
   * reader can answer without opening the task first.
   *
   * `presenceActorId` names the requester; when it is an agent with a
   * presence note (`anx work note`), that note renders between the subject
   * and the last message.
   *
   * `askText` is the item's own title. `anx ask` auto-creates the card it
   * blocks and titles it with the ask, so this box used to reprint the whole
   * ask directly beneath the heading. When that is what the card title is,
   * the box says the card exists and links to it; the ask is never written
   * twice.
   */
  let {
    relation = "On",
    subject = null,
    subjectHref = "",
    askText = "",
    note = null,
    noteAuthor = "",
    loading = false,
    presenceActorId = "",
    now = Date.now(),
  } = $props();

  let presence = $derived(
    Boolean(
      String(
        findAgentSummary(presenceActorId, $agentRegistry)?.last_progress_note ??
          "",
      ).trim(),
    ),
  );
  let repeatsAsk = $derived(
    Boolean(subject) && subjectTitleRepeatsAsk(subject?.title, askText),
  );
  let subjectLabel = $derived(
    repeatsAsk ? "Card created for this ask" : String(subject?.title ?? ""),
  );
</script>

{#if subject || note || loading || presence}
  <div
    class="space-y-1 rounded-md border border-line-subtle bg-bg-soft px-3 py-2 text-micro text-fg-muted"
    data-inbox-context
  >
    {#if subject}
      <p
        class="line-clamp-2 [overflow-wrap:anywhere]"
        data-inbox-context-subject
        data-inbox-context-repeats-ask={repeatsAsk ? "true" : undefined}
      >
        {#if !repeatsAsk}{relation}{" "}{/if}{#if subjectHref}<a
            class="font-medium text-fg hover:underline"
            href={subjectHref}
            data-inbox-shortcut="open">{subjectLabel}</a
          >{:else}<span class="font-medium text-fg">{subjectLabel}</span
          >{/if}{#if subject.status}<span class="text-fg-subtle">{" · "}</span
          >{subject.status}{/if}
      </p>
      {#if subject.summary}
        <!-- The same renderer the Tasks table and the board use. The box used
             to print the stored phase, so an ask could say "In progress"
             about a card the table called blocked. -->
        <WorkSummary
          summary={subject.summary}
          density="row"
          title={subjectLabel}
          {now}
        />
      {/if}
    {/if}
    {#if presence}
      <AgentPresenceLine actorId={presenceActorId} />
    {/if}
    {#if note}
      <p class="line-clamp-2 [overflow-wrap:anywhere]" data-inbox-last-note>
        <span class="text-fg">{noteAuthor || "Someone"}</span>
        <span class="text-fg-subtle">{" · "}</span><Time
          value={note.ts}
          {now}
        />: “{note.text}”
      </p>
    {:else if loading}
      <p class="text-fg-subtle">Reading the latest note…</p>
    {/if}
  </div>
{/if}
