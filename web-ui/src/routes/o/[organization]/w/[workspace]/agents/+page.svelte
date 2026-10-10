<script>
  import { onMount, tick } from "svelte";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import {
    agentPath,
    agentRowModel,
    groupAgentsByState,
    rosterSummary,
    taskPath,
  } from "$lib/agentPresence.js";
  import {
    agentRoster,
    refreshAgentRoster,
    startAgentRoster,
  } from "$lib/agentRoster.js";
  import {
    agentShortcutAction,
    agentShortcutList,
  } from "$lib/agentShortcuts.js";
  import { otherDialogOpen } from "$lib/inboxShortcuts.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import AgentRosterRow from "$lib/components/agents/AgentRosterRow.svelte";
  import AgentStateDot from "$lib/components/agents/AgentStateDot.svelte";
  import KeyboardShortcutsDialog from "$lib/components/KeyboardShortcutsDialog.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import SetupPrompt from "$lib/components/setup/SetupPrompt.svelte";
  import SkeletonInboxRow from "$lib/components/state/SkeletonInboxRow.svelte";
  import StateError from "$lib/components/state/StateError.svelte";

  let workspaceSlug = $derived($page.params.workspace);
  let cliBaseUrl = $derived($page.data?.workspace?.cliBaseUrl ?? "");
  let cliInstallCommand = $derived(
    $page.data?.workspace?.cliInstallCommand ?? "",
  );
  let workspaceLabel = $derived(
    $page.data?.workspace?.label || $page.params.workspace || "",
  );
  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );

  let now = $state(Date.now());
  let helpOpen = $state(false);
  let selectedHandle = $state("");
  let retrying = $state(false);

  let roster = $derived(
    $agentRoster.workspace === workspaceSlug ? $agentRoster : null,
  );
  let agents = $derived(roster?.agents ?? []);
  let summary = $derived(rosterSummary(agents));
  let allGroups = $derived(groupAgentsByState(agents));
  /*
   * Identities that have never checked in collapse into one counted group.
   * They are real rows — an enrolled host agent nobody has run yet — but a
   * roster that lists them beside working agents spends its first screen on
   * bookkeeping.
   */
  let groups = $derived(allGroups.filter((group) => !group.collapsed));
  let foldedGroups = $derived(allGroups.filter((group) => group.collapsed));
  let rows = $derived(
    groups.flatMap((group) =>
      group.agents.map((agent) => ({
        agent,
        group: group.key,
        model: agentRowModel(agent, {
          now,
          loadedAt: roster?.loadedAt || now,
        }),
      })),
    ),
  );
  let selectedIndex = $derived(
    rows.findIndex((row) => row.agent.handle === selectedHandle),
  );
  let loading = $derived(!roster || roster.status === "idle");
  let failed = $derived(roster?.status === "error" && agents.length === 0);

  async function retry() {
    retrying = true;
    try {
      await refreshAgentRoster();
    } finally {
      retrying = false;
    }
  }

  function rowHref(row) {
    return workspaceHref(agentPath(row.agent));
  }

  async function move(step) {
    if (!rows.length) return;
    const index =
      selectedIndex < 0
        ? step > 0
          ? 0
          : rows.length - 1
        : Math.min(rows.length - 1, Math.max(0, selectedIndex + step));
    selectedHandle = rows[index].agent.handle;
    await tick();
    document
      .querySelector(`[data-agent-row="${CSS.escape(selectedHandle)}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }

  function handleKeydown(event) {
    const action = agentShortcutAction(event, {
      helpOpen,
      modalOpen: !helpOpen && otherDialogOpen(),
    });
    if (!action) return;
    const row = rows[selectedIndex] ?? null;
    switch (action.type) {
      case "help":
        helpOpen = true;
        break;
      case "close-help":
        helpOpen = false;
        break;
      case "next":
        void move(1);
        break;
      case "previous":
        void move(-1);
        break;
      case "open":
        if (!row) return;
        void goto(rowHref(row));
        break;
      case "inbox":
        if (!row?.model.ask) return;
        void goto(workspaceHref(row.model.ask.href));
        break;
      case "task":
        if (!row?.model.task?.ref) return;
        void goto(workspaceHref(taskPath(row.model.task.ref)));
        break;
      default:
        return;
    }
    event.preventDefault();
  }

  onMount(() => {
    const release = startAgentRoster(workspaceSlug);
    void refreshAgentRoster();
    const clock = setInterval(() => {
      now = Date.now();
    }, 15_000);
    return () => {
      clearInterval(clock);
      release();
    };
  });
</script>

<svelte:window onkeydown={handleKeydown} />
<svelte:head><title>Agents · Agent Nexus</title></svelte:head>

<WorkspacePageShell data-tour="agents-page">
  <WorkspacePageHeader title="Agents">
    {#snippet subtitle()}
      {#if !loading && (summary.total > 0 || summary.inactiveTotal > 0)}
        <span data-agent-summary>
          {#if summary.total === 0}
            <!-- Enrolled, never used. "0 agents" would read as a failed read. -->
            No agents have checked in yet
          {:else}{summary.total}
            {summary.total === 1 ? "agent" : "agents"}{/if}
          {#if summary.working}
            <span class="text-fg-subtle">·</span> {summary.working} working
          {/if}
          {#if summary.waiting_on_human}
            <span class="text-fg-subtle">·</span>
            <span class="font-medium text-warn-text"
              >{summary.waiting_on_human} waiting on you</span
            >
          {/if}
          {#if summary.idle}
            <span class="text-fg-subtle">·</span> {summary.idle} idle
          {/if}
          {#if summary.offline}
            <span class="text-fg-subtle">·</span> {summary.offline} offline
          {/if}
          {#if summary.stale}
            <!-- Only a silence with work riding on it: worth the colour. -->
            <span class="text-fg-subtle">·</span>
            <span class="font-medium text-warn-text">{summary.stale} stale</span
            >
          {/if}
        </span>
      {/if}
    {/snippet}
  </WorkspacePageHeader>

  {#if roster?.status === "error" && agents.length > 0}
    <p class="text-micro text-warn-text" role="status">
      Showing the last roster; the latest read failed. {roster.error}
    </p>
  {/if}

  {#if loading}
    <div class="overflow-hidden rounded-md border border-line bg-bg-soft">
      {#each [0, 1, 2] as index (index)}
        <SkeletonInboxRow />
      {/each}
    </div>
  {:else if failed}
    <StateError
      title="Agents did not load"
      message={roster?.error || "The roster is unavailable right now."}
      onretry={retry}
      {retrying}
    />
  {:else if !agents.length}
    <!--
      A dead end that links away is a dead end. The work this page is waiting
      for happens here, with the same panel Access → Hosts offers.
    -->
    <div
      class="rounded-md border border-line bg-bg-soft px-4 py-4"
      data-agents-empty
    >
      <SetupPrompt
        kind="machine"
        {cliBaseUrl}
        {cliInstallCommand}
        {workspaceLabel}
        heading="No agents yet — connect a machine"
        lede="Agents reach this workspace through the computer they run on. Set one up once; every agent on it appears here as soon as it uses anx."
      >
        {#snippet status()}
          <p
            class="flex items-center gap-2 border-t border-line-subtle pt-3 text-micro text-fg-muted"
          >
            <span
              class="h-1.5 w-1.5 shrink-0 rounded-full bg-fg-subtle"
              aria-hidden="true"
            ></span>
            Nothing has checked in yet. This page notices the first agent on its own.
          </p>
          <p class="mt-2 text-micro">
            <a
              class="text-accent-text hover:underline"
              href={workspaceHref("/access#hosts")}>Manage machines in Access</a
            >
          </p>
        {/snippet}
      </SetupPrompt>
    </div>
  {:else}
    <div class="space-y-5" data-agents-roster>
      {#each groups as group (group.key)}
        <section aria-labelledby={`agents-group-${group.key}`}>
          <h2
            id={`agents-group-${group.key}`}
            class="mb-1.5 flex items-center gap-2 px-1 text-micro font-semibold uppercase tracking-wide text-fg-muted"
          >
            <AgentStateDot state={group.key} />
            {group.label}
            <span class="font-normal tabular-nums text-fg-subtle"
              >{group.agents.length}</span
            >
          </h2>
          <div class="overflow-hidden rounded-md border border-line bg-bg-soft">
            {#each rows.filter((row) => row.group === group.key) as row (row.agent.id)}
              <AgentRosterRow
                agent={row.agent}
                model={row.model}
                href={rowHref(row)}
                {workspaceHref}
                selected={row.agent.handle === selectedHandle}
                onselect={() => (selectedHandle = row.agent.handle)}
              />
            {/each}
          </div>
        </section>
      {/each}
      {#each foldedGroups as group (group.key)}
        <details class="agents-fold" data-agents-fold={group.key}>
          <summary>
            {group.label}
            <span class="tabular-nums">({group.agents.length})</span>
          </summary>
          <div
            class="mt-1.5 overflow-hidden rounded-md border border-line bg-bg-soft"
          >
            {#each group.agents as agent (agent.id)}
              <AgentRosterRow
                {agent}
                model={agentRowModel(agent, {
                  now,
                  loadedAt: roster?.loadedAt || now,
                })}
                href={workspaceHref(agentPath(agent))}
                {workspaceHref}
              />
            {/each}
          </div>
        </details>
      {/each}
      <p
        class="hidden flex-wrap items-center gap-x-3 gap-y-1 px-1 text-micro text-fg-subtle lg:flex"
        data-agents-key-hints
      >
        <span
          ><kbd class="agents-kbd">J</kbd>/<kbd class="agents-kbd">K</kbd> move</span
        >
        <span><kbd class="agents-kbd">Enter</kbd> open</span>
        <span><kbd class="agents-kbd">I</kbd> answer in Inbox</span>
        <span><kbd class="agents-kbd">T</kbd> task</span>
        <button
          class="ml-auto hover:text-fg"
          type="button"
          onclick={() => (helpOpen = true)}
          ><kbd class="agents-kbd">?</kbd> all shortcuts</button
        >
      </p>
    </div>
  {/if}

  <KeyboardShortcutsDialog
    bind:open={helpOpen}
    shortcuts={agentShortcutList()}
    title="Agents shortcuts"
  />
</WorkspacePageShell>

<style>
  /* Never-used identities: present, counted, out of the way. */
  .agents-fold {
    border-top: 1px solid var(--line-subtle);
    padding-top: 0.5rem;
  }
  .agents-fold summary {
    cursor: pointer;
    padding: 0.25rem 0.25rem;
    color: var(--fg-muted);
    font-size: 11px;
  }
  .agents-fold summary span {
    color: var(--fg-subtle);
  }
  .agents-kbd {
    display: inline-block;
    min-width: 1rem;
    padding: 0 0.25rem;
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    font-family: var(--font-sans);
    font-weight: 500;
    font-size: 11px;
    line-height: 16px;
    text-align: center;
    color: var(--fg-muted);
  }
</style>
