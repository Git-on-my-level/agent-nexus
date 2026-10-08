<script>
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import PlanView from "$lib/components/PlanView.svelte";
  import WorkSummary from "$lib/components/WorkSummary.svelte";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { workSummaryModel } from "$lib/workSummary.js";

  let {
    items = [],
    resolved = new Map(),
    organizationSlug = "",
    workspaceSlug = "",
    onpreview = null,
    onpreviewclose = null,
    now = Date.now(),
  } = $props();
  let canNavigate = $derived(Boolean(organizationSlug && workspaceSlug));
  let workspaceHref = $derived(
    canNavigate ? bindWorkspaceHref(organizationSlug, workspaceSlug) : null,
  );

  const itemRefs = (item) => {
    const refs = new Map(resolved ?? []);
    const stepStates = new Map(
      (item.plan_state?.steps ?? []).map((step) => [step.id, step]),
    );
    for (const step of item.plan?.steps ?? []) {
      if (!step.ref || refs.has(step.ref)) continue;
      const state = stepStates.get(step.id);
      refs.set(step.ref, {
        kind: "card",
        title: step.title,
        status: state?.status,
        resolvable: state?.resolvable === true,
      });
    }
    return refs;
  };
</script>

<ul class="initiatives" aria-label="Initiative details">
  {#each items as item (item.ref)}
    {@const resolvedItemRefs = itemRefs(item)}
    {@const summary = workSummaryModel(item, { now })}
    {@const assignees = item.assignee_refs ?? []}
    <li data-report-initiative={item.ref}>
      <header>
        <div class="heading">
          {#if canNavigate}<a
              href={workspaceHref(`/tasks/${encodeURIComponent(item.ref)}`)}
              >{item.title}</a
            >{:else}<strong>{item.title}</strong>{/if}
          <span class="meta">
            {#if item.priority}<span>{item.priority}</span>{/if}
            <!-- The shared summary, so a report panel and the Overview card
                 for the same initiative cannot disagree. Priority stays
                 beside it: it is not a state. -->
            <WorkSummary {summary} density="row" title={item.title} {now} />
          </span>
        </div>
        {#if item.summary}
          <!-- Authored markdown, like every other body in the product. A
               report panel printing `**Goal:**` was the one surface still
               showing its source. Block rendering, not inline: a summary with
               a list in it should render the list rather than run its dashes
               together. Clamped to two lines — a dashboard panel is a glance,
               and the initiative page has the whole body. -->
          <MarkdownRenderer
            source={item.summary}
            class="summary"
            {resolved}
            {organizationSlug}
            {workspaceSlug}
            {onpreview}
            {onpreviewclose}
          />
        {/if}
      </header>

      <section data-initiative-plan>
        <h4>Plan and linked work</h4>
        {#if item.plan || item.plan_state}
          <PlanView
            plan={item.plan}
            planState={item.plan_state}
            resolved={resolvedItemRefs}
            {organizationSlug}
            {workspaceSlug}
            stepsExpanded
            {onpreview}
            {onpreviewclose}
          />
        {:else}
          <p class="muted">No plan yet.</p>
        {/if}
      </section>

      <section data-initiative-assignees>
        <h4>Assignees</h4>
        {#if assignees.length}
          <p class="assignees">On it: {assignees.join(", ")}</p>
        {:else}
          <p class="muted">Nobody assigned.</p>
        {/if}
      </section>

      {#if item.needs?.length}
        <section>
          <h4>Needs attention</h4>
          <ul class="needs">
            {#each item.needs as need}<li>{need}</li>{/each}
          </ul>
        </section>
      {/if}
    </li>
  {/each}
</ul>

<style>
  .initiatives {
    display: grid;
    /* An implicit column sizes to its widest child's max-content. */
    grid-template-columns: minmax(0, 1fr);
    gap: 20px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .initiatives > li {
    display: grid;
    /* An implicit column sizes to its widest child's max-content. */
    grid-template-columns: minmax(0, 1fr);
    gap: 14px;
    min-width: 0;
    padding-bottom: 16px;
    border-bottom: 1px solid var(--line-subtle);
  }
  .initiatives > li:last-child {
    padding-bottom: 0;
    border-bottom: 0;
  }
  .heading {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    justify-content: space-between;
    gap: 6px 12px;
  }
  .heading a,
  .heading strong {
    color: var(--fg);
    font-size: 13px;
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .heading a:hover {
    color: var(--accent-text);
    text-decoration: underline;
  }
  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    color: var(--fg-muted);
    font-size: 10px;
    text-transform: capitalize;
  }
  .assignees,
  .muted {
    color: var(--fg-muted);
    font-size: 11px;
    line-height: 1.6;
  }
  /*
   * `:global` because the summary is rendered by `MarkdownRenderer`, whose
   * markup this component's scoping does not reach. Kept under `.initiatives`
   * so it stays this component's rule rather than a global one.
   */
  .initiatives :global(.summary) {
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    margin-top: 5px;
    color: var(--fg-muted);
    font-size: 11px;
    line-height: 1.6;
  }
  h4 {
    margin: 0 0 8px;
    color: var(--fg-muted);
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
  }
  .needs {
    display: grid;
    /* An implicit column sizes to its widest child's max-content. */
    grid-template-columns: minmax(0, 1fr);
    gap: 7px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .needs li {
    color: var(--warn-text);
    font-size: 11px;
  }
</style>
