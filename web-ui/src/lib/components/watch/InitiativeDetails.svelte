<script>
  /**
   * An initiative's expanded Watch row: promise, latest evidence and note,
   * the rollup with its first children, the plan's next steps and the owner,
   * and a link to the full initiative.
   *
   * Everything but the next steps comes from the brief row (the owner too,
   * when core sends it there). The steps need the card, read once when the
   * row opens and only then,
   * through `workCache` (30 seconds, one read in flight per card). The
   * initiative page paints from the same entry, so opening it next is free.
   */
  import { onMount } from "svelte";
  import { page } from "$app/stores";
  import { coreClient } from "$lib/coreClient";
  import { prefetchWork, readWorkSnapshot } from "$lib/workCache.js";
  import {
    actorDisplayLabel,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import {
    evidenceLine,
    planModel,
    promiseLine,
    rollupModel,
  } from "$lib/initiativeDetails.js";
  import ActorLabel from "$lib/components/ActorLabel.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";

  let {
    initiative,
    /** The initiative page. */
    href = "",
    /** `(ref) => href` for a child card. */
    childHref = null,
    /** Off where the caller lists the children itself (kid rows). */
    rollup: showRollup = true,
    /**
     * Read the plan on mount. Off for a row that opened by default rather
     * than by the reader: it waits for a tap, so a list read stays one read.
     */
    autoload = true,
    /**
     * One strip under a list row's kid rows: evidence, owner, next steps and
     * the link. The row itself already says the promise.
     */
    compact = false,
    now = Date.now(),
  } = $props();

  let promise = $derived(promiseLine(initiative));
  let evidence = $derived(evidenceLine(initiative, now));
  let rollup = $derived(rollupModel(initiative.rollup));

  /** "idle" | "loading" | "ready" | "failed" */
  let status = $state("idle");
  let work = $state(null);
  let plan = $derived(planModel(work));
  // The brief names the owner; the card read answers for a core that sends
  // none there.
  let ownerRef = $derived(initiative.owner || plan?.owner || "");
  let owner = $derived(
    ownerRef
      ? {
          id: ownerRef,
          label: actorDisplayLabel(
            ownerRef,
            $actorRegistry,
            $principalRegistry,
          ),
        }
      : null,
  );

  let ticket = 0;
  async function load() {
    const mine = ++ticket;
    const cached = readWorkSnapshot(initiative.ref);
    if (cached?.full) {
      work = cached.work;
      status = "ready";
      return;
    }
    status = "loading";
    const read = await prefetchWork(initiative.ref, coreClient);
    if (mine !== ticket) return;
    // `prefetchWork` answers null when another read just filled the cache.
    const found = read ?? readWorkSnapshot(initiative.ref)?.work ?? null;
    work = found;
    status = found ? "ready" : "failed";
  }

  onMount(() => {
    // A cached read costs nothing, so a default-open row still shows it.
    if (autoload || readWorkSnapshot(initiative.ref)?.full) void load();
    return () => (ticket += 1);
  });

  const DOT = {
    not_started: "bg-fg-subtle",
    moving: "bg-accent",
    stuck: "bg-warn",
    done: "bg-ok",
  };
</script>

{#snippet stepsInline()}
  {#if status === "idle"}
    <button
      type="button"
      class="ui-prose-link"
      onclick={() => void load()}
      data-details-load>Show next steps</button
    >
  {:else if status === "loading"}
    <span role="status">Loading the plan…</span>
  {:else if status === "failed"}
    <span
      >Couldn't load the plan. <button
        type="button"
        class="ui-prose-link"
        onclick={() => void load()}
        data-details-retry>Try again</button
      ></span
    >
  {:else if plan?.steps.length}
    <ol class="inline">
      {#each plan.steps as step, i (step.id)}
        <li class="inline text-fg [overflow-wrap:anywhere]">
          {#if i > 0}<span aria-hidden="true" class="mx-1 text-fg-subtle"
              >·</span
            >{/if}{i + 1}. {step.title}
        </li>
      {/each}
    </ol>
    {#if plan.more}<span class="ml-1">+{plan.more} more ready</span>{/if}
  {:else if plan?.hasPlan}
    <span>No step is ready to start.</span>
  {:else}
    <span>No plan yet.</span>
  {/if}
{/snippet}

{#if compact}
  <div
    class="flex flex-wrap items-baseline gap-x-5 gap-y-1.5 text-micro text-fg-muted"
    data-initiative-details
  >
    <span class="min-w-0 [overflow-wrap:anywhere]" data-details-evidence>
      <span data-source="computed:{evidence.field}">{evidence.text}</span
      >{#if initiative.note}<span aria-hidden="true" class="mx-1">·</span
        >“<MarkdownRenderer
          inline
          plain
          source={initiative.note.text}
          organizationSlug={$page.params.organization}
          workspaceSlug={$page.params.workspace}
        />”
        <span data-author-chip
          >— {initiative.note.author?.display || "Unknown"}</span
        >{/if}
    </span>
    <span class="inline-flex items-center gap-1.5" data-details-owner>
      Owner
      {#if owner}
        <ActorLabel label={owner.label} seed={owner.id} size="xs" />
      {:else if status === "ready"}
        <span class="text-fg">not assigned</span>
      {:else}
        <span>—</span>
      {/if}
    </span>
    <span class="min-w-0" data-details-steps>
      <span data-ui-label>Next steps:</span>
      {@render stepsInline()}
    </span>
    {#if href}
      <a
        class="font-medium text-accent-text hover:underline"
        {href}
        data-details-open>Open initiative →</a
      >
    {/if}
  </div>
{:else}
  <div class="details-grid pt-1 text-meta" data-initiative-details>
    <div class="min-w-0 space-y-3">
      <div data-details-promise>
        <p class="ui-label mb-0.5">Promise</p>
        <p
          class="[overflow-wrap:anywhere] {promise.tone === 'warning'
            ? 'text-warn-text'
            : promise.tone === 'muted'
              ? 'text-fg-muted'
              : 'text-fg'}"
          data-source="computed:{promise.field}"
        >
          {promise.text}
        </p>
      </div>

      <div data-details-evidence>
        <p class="ui-label mb-0.5">Latest evidence</p>
        <p class="text-fg-muted" data-source="computed:{evidence.field}">
          {evidence.text}
        </p>
        {#if initiative.note}
          <blockquote
            class="mt-1 border-l-2 border-line-strong pl-2 text-micro text-fg [overflow-wrap:anywhere]"
          >
            <MarkdownRenderer
              inline
              plain
              source={initiative.note.text}
              organizationSlug={$page.params.organization}
              workspaceSlug={$page.params.workspace}
            />
            <span class="text-fg-muted" data-author-chip
              >— {initiative.note.author?.display || "Unknown"}</span
            >
          </blockquote>
        {/if}
      </div>

      <div data-details-owner>
        <p class="ui-label mb-0.5">Owner</p>
        {#if owner}
          <ActorLabel label={owner.label} seed={owner.id} size="xs" />
        {:else if status === "loading"}
          <span
            class="ui-skeleton-reveal inline-block h-3 w-24 rounded bg-panel-hover align-middle"
            aria-hidden="true"
          ></span>
        {:else if status === "ready"}
          <p class="text-fg-muted">Not assigned</p>
        {:else}
          <p class="text-fg-muted">—</p>
        {/if}
      </div>
    </div>

    <div class="min-w-0 space-y-3">
      {#if showRollup && rollup.total}
        <div data-details-rollup>
          <p class="ui-label mb-1">
            <span data-source="computed:initiatives.rollup.total"
              >{rollup.total}
              {rollup.total === 1 ? "card" : "cards"} inside</span
            >
            {#if rollup.needingViewer}<span
                class="ml-1 font-semibold text-danger-text"
                data-source="computed:initiatives.rollup.needing_viewer"
                >· {rollup.needingViewer} need you</span
              >{/if}
            {#if rollup.pastPromise}<span
                class="ml-1 font-semibold text-warn-text"
                data-source="computed:initiatives.rollup.past_promise"
                >· {rollup.pastPromise} past promise</span
              >{/if}
          </p>
          <div
            class="flex h-1.5 gap-0.5 overflow-hidden rounded-sm bg-panel-hover"
            role="img"
            aria-label={rollup.segments
              .map((s) => `${s.count} ${s.label}`)
              .join(", ")}
            data-source="computed:initiatives.rollup.by_state"
          >
            {#each rollup.segments as segment (segment.key)}
              <span class={DOT[segment.key]} style="flex: {segment.share} 1 0%"
              ></span>
            {/each}
          </div>
          <ul
            class="mt-2 space-y-1"
            data-source="computed:initiatives.rollup.children"
            data-details-children
          >
            {#each rollup.children as child (child.ref)}
              <li class="flex min-w-0 items-baseline gap-2">
                <span
                  class="h-2 w-2 shrink-0 rounded-full {DOT[child.column]}"
                  aria-hidden="true"
                ></span>
                {#if childHref}
                  <a
                    class="min-w-0 flex-1 truncate text-fg hover:text-accent-text"
                    href={childHref(child.ref)}>{child.title}</a
                  >
                {:else}
                  <span class="min-w-0 flex-1 truncate text-fg"
                    >{child.title}</span
                  >
                {/if}
                <span
                  class="shrink-0 text-micro {child.needsViewer
                    ? 'text-danger-text'
                    : child.pastPromise
                      ? 'text-warn-text'
                      : 'text-fg-muted'}"
                  >{child.pastPromise && !child.needsViewer
                    ? "Past promise"
                    : child.stateLabel}</span
                >
              </li>
            {/each}
          </ul>
          {#if rollup.more}
            <p class="mt-1 text-micro text-fg-muted">
              {rollup.more} more on the initiative
            </p>
          {/if}
        </div>
      {/if}

      <div data-details-steps>
        <p class="ui-label mb-0.5">Next steps</p>
        {#if status === "idle"}
          <button
            type="button"
            class="ui-prose-link text-meta"
            onclick={() => void load()}
            data-details-load>Show next steps</button
          >
        {:else if status === "loading"}
          <p class="sr-only" role="status">Loading the plan…</p>
          <div class="space-y-1.5" aria-hidden="true">
            <span
              class="ui-skeleton-reveal block h-3 w-3/4 rounded bg-panel-hover"
            ></span>
            <span
              class="ui-skeleton-reveal block h-3 w-1/2 rounded bg-panel-hover"
            ></span>
          </div>
        {:else if status === "failed"}
          <p class="text-fg-muted">
            Couldn't load the plan.
            <button
              type="button"
              class="ui-prose-link"
              onclick={() => void load()}
              data-details-retry>Try again</button
            >
          </p>
        {:else if plan?.steps.length}
          <ol class="list-decimal space-y-0.5 pl-5 text-fg">
            {#each plan.steps as step (step.id)}
              <li class="[overflow-wrap:anywhere]">{step.title}</li>
            {/each}
          </ol>
          {#if plan.more}
            <p class="mt-0.5 text-micro text-fg-muted">
              {plan.more} more ready
            </p>
          {/if}
        {:else if plan?.hasPlan}
          <p class="text-fg-muted">No step is ready to start.</p>
        {:else}
          <p class="text-fg-muted">No plan yet.</p>
        {/if}
      </div>
    </div>

    {#if href}
      <a
        class="details-open text-micro font-medium text-accent-text hover:underline"
        {href}
        data-details-open>Open initiative →</a
      >
    {/if}
  </div>
{/if}

<style>
  .details-grid {
    display: grid;
    gap: 0.9rem;
  }
  /* The table is the container: two columns once it is wide enough. */
  @container (min-width: 40rem) {
    .details-grid {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
      column-gap: 1.5rem;
    }
    .details-open {
      grid-column: 1 / -1;
    }
  }
</style>
