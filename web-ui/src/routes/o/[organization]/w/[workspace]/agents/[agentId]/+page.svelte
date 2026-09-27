<script>
  import { onMount, tick } from "svelte";
  import { page } from "$app/stores";

  import {
    agentActivity,
    agentKindLabel,
    agentRuntimeLabel,
    agentStateLabel,
    askKindLabel,
    formatAge,
    formatDurationSeconds,
    inboxAskPath,
    isRunActive,
    runDuration,
    runLabel,
    runStateLabel,
    taskPath,
    titleFromCardRef,
  } from "$lib/agentPresence.js";
  import { agentRoster, startAgentRoster } from "$lib/agentRoster.js";
  import { authenticatedAgent } from "$lib/authSession";
  import { coreClient } from "$lib/coreClient";
  import { formatAbsoluteDateTime } from "$lib/formatDate";
  import { label as phaseLabel } from "$lib/pm/presentation.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import AgentBridgeIndicator from "$lib/components/agents/AgentBridgeIndicator.svelte";
  import AgentStateDot from "$lib/components/agents/AgentStateDot.svelte";
  import RunAttribution from "$lib/components/agents/RunAttribution.svelte";
  import Button from "$lib/components/Button.svelte";
  import CopyableId from "$lib/components/CopyableId.svelte";
  import CopyButton from "$lib/components/CopyButton.svelte";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import StateError from "$lib/components/state/StateError.svelte";

  let agentKey = $derived(decodeURIComponent($page.params.agentId ?? ""));
  let workspaceSlug = $derived($page.params.workspace);
  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  let highlightedRun = $derived($page.url.searchParams.get("run") ?? "");

  let detail = $state(null);
  let messages = $state([]);
  let principal = $state(null);
  let host = $state(null);
  let loading = $state(true);
  let error = $state("");
  let notFound = $state(false);
  let now = $state(Date.now());

  let excludeOpen = $state(false);
  let excluding = $state(false);
  let excludeError = $state("");
  let revokeOpen = $state(false);
  let revoking = $state(false);
  let revokeError = $state("");

  // The roster is the live source of state; the detail read adds history.
  let rosterAgent = $derived(
    $agentRoster.workspace === workspaceSlug
      ? ($agentRoster.agents.find(
          (candidate) =>
            candidate.handle === agentKey ||
            candidate.id === agentKey ||
            candidate.actor_id === agentKey,
        ) ?? null)
      : null,
  );
  let agent = $derived(rosterAgent ?? detail?.agent ?? null);
  let canManage = $derived(Boolean($authenticatedAgent));
  let titles = $derived(
    new Map(
      (detail?.recent_cards ?? []).map((card) => [
        card.ref || `card:${card.handle}`,
        card.title,
      ]),
    ),
  );
  const ACTIVITY_PREVIEW = 6;
  let showAllActivity = $state(false);
  let activity = $derived(
    agentActivity({ notes: detail?.recent_notes ?? [], messages, limit: 20 }),
  );
  let visibleActivity = $derived(
    showAllActivity ? activity : activity.slice(0, ACTIVITY_PREVIEW),
  );
  let openAsks = $derived(
    (detail?.open_asks ?? []).map((ask) => ({
      ask,
      href: inboxAskPath(ask),
      since: ask.created_at,
      wait: formatAge(ask.created_at, now),
    })),
  );
  let runs = $derived(detail?.recent_runs ?? []);
  let activeRun = $derived(agent?.active_run ?? null);
  let currentTaskRef = $derived(agent?.current_card_ref ?? "");
  let currentTaskTitle = $derived(
    agent?.current_card_title || titleFromCardRef(currentTaskRef, titles),
  );
  let hostExcludes = $derived(
    Boolean(agent?.name && host?.excluded_names?.includes(agent.name)),
  );

  async function load(key = agentKey, { quiet = false } = {}) {
    if (!quiet) {
      loading = true;
      error = "";
      notFound = false;
    }
    try {
      const response = await coreClient.getAgent(key);
      if (key !== agentKey) return;
      detail = response ?? null;
      const actorId = response?.agent?.actor_id;
      const hostId = response?.agent?.host_id;
      const [events, principals, hostResponse] = await Promise.allSettled([
        actorId
          ? coreClient.listEvents({
              actor_id: actorId,
              type: "message_posted",
              limit: 20,
            })
          : Promise.resolve({ events: [] }),
        canManage && !principal
          ? coreClient.listPrincipals({ limit: 200 })
          : Promise.resolve(null),
        hostId ? coreClient.getHost(hostId) : Promise.resolve(null),
      ]);
      if (key !== agentKey) return;
      if (events.status === "fulfilled") {
        messages = Array.isArray(events.value?.events)
          ? events.value.events
          : [];
      }
      if (principals.status === "fulfilled" && principals.value) {
        principal =
          (principals.value.principals ?? []).find(
            (entry) => entry.agent_id === response?.agent?.id,
          ) ?? null;
      }
      if (hostResponse.status === "fulfilled") {
        host = hostResponse.value?.host ?? null;
      }
    } catch (loadError) {
      if (key !== agentKey) return;
      if (loadError?.status === 404) {
        notFound = true;
      } else if (!quiet) {
        error =
          loadError instanceof Error
            ? loadError.message
            : "This agent did not load.";
      }
    } finally {
      if (key === agentKey) loading = false;
    }
  }

  $effect(() => {
    const key = agentKey;
    detail = null;
    messages = [];
    principal = null;
    host = null;
    void load(key);
  });

  // Follow the roster: when it re-reads (events, timer), refresh history.
  let lastRosterLoad = 0;
  $effect(() => {
    const loadedAt = $agentRoster.loadedAt;
    if (!loadedAt || loading || !detail) return;
    // The roster stream can fire in bursts; one history re-read per 3 s.
    if (lastRosterLoad && loadedAt - lastRosterLoad < 3_000) return;
    const first = !lastRosterLoad;
    lastRosterLoad = loadedAt;
    if (!first) void load(agentKey, { quiet: true });
  });

  // A run linked from "via run …" scrolls into view once runs are loaded.
  $effect(() => {
    if (!highlightedRun || !runs.length) return;
    void tick().then(() => {
      document
        .querySelector(`[data-run-id="${CSS.escape(highlightedRun)}"]`)
        ?.scrollIntoView({ block: "center" });
    });
  });

  async function excludeOnHost() {
    if (!agent?.host_id || !agent?.name) return;
    excluding = true;
    excludeError = "";
    try {
      const current = await coreClient.getHost(agent.host_id);
      const names = new Set(current?.host?.excluded_names ?? []);
      names.add(agent.name);
      const result = await coreClient.patchHost(agent.host_id, {
        excluded_names: [...names],
      });
      host = result?.host ?? host;
      excludeOpen = false;
    } catch (patchError) {
      excludeError =
        patchError?.details ||
        (patchError instanceof Error ? patchError.message : "") ||
        "The exclusion was not saved.";
    } finally {
      excluding = false;
    }
  }

  async function revokeAgent() {
    if (!agent?.id) return;
    revoking = true;
    revokeError = "";
    try {
      await coreClient.revokePrincipal(agent.id, {});
      revokeOpen = false;
      await load(agentKey, { quiet: true });
    } catch (revokeFailure) {
      revokeError =
        revokeFailure?.details ||
        (revokeFailure instanceof Error ? revokeFailure.message : "") ||
        "The agent was not revoked.";
    } finally {
      revoking = false;
    }
  }

  function runStateTone(run) {
    if (isRunActive(run)) return "text-accent-text";
    if (run?.state === "failed") return "text-danger-text";
    if (run?.state === "completed") return "text-fg-muted";
    return "text-fg-subtle";
  }

  onMount(() => {
    const release = startAgentRoster(workspaceSlug);
    const clock = setInterval(() => {
      now = Date.now();
    }, 15_000);
    return () => {
      clearInterval(clock);
      release();
    };
  });
