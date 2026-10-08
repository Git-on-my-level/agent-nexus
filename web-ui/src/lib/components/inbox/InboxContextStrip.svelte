<script>
  import { agentRegistry, findAgentSummary } from "$lib/actorSession";
  import AgentPresenceLine from "$lib/components/agents/AgentPresenceLine.svelte";
  import WorkSummary from "$lib/components/WorkSummary.svelte";
  import { formatAbsoluteDateTime, formatTimestamp } from "$lib/formatDate";

  /**
   * What the item is blocking and the latest progress note around it, so the
   * reader can answer without opening the task first.
   *
   * `presenceActorId` names the requester; when it is an agent with a
   * presence note (`anx work note`), that note renders between the subject
   * and the last message.
   */
  let {
    relation = "On",
    subject = null,
    subjectHref = "",
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
</script>

{#if subject || note || loading || presence}
  <div
    class="space-y-1 rounded-md border border-line-subtle bg-bg-soft px-3 py-2 text-micro text-fg-muted"
    data-inbox-context
  >
    {#if subject}
      <p class="[overflow-wrap:anywhere]">
        {relation}
        {#if subjectHref}
          <a
            class="font-medium text-fg hover:underline"
            href={subjectHref}
            data-inbox-shortcut="open">{subject.title}</a
          >
        {:else}
          <span class="font-medium text-fg">{subject.title}</span>
        {/if}
        {#if subject.status}<span class="text-fg-subtle">{" · "}</span
          >{subject.status}{/if}
      </p>
      {#if subject.summary}
        <!-- The same renderer the Tasks table and the board use. The box used
             to print the stored phase, so an ask could say "In progress"
             about a card the table called blocked. -->
        <WorkSummary
          summary={subject.summary}
          density="row"
          title={subject.title}
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
        <span class="text-fg-subtle">{" · "}</span><time
          datetime={note.ts}
          title={formatAbsoluteDateTime(note.ts)}
          >{formatTimestamp(note.ts)}</time
        >: “{note.text}”
      </p>
    {:else if loading}
      <p class="text-fg-subtle">Reading the latest note…</p>
    {/if}
  </div>
{/if}
