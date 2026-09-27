<script>
  import { formatAbsoluteDateTime, formatTimestamp } from "$lib/formatDate";

  /**
   * What the item is blocking and the latest progress note around it, so the
   * reader can answer without opening the task first.
   *
   * `presence` is a slot for the agent's own presence note (set with
   * `anx work note`) once core serves one; it renders between the subject and
   * the last message.
   */
  let {
    relation = "On",
    subject = null,
    subjectHref = "",
    note = null,
    noteAuthor = "",
    loading = false,
    presence = null,
  } = $props();
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
    {/if}
    {#if presence}
      {@render presence()}
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
