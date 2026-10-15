<script>
  /**
   * One initiative, drawn once for both Watch views (`variant="row"` in the
   * list, `"card"` in the by-stage columns). Everything about state, progress,
   * promise and evidence is a brief field and carries
   * `data-source="computed:<field>"` (I1). The only authored text is the
   * quoted note, with its author chip.
   */
  import { page } from "$app/stores";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { initiativeLine } from "$lib/brief.js";
  import { formatAgo } from "$lib/time/format.js";

  let {
    initiative,
    variant = "row",
    now = Date.now(),
    /** Extra content under the card (the move menu). */
    footer = null,
  } = $props();

  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  let line = $derived(initiativeLine(initiative, now));
  let noteAge = $derived(formatAgo(initiative.note?.at, now));
  let decideHref = $derived(
    workspaceHref(`/decide?initiative=${encodeURIComponent(initiative.ref)}`),
  );
  const STATE_TEXT = {
    past_promise: "Past promise",
    moving: "Moving",
    no_promise: "No promise",
    done: "Done",
  };
</script>

<div
  class="min-w-0 {variant === 'card'
    ? 'rounded-md border border-border bg-panel p-2.5'
    : 'border-b border-border px-3 py-2.5 last:border-b-0'}"
  data-watch-row={initiative.ref}
  data-watch-variant={variant}
  data-watch-state={initiative.state}
>
  <div class="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
    <a
      class="min-w-0 max-w-full text-meta font-medium text-fg [overflow-wrap:anywhere] hover:underline"
      href={workspaceHref(`/initiatives/${encodeURIComponent(initiative.id)}`)}
      data-watch-title>{initiative.title}</a
    >
    {#if variant === "card"}
      <span
        class="text-micro text-fg-muted"
        data-source="computed:initiatives.state"
        >{STATE_TEXT[initiative.state]}</span
      >
    {/if}
    {#if initiative.possiblyMoot}
      <span
        class="rounded border border-border px-1.5 text-micro text-fg-muted"
        title={initiative.possiblyMoot.reason}
        data-source="computed:initiatives.state">possibly moot</span
      >
    {/if}
    <span class="ml-auto flex shrink-0 items-center gap-2">
      {#if initiative.progress}
        <span
          class="text-micro tabular-nums text-fg-muted"
          data-source="computed:initiatives.progress"
          >{initiative.progress.done}/{initiative.progress.total}</span
        >
      {/if}
      {#if initiative.blockedCount > 0}
        <a
          class="rounded bg-danger-soft px-1.5 text-micro text-danger-text hover:underline"
          href={decideHref}
          data-source="computed:initiatives.blocked_count"
          >⚑{initiative.blockedCount}<span class="sr-only"> blocked</span></a
        >
      {/if}
      {#if initiative.waitingOnYouCount > 0}
        <a
          class="rounded bg-warn-soft px-1.5 text-micro text-warn-text hover:underline"
          href={decideHref}
          data-source="computed:initiatives.waiting_on_you_count"
          >{initiative.waitingOnYouCount} waiting on you</a
        >
      {/if}
    </span>
  </div>
  {#if line.length}
    <p class="mt-0.5 flex flex-wrap gap-x-1.5 text-micro text-fg-muted">
      {#each line as part, i (part.field)}
        {#if i > 0}<span aria-hidden="true">·</span>{/if}<span
          data-source="computed:{part.field}">{part.text}</span
        >
      {/each}
    </p>
  {/if}
  {#if initiative.note}
    <blockquote
      class="mt-1 border-l-2 border-border pl-2 text-micro text-fg [overflow-wrap:anywhere]"
      data-watch-note
    >
      “{initiative.note.text}”
      <span class="text-fg-muted" data-author-chip
        >{initiative.note.author?.display || "Unknown"}{noteAge
          ? ` · ${noteAge}`
          : ""}</span
      >
    </blockquote>
  {/if}
  {@render footer?.()}
</div>