</script>

<svelte:head>
  <title>{agent?.display_name || agentKey} · Agents · Agent Nexus</title>
</svelte:head>

<WorkspacePageShell>
  <nav class="text-micro text-fg-muted" aria-label="Breadcrumb">
    <a class="hover:text-fg" href={workspaceHref("/agents")}>Agents</a>
    <span class="text-fg-subtle" aria-hidden="true">/</span>
    <span class="text-fg">{agent?.handle || agentKey}</span>
  </nav>

  {#if loading && !agent}
    <p class="text-meta text-fg-muted">Loading agent…</p>
  {:else if notFound}
    <div class="rounded-md border border-line bg-bg-soft px-6 py-8 text-center">
      <h1 class="text-subtitle text-fg">No agent named {agentKey}</h1>
      <p class="mt-1.5 text-meta text-fg-subtle">
        It may have been revoked with its host, or the link is out of date.
      </p>
      <a
        class="mt-4 inline-block text-meta font-medium text-accent-text hover:underline"
        href={workspaceHref("/agents")}>Back to Agents</a
      >
    </div>
  {:else if error && !agent}
    <StateError
      title="This agent did not load"
      message={error}
      onretry={() => load()}
    />
  {:else if agent}
    <header class="space-y-1" data-agent-header>
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
        <AgentStateDot state={agent.state} size="md" />
        <h1 class="text-title text-fg [overflow-wrap:anywhere]">
          {agent.display_name || agent.handle}
        </h1>
        <span
          class="rounded px-1.5 py-0.5 text-micro font-medium {agent.state ===
          'waiting_on_human'
            ? 'bg-warn-soft text-warn-text'
            : agent.state === 'working'
              ? 'bg-accent-soft text-accent-text'
              : 'bg-line-subtle text-fg-muted'}"
          data-agent-state-label
        >
          {agentStateLabel(agent.state)}
        </span>
        {#if agent.revoked_at}
          <span
            class="rounded bg-danger-soft px-1.5 py-0.5 text-micro font-medium text-danger-text"
            >Revoked</span
          >
        {/if}
      </div>
      <p
        class="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-meta text-fg-muted"
      >
        <span class="inline-flex items-center gap-0.5"
          >@{agent.handle}<CopyButton
            value={`@${agent.handle}`}
            label="Copy handle"
            iconOnly
          /></span
        >
        {#if agent.host_slug}
          <span class="text-fg-subtle" aria-hidden="true">·</span>
          <a
            class="hover:text-fg hover:underline"
            href={workspaceHref(
              `/access#host-${encodeURIComponent(agent.host_slug)}`,
            )}>{agent.host_slug}</a
          >
        {/if}
        <span class="text-fg-subtle" aria-hidden="true">·</span>
        <span
          >{agentKindLabel(agent)}{agentRuntimeLabel(agent)
            ? ` · ${agentRuntimeLabel(agent)}`
            : ""}</span
        >
        <span class="text-fg-subtle" aria-hidden="true">·</span>
        <AgentBridgeIndicator online={agent.bridge_online} showLabel />
      </p>
    </header>

    <div class="grid gap-6 lg:grid-cols-[minmax(0,1fr)_18rem]">
      <div class="min-w-0 space-y-6">
        {#if openAsks.length}
          <section aria-labelledby="agent-asks">
            <h2 id="agent-asks" class="ui-label">
              Waiting on you <span class="tabular-nums">{openAsks.length}</span>
            </h2>
            <ul
              class="overflow-hidden rounded-md border bg-bg-soft"
              style="border-color: color-mix(in srgb, var(--warn) 35%, var(--line))"
            >
              {#each openAsks as entry (entry.ask.id)}
                <li
                  class="flex items-start gap-3 border-t border-line-subtle px-4 py-2.5 first:border-t-0"
                  data-agent-open-ask
                >
                  <div class="min-w-0 flex-1">
                    <a
                      class="block text-meta text-fg hover:underline [overflow-wrap:anywhere]"
                      href={workspaceHref(entry.href)}>{entry.ask.title}</a
                    >
                    <p class="text-micro text-fg-muted">
                      {askKindLabel(
                        entry.ask.kind,
                      )}{#if entry.ask.severity}<span class="text-fg-subtle"
                          >{" · "}</span
                        ><span
                          class={entry.ask.severity === "critical"
                            ? "text-danger-text"
                            : entry.ask.severity === "high"
                              ? "text-warn-text"
                              : ""}>{entry.ask.severity}</span
                        >{/if}{#if entry.ask.subject_title}<span
                          class="text-fg-subtle">{" · "}</span
                        >on {entry.ask.subject_title}{/if}
                    </p>
                  </div>
                  <div class="shrink-0 text-right">
                    {#if entry.wait}
                      <p class="text-meta tabular-nums text-fg">
                        {entry.wait}
                      </p>
                    {/if}
                    <a
                      class="text-micro font-medium text-accent-text hover:underline"
                      href={workspaceHref(entry.href)}>Answer in Inbox</a
                    >
                  </div>
                </li>
              {/each}
            </ul>
          </section>
        {/if}

        <section aria-labelledby="agent-now">
          <h2 id="agent-now" class="ui-label">Now</h2>
          <div
            class="space-y-2 rounded-md border border-line bg-bg-soft px-4 py-3 text-meta"
          >
            {#if currentTaskRef}
              <p class="text-fg">
                <span class="text-fg-muted">On</span>
                <a
                  class="font-medium hover:underline"
                  href={workspaceHref(taskPath(currentTaskRef))}
                  >{currentTaskTitle || currentTaskRef}</a
                >
              </p>
            {:else}
              <p class="text-fg-muted">No current task.</p>
            {/if}
            {#if agent.last_progress_note}
              <p class="text-fg-muted [overflow-wrap:anywhere]" data-agent-note>
                “{agent.last_progress_note}”
                {#if agent.last_progress_at}
                  <span class="text-fg-subtle"
                    >· <time
                      datetime={agent.last_progress_at}
                      title={formatAbsoluteDateTime(agent.last_progress_at)}
                      >{formatAge(agent.last_progress_at, now) === "<1m"
                        ? "just now"
                        : `${formatAge(agent.last_progress_at, now)} ago`}</time
                    ></span
                  >
                {/if}
              </p>
            {/if}
            {#if activeRun}
              <p class="text-fg-muted">
                <span class="text-accent-text">Running</span>
                {activeRun.adapter}{activeRun.model
                  ? ` ${activeRun.model}`
                  : ""} for {formatDurationSeconds(
                  Number(activeRun.duration_seconds) +
                    Math.max(
                      0,
                      Math.floor((now - ($agentRoster.loadedAt || now)) / 1000),
                    ),
                )}
                <a
                  class="text-fg-subtle hover:text-fg hover:underline"
                  href="#runs">see run</a
                >
              </p>
            {/if}
            <p class="text-micro text-fg-subtle">
              {#if agent.last_signal_at}
                Last signal <time
                  datetime={agent.last_signal_at}
                  title={formatAbsoluteDateTime(agent.last_signal_at)}
                  >{formatAge(agent.last_signal_at, now) === "<1m"
                    ? "just now"
                    : `${formatAge(agent.last_signal_at, now)} ago`}</time
                >
              {:else}
                No signal yet: no run, note, write, or bridge check-in.
              {/if}
            </p>
          </div>
        </section>

        <section id="runs" class="scroll-mt-6" aria-labelledby="agent-runs">
          <h2 id="agent-runs" class="ui-label">Recent runs</h2>
          {#if runs.length}
            <div
              class="overflow-x-auto rounded-md border border-line bg-bg-soft"
            >
              <table class="w-full min-w-[36rem] text-left text-meta">
                <thead class="text-micro text-fg-subtle">
                  <tr class="border-b border-line-subtle">
                    <th class="px-4 py-2 font-medium">Run</th>
                    <th class="px-2 py-2 font-medium">State</th>
                    <th class="px-2 py-2 font-medium">Runtime</th>
                    <th class="px-2 py-2 text-right font-medium">Duration</th>
                    <th class="px-2 py-2 font-medium">Result</th>
                    <th class="px-4 py-2 font-medium">Task</th>
                  </tr>
                </thead>
                <tbody>
                  {#each runs as run (run.id)}
                    <tr
                      class="border-t border-line-subtle first:border-t-0 {highlightedRun ===
                      run.id
                        ? 'bg-accent-soft'
                        : ''}"
                      data-run-id={run.id}
                    >
                      <td class="px-4 py-2">
                        <span class="inline-flex items-center gap-0.5">
                          <span
                            class="font-mono text-micro text-fg"
                            title={`Started ${formatAbsoluteDateTime(run.started_at)}`}
                            >{runLabel(run)}</span
                          >
                          <CopyButton
                            value={runLabel(run)}
                            label="Copy run id"
                            iconOnly
                          />
                        </span>
                        {#if run.started_at}
                          <p class="text-micro text-fg-subtle">
                            started {formatAge(run.started_at, now)} ago
                          </p>
                        {/if}
                      </td>
                      <td class="px-2 py-2 {runStateTone(run)}">
                        {runStateLabel(
                          run.state,
                        )}{#if run.liveness === "stale" && !run.ended_at}<span
                            class="text-warn-text"
                            title="The launcher stopped reporting this run"
                          >
                            · no heartbeat</span
                          >{/if}
                      </td>
                      <td class="px-2 py-2 text-fg-muted">
                        {run.adapter}{run.model ? ` ${run.model}` : ""}
                      </td>
                      <td class="px-2 py-2 text-right tabular-nums text-fg">
                        {runDuration(run, now) || "—"}
                      </td>
                      <td class="px-2 py-2 text-fg-muted">
                        {#if run.result_collected}
                          Collected
                        {:else if isRunActive(run)}
                          <span class="text-fg-subtle">—</span>
                        {:else}
                          <span
                            class="text-warn-text"
                            title="The launcher has a result nobody read yet"
                            >Not collected</span
                          >
                        {/if}
                      </td>
                      <td class="max-w-[14rem] truncate px-4 py-2">
                        {#if run.card_ref}
                          <a
                            class="text-fg hover:underline"
                            href={workspaceHref(taskPath(run.card_ref))}
                            title={titleFromCardRef(run.card_ref, titles)}
                            >{titleFromCardRef(run.card_ref, titles) ||
                              run.card_ref}</a
                          >
                        {:else}
                          <span class="text-fg-subtle">—</span>
                        {/if}
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {:else}
            <p
              class="rounded-md border border-line bg-bg-soft px-4 py-3 text-meta text-fg-muted"
            >
              No runs reported. Runs launched with <code
                class="rounded bg-line px-1 py-px text-fg">agentctl run</code
              > appear here.
            </p>
          {/if}
        </section>

        <section aria-labelledby="agent-activity">
          <h2 id="agent-activity" class="ui-label">Notes and messages</h2>
          {#if activity.length}
            <ol
              class="overflow-hidden rounded-md border border-line bg-bg-soft"
            >
              {#each visibleActivity as entry (entry.id)}
                <li
                  class="border-t border-line-subtle px-4 py-2.5 first:border-t-0"
                  data-agent-activity={entry.kind}
                >
                  <p
                    class="flex flex-wrap items-center gap-x-1.5 text-micro text-fg-subtle"
                  >
                    <span class="font-medium text-fg-muted"
                      >{entry.kind === "note" ? "Note" : "Message"}</span
                    >
                    {#if entry.cardRef}
                      <span aria-hidden="true">·</span>
                      <a
                        class="max-w-[24rem] truncate hover:text-fg hover:underline"
                        href={workspaceHref(taskPath(entry.cardRef))}
                        >{titleFromCardRef(entry.cardRef, titles) ||
                          entry.cardRef}</a
                      >
                    {/if}
                    <span aria-hidden="true">·</span>
                    <time
                      datetime={entry.at}
                      title={formatAbsoluteDateTime(entry.at)}
                      >{formatAge(entry.at, now) === "<1m"
                        ? "just now"
                        : `${formatAge(entry.at, now)} ago`}</time
                    >
                    <RunAttribution attribution={entry.runAttribution} />
                  </p>
                  <p
                    class="mt-0.5 line-clamp-3 text-meta text-fg [overflow-wrap:anywhere]"
                  >
                    {entry.text}
                  </p>
                </li>
              {/each}
            </ol>
            {#if activity.length > ACTIVITY_PREVIEW}
              <button
                class="mt-1.5 text-micro font-medium text-fg-muted hover:text-fg"
                type="button"
                onclick={() => (showAllActivity = !showAllActivity)}
                >{showAllActivity
                  ? "Show fewer"
                  : `Show ${activity.length - ACTIVITY_PREVIEW} more`}</button
              >
            {/if}
          {:else}
            <p
              class="rounded-md border border-line bg-bg-soft px-4 py-3 text-meta text-fg-muted"
            >
              No notes or messages yet.
            </p>
          {/if}
        </section>

        <section aria-labelledby="agent-tasks">
          <h2 id="agent-tasks" class="ui-label">Recent tasks</h2>
          {#if detail?.recent_cards?.length}
            <ul
              class="overflow-hidden rounded-md border border-line bg-bg-soft"
            >
              {#each detail.recent_cards as card (card.id)}
                <li
                  class="flex items-center gap-3 border-t border-line-subtle px-4 py-2 first:border-t-0"
                >
                  <a
                    class="min-w-0 flex-1 truncate text-meta text-fg hover:underline"
                    href={workspaceHref(
                      taskPath(card.ref || `card:${card.handle}`),
                    )}>{card.title}</a
                  >
                  <span class="shrink-0 text-micro text-fg-muted"
                    >{phaseLabel(card.column_key)}</span
                  >
                  <span
                    class="w-16 shrink-0 text-right text-micro tabular-nums text-fg-subtle"
                    title={formatAbsoluteDateTime(card.updated_at)}
                    >{formatAge(card.updated_at, now)}</span
                  >
                </li>
              {/each}
            </ul>
          {:else}
            <p
              class="rounded-md border border-line bg-bg-soft px-4 py-3 text-meta text-fg-muted"
            >
              No recent tasks.
            </p>
          {/if}
        </section>
      </div>

      <aside class="space-y-3 lg:pt-[22px]" aria-labelledby="agent-identity">
        <div
          class="rounded-md border border-line-subtle bg-bg-soft px-4 py-3 text-meta"
        >
          <h2 id="agent-identity" class="ui-label">Identity</h2>
          <dl class="space-y-1.5">
            <div class="flex items-baseline justify-between gap-3">
              <dt class="text-fg-subtle">Host</dt>
              <dd class="min-w-0 truncate text-fg">
                {#if agent.host_slug}
                  <a
                    class="hover:underline"
                    href={workspaceHref(
                      `/access#host-${encodeURIComponent(agent.host_slug)}`,
                    )}>{host?.display_name || agent.host_slug}</a
                  >
                {:else}
                  <span class="text-fg-muted">None (standalone)</span>
                {/if}
              </dd>
            </div>
            {#if host}
              <div class="flex items-baseline justify-between gap-3">
                <dt class="text-fg-subtle">Machine</dt>
                <dd class="min-w-0 truncate text-fg-muted">
                  {host.os_user}@{host.hostname}
                </dd>
              </div>
            {/if}
            <div class="flex items-baseline justify-between gap-3">
              <dt class="text-fg-subtle">Name</dt>
              <dd class="text-fg-muted">
                {agent.name}
                <span class="text-fg-subtle">({agentKindLabel(agent)})</span>
              </dd>
            </div>
            <div class="flex items-baseline justify-between gap-3">
              <dt class="text-fg-subtle">Identity</dt>
              <dd class="text-fg-muted">
                {agent.identity_kind === "adopted"
                  ? "Adopted into host"
                  : agent.identity_kind === "standalone"
                    ? "Standalone (legacy)"
                    : "Derived from host"}
              </dd>
            </div>
            {#if principal?.created_at}
              <div class="flex items-baseline justify-between gap-3">
                <dt class="text-fg-subtle">Created</dt>
                <dd
                  class="text-fg-muted"
                  title={formatAbsoluteDateTime(principal.created_at)}
                >
                  {formatAge(principal.created_at, now)} ago
                </dd>
              </div>
            {/if}
            <div class="flex items-baseline justify-between gap-3">
              <dt class="text-fg-subtle">Agent ID</dt>
              <dd class="min-w-0">
                <CopyableId value={agent.id} label="Copy agent id" />
              </dd>
            </div>
            {#if agent.actor_id && agent.actor_id !== agent.id}
              <div class="flex items-baseline justify-between gap-3">
                <dt class="text-fg-subtle">Actor ID</dt>
                <dd class="min-w-0">
                  <CopyableId value={agent.actor_id} label="Copy actor id" />
                </dd>
              </div>
            {/if}
          </dl>

          {#if canManage && !agent.revoked_at}
            <div class="mt-3 border-t border-line-subtle pt-3">
              {#if agent.host_id}
                {#if hostExcludes}
                  <p class="text-micro text-fg-muted">
                    {agent.host_slug} may not act as {agent.name}.
                    <a
                      class="text-accent-text hover:underline"
                      href={workspaceHref(
                        `/access#host-${encodeURIComponent(agent.host_slug)}`,
                      )}>Edit exclusions</a
                    >
                  </p>
                {:else if excludeOpen}
                  <div class="space-y-2" data-agent-exclude-confirm>
                    <p class="text-micro text-fg-muted">
                      {agent.host_slug} will no longer be able to act as
                      {agent.name}. Its open sessions end now; its history
                      stays.
                    </p>
                    {#if excludeError}
                      <p class="text-micro text-danger-text" role="alert">
                        {excludeError}
                      </p>
                    {/if}
                    <div class="flex gap-2">
                      <Button
                        variant="destructive"
                        size="compact"
                        busy={excluding}
                        onclick={excludeOnHost}
                      >
                        {excluding ? "Excluding…" : `Exclude ${agent.name}`}
                      </Button>
                      <Button
                        variant="ghost"
                        size="compact"
                        onclick={() => (excludeOpen = false)}>Cancel</Button
                      >
                    </div>
                  </div>
                {:else}
                  <button
                    class="text-micro text-fg hover:underline"
                    type="button"
                    onclick={() => (excludeOpen = true)}
                    >Exclude on {agent.host_slug}…</button
                  >
                  <p class="text-micro text-fg-subtle">
                    To cut off every agent on the machine, revoke the host in
                    <a
                      class="text-fg-muted hover:underline"
                      href={workspaceHref(
                        `/access#host-${encodeURIComponent(agent.host_slug)}`,
                      )}>Access</a
                    >.
                  </p>
                {/if}
              {/if}
              <div class="mt-2" data-agent-revoke>
                {#if revokeOpen}
                  <div class="space-y-2">
                    <p class="text-micro text-fg-muted">
                      {#if agent.host_id}
                        Permanent: {agent.handle} can never act again and
                        {agent.host_slug} cannot use the name {agent.name}. Its
                        history stays. To pause it instead, exclude it.
                      {:else}
                        {agent.handle} loses access now. Its history stays.
                      {/if}
                    </p>
                    {#if revokeError}
                      <p class="text-micro text-danger-text" role="alert">
                        {revokeError}
                      </p>
                    {/if}
                    <div class="flex gap-2">
                      <Button
                        variant="destructive"
                        size="compact"
                        busy={revoking}
                        onclick={revokeAgent}
                        >{revoking ? "Revoking…" : "Revoke agent"}</Button
                      >
                      <Button
                        variant="ghost"
                        size="compact"
                        onclick={() => (revokeOpen = false)}>Cancel</Button
                      >
                    </div>
                  </div>
                {:else}
                  <button
                    class="text-micro text-danger-text hover:underline"
                    type="button"
                    onclick={() => (revokeOpen = true)}>Revoke agent…</button
                  >
                {/if}
              </div>
            </div>
          {/if}
        </div>
      </aside>
    </div>
  {/if}
</WorkspacePageShell>
