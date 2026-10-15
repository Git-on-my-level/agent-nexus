<script>
  /**
   * One initiative, drawn once for both Watch views (`variant="row"` in the
   * list, `"card"` in the by-stage columns). Everything about state, progress,
   * promise and evidence is a brief field and carries
   * `data-source="computed:<field>"` (I1). The only authored text is the
   * quoted note, with its author chip, rendered as the markdown it was
   * written in.
   *
   * The card is the old board card's shape: the title owns its own line (two
   * at most), the state and the badges share the line under it, then the
   * progress bar, the computed line and the note.
   */
  import { page } from "$app/stores";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { initiativeLine, WATCH_STATE_LABELS } from "$lib/brief.js";
  import { formatAgo } from "$lib/time/format.js";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import InitiativeProgress from "./InitiativeProgress.svelte";

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
  let href = $derived(
    workspaceHref(`/initiatives/${encodeURIComponent(initiative.id)}`),
  );
  const STATE_TONE = {
    past_promise: "warn",
    moving: "moving",
    no_promise: "neutral",
    done: "ok",
  };
  let card = $derived(variant === "card");
</script>

{#snippet badges()}
  {#if initiative.possiblyMoot}
    <span
      class="ui-badge ui-badge--neutral"
      title={initiative.possiblyMoot.reason}
      data-source="computed:initiatives.state">possibly moot</span
    >
  {/if}
  {#if initiative.blockedCount > 0}
    <a
      class="ui-badge ui-badge--danger hover:underline"
      href={decideHref}
      draggable="false"
      title="{initiative.blockedCount} blocked — answer in Decide"
      data-source="computed:initiatives.blocked_count"
      >⚑ {initiative.blockedCount}<span class="sr-only"> blocked</span></a
    >
  {/if}
  {#if initiative.waitingOnYouCount > 0}
    <a
      class="ui-badge ui-badge--warn hover:underline"
      href={decideHref}
      draggable="false"
      data-source="computed:initiatives.waiting_on_you_count"
      >{initiative.waitingOnYouCount} waiting on you</a
    >
  {/if}
{/snippet}

{#snippet computedLine()}
  {#if line.length}
    <p
      class="flex min-w-0 flex-wrap gap-x-1.5 text-micro text-fg-muted [overflow-wrap:anywhere]"
    >
      {#each line as part, i (part.field)}
        {#if i > 0}<span aria-hidden="true" class="text-fg-subtle">·</span
          >{/if}<span
          class={part.field === "initiatives.past_promise_for"
            ? "text-warn-text"
            : ""}
          data-source="computed:{part.field}">{part.text}</span
        >
      {/each}
    </p>
  {/if}
{/snippet}

{#snippet note()}
  {#if initiative.note}
    <blockquote
      class="note min-w-0 border-l-2 border-line-strong pl-2 text-micro text-fg [overflow-wrap:anywhere]"
      data-watch-note
    >
      <MarkdownRenderer
        inline
        source={initiative.note.text}
        class="note-text"
        organizationSlug={$page.params.organization}
        workspaceSlug={$page.params.workspace}
      />
      <span class="text-fg-muted" data-author-chip
        >— {initiative.note.author?.display || "Unknown"}{noteAge
          ? ` · ${noteAge}`
          : ""}</span
      >
    </blockquote>
  {/if}
{/snippet}

{#if card}
  <div
    class="watch-card rounded-md border border-line bg-panel transition-colors hover:border-line-strong hover:bg-panel-hover"
    data-watch-row={initiative.ref}
    data-watch-variant="card"
    data-watch-state={initiative.state}
  >
    <a
      {href}
      draggable="false"
      class="watch-card-link block px-3 pb-1.5 pt-2.5"
      ondragstart={(event) => event.preventDefault()}
      data-watch-title
      ><h3
        class="line-clamp-2 break-words text-meta font-medium leading-snug text-fg"
      >
        {initiative.title}
      </h3></a
    >
    <div class="flex min-w-0 flex-col gap-1.5 px-3 pb-2.5">
      <div class="flex min-w-0 flex-wrap items-center gap-1.5">
        <span
          class="ui-badge state state--{STATE_TONE[initiative.state]}"
          data-source="computed:initiatives.state"
          >{WATCH_STATE_LABELS[initiative.state]}</span
        >
        {@render badges()}
      </div>
      {#if initiative.progress}
        <InitiativeProgress
          progress={initiative.progress}
          state={initiative.state}
          title={initiative.title}
          variant="card"
        />
      {/if}
      {@render computedLine()}
      {@render note()}
      {@render footer?.()}
    </div>
  </div>
{:else}
  <div
    class="watch-row flex min-w-0 flex-wrap items-start gap-x-4 gap-y-1.5 px-3 py-2.5 transition-colors hover:bg-panel-hover"
    data-watch-row={initiative.ref}
    data-watch-variant="row"
    data-watch-state={initiative.state}
  >
    <div class="flex min-w-0 flex-[1_1_18rem] flex-col gap-0.5">
      <a
        class="min-w-0 max-w-full text-meta font-medium text-fg [overflow-wrap:anywhere] hover:text-accent-text"
        {href}
        data-watch-title>{initiative.title}</a
      >
      {@render computedLine()}
      {#if initiative.note}<div class="mt-1">{@render note()}</div>{/if}
    </div>
    <div class="flex shrink-0 flex-wrap items-center gap-2 pt-0.5 sm:ml-auto">
      {@render badges()}
      {#if initiative.progress}
        <InitiativeProgress
          progress={initiative.progress}
          state={initiative.state}
          title={initiative.title}
        />
      {/if}
    </div>
  </div>
{/if}

<style>
  /* Native link dragging cancels the board's pointer session. */
  .watch-card-link {
    -webkit-user-drag: none;
  }
  .watch-card-link:hover h3 {
    color: var(--accent-text);
  }
  .state {
    border-color: var(--line);
    color: var(--fg-muted);
  }
  .state--warn {
    background: rgba(245, 158, 11, 0.08);
    border-color: rgba(245, 158, 11, 0.28);
    color: var(--warn-text);
  }
  .state--ok {
    background: rgba(16, 185, 129, 0.08);
    border-color: rgba(16, 185, 129, 0.28);
    color: var(--ok-text);
  }
  .state--moving {
    background: var(--accent-soft);
    border-color: transparent;
    color: var(--accent-text);
  }
  /* A glance, not the thread: three lines of the note at most. */
  .note {
    display: -webkit-box;
    -webkit-line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .note :global(.note-text) {
    margin-right: 4px;
  }
  .note :global(.note-text p) {
    display: inline;
    margin: 0;
  }
  .note :global(.note-text code) {
    font-size: 11px;
  }
</style>
