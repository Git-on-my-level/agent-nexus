<script>
  import { onMount, untrack } from "svelte";
  import { authenticatedAgent } from "$lib/authSession";
  import { coreClient } from "$lib/coreClient";
  import {
    actorDisplayLabel,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import { formatAge } from "$lib/agentPresence.js";
  import { formatAbsoluteDateTime } from "$lib/formatDate";
  import {
    createParticipationReader,
    participationState,
    participationSummary,
    participationTasks,
  } from "$lib/taskParticipation.js";
  import ActorLabel from "$lib/components/ActorLabel.svelte";
  import FinePrint from "$lib/components/FinePrint.svelte";

  let {
    tasks = [],
    /**
     * Collapse the whole section to one line when there is nothing to show.
     * On a task page an empty participation section was most of the page's
     * quiet height; on an agent page the section is the point, so it stays.
     */
    quietWhenEmpty = false,
    agentId = "",
    workspaceHref = (path) => path,
    client = coreClient,
  } = $props();
  let read;
  let readerScope = $state("");
  let groups = $state([]);
  let loading = $state(true);
  let ready = $state(false);
  let now = $state(Date.now());
  let generation = 0;
  let inFlightKey = $state("");
  let selected = $derived(participationTasks(tasks));
  let scopeKey = $derived(
    JSON.stringify([
      workspaceHref("/"),
      $authenticatedAgent?.actor_id,
      agentId,
      selected,
    ]),
  );
  let summary = $derived(participationSummary(groups, now));
  let scope = $derived(agentId ? "Recent task participation" : "Participation");
  /** Nothing reported, nothing unavailable, and no longer loading. */
  let empty = $derived(
    !loading &&
      (!selected.length ||
        groups.every(
          (group) => !group.unavailable && !group.participants.length,
        )),
  );

  async function load(key = scopeKey, force = false) {
    if (inFlightKey === key) return;
    const currentScope = JSON.stringify([
      workspaceHref("/"),
      $authenticatedAgent?.actor_id,
    ]);
    if (readerScope !== currentScope) {
      read?.dispose();
      read = createParticipationReader(client);
      readerScope = currentScope;
    }
    inFlightKey = key;
    const ticket = ++generation;
    loading = true;
    const result = await read(selected, { agentId, force });
    if (ticket !== generation) return;
    groups = result;
    now = Date.now();
    loading = false;
    inFlightKey = "";
  }
  $effect(() => {
    const key = scopeKey;
    if (ready)
      untrack(() => {
        groups = [];
        void load(key);
      });
  });
  onMount(() => {
    ready = true;
    const ageTimer = setInterval(() => {
      now = Date.now();
    }, 5_000);
    const refreshTimer = setInterval(() => {
      if (document.visibilityState !== "hidden") void load(scopeKey, true);
    }, 30_000);
    return () => {
      generation++;
      read?.dispose();
      clearInterval(ageTimer);
      clearInterval(refreshTimer);
    };
  });
</script>

{#if quietWhenEmpty && empty}
  <!-- One line, not a section: an empty participation panel used to be four
       lines of explanation about an absence. -->
  <section aria-label={scope} data-participation data-participation-quiet>
    <p class="text-micro text-fg-subtle">
      {scope} · No activity
      <button
        class="ui-prose-link ml-1"
        disabled={loading || !selected.length}
        onclick={() => load(scopeKey, true)}>Check again</button
      >
    </p>
  </section>
{:else}
  <section aria-label={scope} data-participation>
    <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
      <h2 class="ui-label mb-0">{scope}</h2>
      <button
        class="ui-prose-link text-micro"
        disabled={loading || !selected.length}
        onclick={() => load(scopeKey, true)}
      >
        {loading ? "Checking activity…" : "Refresh activity"}
      </button>
    </div>
    {#if agentId}
      <p class="mb-2 text-micro text-fg-muted">
        Shared activity on up to 8 recent or current tasks. Other tasks and
        private sessions are not included. Sessions are counted per task.
      </p>
    {/if}
    {#if loading && !groups.length}
      <p class="text-meta text-fg-muted" role="status">
        Loading shared participation…
      </p>
    {:else if !selected.length}
      <p class="text-meta text-fg-muted">
        No recent task context available. Session activity is unknown.
      </p>
    {:else}
      {#if groups.some((group) => !group.unavailable)}
        <p class="mb-2 text-micro text-fg-muted" data-participation-summary>
          {summary.participating}
          {agentId
            ? summary.participating === 1
              ? "task participation"
              : "task participations"
            : summary.participating === 1
              ? "session participating"
              : "sessions participating"} · {summary.active}
          currently active{summary.partial ? " in available results" : ""}
        </p>
      {/if}
      <div
        class="divide-y divide-line rounded-md border border-line bg-bg-soft"
      >
        {#each groups as group (group.ref)}
          <div class="min-w-0 px-4 py-3">
            {#if agentId && !group.unavailable}
              <a
                class="mb-2 block break-words text-meta text-accent-text hover:text-accent-hover"
                href={workspaceHref(`/tasks/${encodeURIComponent(group.ref)}`)}
                >{group.title || "Open task"}</a
              >
            {/if}
            {#if group.unavailable}
              <p class="text-meta text-fg-muted" role="status">
                Participation unavailable. Access or service availability may
                have changed.
              </p>
            {:else if !group.participants.length}
              <p class="text-meta text-fg-muted">
                No shared participation reported{agentId
                  ? " for this agent on this task"
                  : ""}.
              </p>
            {:else}
              <ul class="space-y-3">
                {#each group.participants as participant, index (participant.id)}
                  {@const state = participationState(participant, now)}
                  <li class="min-w-0" data-participation-row>
                    <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
                      {#if !agentId}
                        <a
                          class="min-w-0 max-w-full hover:underline"
                          href={workspaceHref(
                            `/agents/${encodeURIComponent(participant.agentId || participant.actorId)}`,
                          )}
                        >
                          <ActorLabel
                            label={actorDisplayLabel(
                              participant.actorId || participant.agentId,
                              $actorRegistry,
                              $principalRegistry,
                            )}
                            seed={participant.actorId || participant.agentId}
                            size="xs"
                          />
                        </a>
                      {/if}
                      <span class="text-micro text-fg-muted"
                        >Session {index + 1}</span
                      >
                      <span
                        class="text-micro {state.active
                          ? 'text-accent-text'
                          : 'text-fg-muted'}">{state.label}</span
                      >
                    </div>
                    <p class="mt-1 text-micro text-fg-muted">
                      {#if participant.lastSeenAt}
                        Last reported <time
                          datetime={participant.lastSeenAt}
                          title={formatAbsoluteDateTime(participant.lastSeenAt)}
                          >{formatAge(participant.lastSeenAt, now) === "<1m"
                            ? "just now"
                            : `${formatAge(participant.lastSeenAt, now)} ago`}</time
                        >
                      {:else}Last report unknown{/if}
                      · Agent-reported activity
                    </p>
                  </li>
                {/each}
              </ul>
            {/if}
            {#if group.hasMore}<p class="mt-2 text-micro text-warn-text">
                More participation exists. Counts cover only the sessions shown.
              </p>{/if}
          </div>
        {/each}
      </div>
      <FinePrint label="What participation means">
        Participation does not assign or complete a task. Stale activity does
        not establish whether an agent is offline.
      </FinePrint>
    {/if}
  </section>
{/if}
