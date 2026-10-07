<script>
  import { goto } from "$app/navigation";

  import { tooltip } from "$lib/actions/tooltip.js";

  import AgentBridgeIndicator from "$lib/components/agents/AgentBridgeIndicator.svelte";
  import AgentStateDot from "$lib/components/agents/AgentStateDot.svelte";
  import {
    agentRuntimeLabel,
    isQuietAgentState,
    taskPath,
  } from "$lib/agentPresence.js";
  import { formatAbsoluteDateTime } from "$lib/formatDate";

  /**
   * One agent in the roster: who and where, doing what, for how long. The
   * row opens the agent page; a waiting row's ask opens the Inbox item (the
   * Inbox is where asks are answered).
   */
  let {
    agent,
    model,
    href = "",
    workspaceHref = (path) => path,
    selected = false,
    onselect = () => {},
  } = $props();

  let runtime = $derived(agentRuntimeLabel(agent));
  let taskHref = $derived(
    model.task?.ref ? workspaceHref(taskPath(model.task.ref)) : "",
  );
  let askHref = $derived(model.ask ? workspaceHref(model.ask.href) : "");
  let severityTone = $derived(
    model.ask?.severity === "critical"
      ? "text-danger-text"
      : model.ask?.severity === "high"
        ? "text-warn-text"
        : "text-fg-muted",
  );

  function openRow(event) {
    // Links inside the row keep their own destination.
    if (event.target instanceof Element && event.target.closest("a, button")) {
      return;
    }
    if (event.metaKey || event.ctrlKey) {
      window.open(href, "_blank", "noopener");
      return;
    }
    onselect();
    void goto(href);
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="agent-row {selected ? 'agent-row--selected' : ''} {isQuietAgentState(
    model.state,
  )
    ? 'agent-row--quiet'
    : ''}"
  data-agent-row={agent.handle}
  data-agent-state={model.state}
  aria-current={selected ? "true" : undefined}
  onclick={openRow}
>
  <span class="agent-row__dot"><AgentStateDot state={model.state} /></span>

  <div class="agent-row__who min-w-0">
    <a
      class="block truncate text-meta font-medium text-fg hover:underline"
      {href}
      data-agent-link
      onfocus={onselect}>{agent.display_name || agent.handle}</a
    >
    <p class="flex min-w-0 items-center gap-1.5 text-micro text-fg-muted">
      <span class="truncate">@{agent.handle}</span>
      {#if runtime}
        <span class="text-fg-subtle" aria-hidden="true">·</span>
        <span class="shrink-0">{runtime}</span>
      {/if}
      <AgentBridgeIndicator online={agent.bridge_online} />
    </p>
  </div>

  <div class="agent-row__now min-w-0">
    {#if model.ask}
      <a
        class="block truncate text-meta text-fg hover:underline"
        href={askHref}
        data-agent-ask-link>{model.ask.title}</a
      >
      <p class="truncate text-micro text-fg-muted">
        {model.ask.kind}{#if model.ask.severity}<span class="text-fg-subtle"
            >{" · "}</span
          ><span class={severityTone}>{model.ask.severity}</span
          >{/if}{#if model.ask.subjectTitle}<span class="text-fg-subtle"
            >{" · "}</span
          >on {model.ask.subjectTitle}{/if}{#if model.moreAsks}<span
            class="text-fg-subtle">{" · "}</span
          >{model.moreAsks} more open{/if}
      </p>
    {:else}
      {#if model.task && taskHref}
        <a
          class="block truncate text-meta text-fg hover:underline"
          href={taskHref}>{model.task.title}</a
        >
      {:else}
        <p
          class="truncate text-meta {model.state === 'working'
            ? 'text-fg'
            : 'text-fg-muted'}"
        >
          {model.headline}
        </p>
      {/if}
      {#if model.note}
        <p class="truncate text-micro text-fg-muted" data-agent-note>
          “{model.note.text}”{#if model.note.age}<span class="text-fg-subtle"
              >{" · "}<time
                datetime={model.note.at}
                use:tooltip={formatAbsoluteDateTime(model.note.at)}
                >{model.note.age === "<1m"
                  ? "just now"
                  : `${model.note.age} ago`}</time
              ></span
            >{/if}
        </p>
      {:else if model.task && model.state === "stale"}
        <p class="truncate text-micro text-fg-subtle">{model.headline}</p>
      {/if}
    {/if}
  </div>

  <div class="agent-row__side">
    {#if model.duration}
      <p
        class="text-meta tabular-nums text-fg"
        use:tooltip={model.durationTitle}
      >
        {#if model.run}<span class="text-fg-subtle">run{" "}</span
          >{/if}{model.duration}
      </p>
    {/if}
    {#if model.ask}
      <a
        class="agent-row__inbox"
        href={askHref}
        data-agent-inbox-link
        use:tooltip={"Answer in Inbox (I)"}>Answer in Inbox</a
      >
    {:else if model.durationTitle && model.duration}
      <p class="text-micro text-fg-subtle">
        {model.state === "working"
          ? model.run
            ? "active run"
            : "last update"
          : "last signal"}
      </p>
    {/if}
  </div>
</div>

<style>
  .agent-row {
    display: grid;
    grid-template-columns: 12px minmax(0, 15rem) minmax(0, 1fr) auto;
    gap: 0.75rem;
    align-items: center;
    padding: 0.625rem 0.875rem;
    border-top: 1px solid var(--line-subtle);
    cursor: pointer;
    transition: background var(--motion-fast);
  }
  .agent-row:first-child {
    border-top: 0;
  }
  .agent-row:hover {
    background: var(--panel-hover);
  }
  .agent-row--selected {
    background: var(--panel-hover);
    box-shadow: inset 2px 0 0 var(--accent);
  }
  /* Not running and nothing waiting: quieter than the rows that matter. */
  .agent-row--quiet {
    opacity: 0.72;
  }
  .agent-row__dot {
    display: flex;
    justify-content: center;
  }
  .agent-row__side {
    min-width: 5.5rem;
    text-align: right;
  }
  .agent-row__inbox {
    display: inline-block;
    margin-top: 0.125rem;
    border-radius: var(--radius-sm);
    font-size: 11px;
    line-height: 16px;
    font-weight: 500;
    color: var(--accent-text);
  }
  .agent-row__inbox:hover {
    text-decoration: underline;
  }
  @media (max-width: 767px) {
    .agent-row {
      grid-template-columns: 12px minmax(0, 1fr) auto;
      row-gap: 0.25rem;
      align-items: start;
    }
    .agent-row__dot {
      padding-top: 5px;
    }
    .agent-row__now {
      grid-column: 2 / 4;
    }
    .agent-row__side {
      grid-row: 1;
      grid-column: 3;
      min-width: 0;
    }
  }
</style>
