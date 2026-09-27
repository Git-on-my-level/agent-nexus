<script>
  import { decisionRowStatus } from "$lib/inboxMailbox.js";
  import { onMount } from "svelte";
  import { page } from "$app/stores";
  import { coreClient } from "$lib/coreClient";
  import {
    liveWorkspaceEvents,
    TASK_LIST_EVENT_TYPES,
  } from "$lib/liveWorkspaceEvents.js";
  import { initializeAuthSession } from "$lib/authSession";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { formatAbsoluteDateTime, formatTimestamp } from "$lib/formatDate";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import ActorLabel from "$lib/components/ActorLabel.svelte";
  import {
    actorDisplayLabel,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import SignalBadge from "$lib/components/pm/SignalBadge.svelte";
  import ReceiptSignal from "$lib/components/pm/ReceiptSignal.svelte";
  import {
    decisionPayload,
    decisionTitle,
    safeSourceHref,
    sourceLabel,
    isNexusOwned,
    workFreshness,
    workKey,
    label,
    errorMessage,
    readErrorExplanation,
    receiptSignal,
    workSourceKey,
    humanizeInstants,
    connectionName,
  } from "$lib/pm/presentation.js";
  import {
    evidenceSources,
    observationHistory,
    observationErrorText,
    observationStatusLabel,
    observationStatusTone,
  } from "$lib/pm/evidence.js";
  let work = $state(null),
    observations = $state([]),
    nextCursor = $state(""),
    loading = $state(true),
    error = $state(""),
    evidenceError = $state(""),
    decisions = $state([]),
    actions = $state([]),
    decisionsError = $state(""),
    decisionsLoading = $state(false),
    notice = $state(""),
    refreshing = $state(false),
    ready = $state(false);
  let requestId = 0;
  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  let workId = $derived($page.params.workId);
  let signal = $derived(workFreshness(work));
  let pmHref = $derived(
    workspaceHref(`/pm?work_ref=${encodeURIComponent(work?.ref || workId)}`),
  );
  let sourceName = $derived(sourceLabel(work?.source));
  let nexusOwned = $derived(work ? isNexusOwned(work) : false);
  let lastCheckedAt = $derived(work?.freshness?.last_observed_at || "");
  let refreshPending = $derived(
    ["queued", "running"].includes(work?.refresh?.state),
  );
  let refreshError = $derived(readErrorExplanation(work?.refresh?.last_error));
  let lastAttemptAt = $derived(work?.refresh?.last_attempt_at || "");
  let failedAttempts = $derived(Number(work?.refresh?.failures) || 0);
  let sources = $derived(evidenceSources(observations, work));
  // Evidence links are distinct already; a long comment thread still gets a
  // short list first.
  const LINK_PREVIEW = 6;
  let showAllLinks = $state(false);
  // Other tasks that mirror this same source item through another connection.
  // The Tasks table keeps one row per source item; the rest live here.
  let mirrors = $state([]);
  let hasNext = $derived(
    Boolean(
      work?.next_actor ||
      work?.next_action ||
      work?.blockers?.length ||
      work?.wake_condition,
    ),
  );
  let hasDetails = $derived(
    Boolean(work?.relations?.length || work?.executions?.length),
  );
  $effect(() => {
    const id = workId;
    if (ready) void load(id);
  });
  async function load(id = workId) {
    const ticket = ++requestId;
    loading = true;
    error = "";
    evidenceError = "";
    decisionsError = "";
    refreshing = false;
    notice = "";
    work = null;
    observations = [];
    decisions = [];
    mirrors = [];
    showAllLinks = false;
    nextCursor = "";
    const results = await Promise.allSettled([
      coreClient.getWork(id),
      coreClient.listWorkObservations(id, { limit: 30 }),
    ]);
    if (ticket !== requestId) return;
    if (results[0].status === "fulfilled") {
      work = results[0].value.work;
      if (!work) error = "The workspace did not return this task.";
      else {
        void loadDecisions(ticket, work);
        void loadMirrors(ticket, work);
      }
    } else error = errorMessage(results[0].reason);
    if (results[1].status === "fulfilled") {
      observations = results[1].value.observations || [];
      nextCursor = results[1].value.next_cursor || "";
    } else evidenceError = errorMessage(results[1].reason);
    loading = false;
  }
  // The record only, in place: a live change must not blank the page the
  // operator is reading.
  async function refreshWorkRecord() {
    const ticket = requestId;
    try {
      const result = await coreClient.getWork(workId);
      if (ticket === requestId && result?.work) work = result.work;
    } catch {
      // The next change or Reload tries again.
    }
  }
  async function loadDecisions(ticket, loadedWork) {
    decisionsLoading = true;
    decisionsError = "";
    decisions = [];
    try {
      const items = [];
      let cursor;
      for (let page = 0; page < 10; page += 1) {
        const result = await coreClient.listPmDecisions({ limit: 200, cursor });
        items.push(...(result.items || []));
        cursor = result.next_cursor || "";
        if (!cursor) break;
      }
      // Receipts decide what an answered decision reads as; fail soft.
      const receipts = [];
      try {
        let actionCursor;
        for (let page = 0; page < 10; page += 1) {
          const result = await coreClient.listPmActions({
            limit: 200,
            cursor: actionCursor,
          });
          receipts.push(...(result.items || []));
          actionCursor = result.next_cursor || "";
          if (!actionCursor) break;
        }
      } catch {
        // The decision status still renders without receipts.
      }
      if (ticket !== requestId) return;
      actions = receipts;
      const ref = workKey(loadedWork);
      decisions = items
        .filter((decision) => decision.work_ref === ref)
        .sort(
          (a, b) =>
            Date.parse(b.created_at || 0) - Date.parse(a.created_at || 0),
        );
    } catch (err) {
      if (ticket === requestId) decisionsError = errorMessage(err);
    } finally {
      if (ticket === requestId) decisionsLoading = false;
    }
  }
  async function loadMirrors(ticket, loadedWork) {
    const key = workSourceKey(loadedWork);
    if (!key) return;
    try {
      const result = await coreClient.listWork({
        source: loadedWork.source.authority,
        limit: 200,
      });
      if (ticket !== requestId) return;
      mirrors = (Array.isArray(result?.work) ? result.work : []).filter(
        (other) =>
          workSourceKey(other) === key &&
          workKey(other) !== workKey(loadedWork),
      );
    } catch {
      // Fail soft: the disclosure is context, not the page.
    }
  }
  async function loadMore() {
    if (loading) return;
    const ticket = requestId;
    loading = true;
    evidenceError = "";
    try {
      const result = await coreClient.listWorkObservations(workId, {
        limit: 30,
        cursor: nextCursor,
      });
      if (ticket === requestId) {
        observations = [...observations, ...(result.observations || [])];
        nextCursor = result.next_cursor || "";
      }
    } catch (err) {
      if (ticket === requestId) evidenceError = errorMessage(err);
    } finally {
      if (ticket === requestId) loading = false;
    }
  }
  async function refresh() {
    refreshing = true;
    notice = "";
    const ticket = requestId;
    try {
      const result = await coreClient.requestWorkRefresh(workId);
      if (ticket !== requestId) return;
      work = { ...work, refresh: result.refresh };
      notice = `Refresh ${result.refresh?.state || "requested"}. Evidence changes only after a reader reports back.`;
    } catch (err) {
      if (ticket === requestId)
        evidenceError = `The check could not be requested: ${errorMessage(err)}`;
    } finally {
      if (ticket === requestId) refreshing = false;
    }
  }
  onMount(() => {
    let disposed = false;
    // A move or assignment made elsewhere (the ⌘K palette, an agent, the
    // board) re-reads this task; evidence still refreshes on Reload.
    const stopLive = liveWorkspaceEvents({
      client: coreClient,
      types: TASK_LIST_EVENT_TYPES,
      filter: (event) =>
        Array.isArray(event.refs) && event.refs.includes(work?.ref || workId),
      onChange: () => {
        if (ready && work) void refreshWorkRecord();
      },
    });
    initializeAuthSession({
      fetchFn: globalThis.fetch.bind(globalThis),
      workspaceSlug: $page.params.workspace,
      authDriver: "work-detail",
    })
      .then(() => {
        if (!disposed) ready = true;
      })
      .catch((err) => {
        error = errorMessage(err);
        loading = false;
      });
    return () => {
      disposed = true;
      requestId++;
      stopLive();
    };
  });
</script>

<svelte:head><title>{work?.title || "Task"} · Agent Nexus</title></svelte:head>
<WorkspacePageShell>
  <a
    class="w-fit text-micro text-fg-muted hover:text-fg"
    href={workspaceHref("/tasks")}>← Tasks</a
  >
  {#if error}<StateError
      title="Task unavailable"
      message={error}
      onretry={() => load()}
      retrying={loading}
    />{/if}
  {#if loading && !work}<p class="py-10 text-meta text-fg-muted" role="status">
      Loading…
    </p>{/if}
  {#if work}
    <WorkspacePageHeader title={work.title || "Untitled task"}>
      {#snippet subtitle()}
        <span class="flex flex-wrap items-center gap-1.5">
          <SignalBadge tone={work.phase === "blocked" ? "warn" : "neutral"}
            >{label(work.phase)}</SignalBadge
          >
          {#if !nexusOwned}
            <SignalBadge tone={signal.tone}>{signal.label}</SignalBadge>
          {/if}
          {#if work.source?.native_status}
            <SignalBadge>{work.source.native_status}</SignalBadge>
          {/if}
          {#if !work.definition_of_done?.length}
            <!-- A badge, not a sentence: the reader needs the fact, not a
                 lecture about what a finished run cannot do. -->
            <SignalBadge tone="warn">No acceptance criteria</SignalBadge>
          {/if}
          <button
            class="ui-prose-link text-micro"
            type="button"
            title={work.ref}
            onclick={() => navigator.clipboard?.writeText(work.ref)}
            >Copy ref</button
          >
        </span>
      {/snippet}
      {#snippet actions()}<button
          class="ui-btn-secondary"
          onclick={() => load()}
          disabled={loading}>{loading ? "Reloading…" : "Reload"}</button
        ><a class="ui-btn-primary" href={pmHref}>Ask PM</a>{/snippet}
    </WorkspacePageHeader>
    <div class="grid gap-6 xl:grid-cols-[minmax(0,1fr)_18rem]">
      <div class="min-w-0 space-y-7">
        {#if work.summary}
          <p class="whitespace-pre-wrap break-words text-meta text-fg">
            {work.summary}
          </p>
        {/if}
        {#if work.definition_of_done?.length}
          <section>
            <h2 class="ui-label">Done when</h2>
            <ul class="list-disc space-y-1 break-words pl-5 text-meta text-fg">
              {#each work.definition_of_done as criterion}<li>
                  {criterion}
                </li>{/each}
            </ul>
          </section>
        {/if}
        {#if hasNext}
          <section>
            <h2 class="ui-label">Next</h2>
            <p class="text-meta text-fg">
              {#if work.next_actor}<ActorLabel
                  label={actorDisplayLabel(
                    work.next_actor,
                    $actorRegistry,
                    $principalRegistry,
                  )}
                  seed={work.next_actor}
                  size="xs"
                />{:else}<span class="text-fg-muted">Nobody assigned</span>{/if}
              {#if work.next_action}
                <span class="ml-1">— {work.next_action}</span>
              {/if}
            </p>
            {#if work.blockers?.length}
              <ul
                class="mt-2 list-disc space-y-1 break-words pl-5 text-meta text-warn-text"
              >
                {#each work.blockers as blocker}<li>{blocker}</li>{/each}
              </ul>
            {/if}
            {#if work.wake_condition}<p class="mt-2 text-micro text-fg-muted">
                Wakes when: {work.wake_condition}
              </p>{/if}
          </section>
        {/if}
        <section>
          <h2 class="ui-label">Evidence</h2>
          {#if nexusOwned && !lastCheckedAt && !observations.length}
            <p class="text-meta text-fg-muted">
              Created here — nothing to check
            </p>
          {:else}
            <!--
              One line per source, not one block per read. A reader reports
              the item and every comment on it each time it runs, so four
              reads used to print twenty identical links. The line says what
              was read, how often and when; the links follow once; the
              read-by-read history is a disclosure.
            -->
            {#each sources as source (source.key)}
              {@const visibleLinks = showAllLinks
                ? source.links
                : source.links.slice(0, LINK_PREVIEW)}
              <div class="flex flex-wrap items-center gap-x-2 gap-y-1.5">
                <p class="text-meta text-fg" data-evidence-source>
                  <span class="font-medium">{source.name}</span><span
                    class="text-fg-muted"
                    >{` · ${source.reads === 1 ? "1 observation" : `${source.reads} observations`}`}{#if source.lastAt}{" · last "}<time
                        datetime={source.lastAt}
                        title={formatAbsoluteDateTime(source.lastAt)}
                        >{formatTimestamp(source.lastAt)}</time
                      >{/if}</span
                  >
                </p>
                {#if source.latestRead}
                  <SignalBadge tone={observationStatusTone(source.latestRead)}
                    >{observationStatusLabel(source.latestRead)}</SignalBadge
                  >
                {/if}
              </div>
              {#if source.latest && observationErrorText(source.latest) && source.latest.status === "error"}
                <p class="mt-1 break-words text-micro text-warn-text">
                  Last read failed <time
                    datetime={source.latest.observed_at}
                    title={formatAbsoluteDateTime(source.latest.observed_at)}
                    >{formatTimestamp(source.latest.observed_at)}</time
                  >: {humanizeInstants(observationErrorText(source.latest))}
                </p>
              {/if}
              {#if source.latestRead?.uncertainty?.length}
                <ul
                  class="mt-1.5 list-disc break-words pl-5 text-meta text-warn-text"
                >
                  {#each source.latestRead.uncertainty as item}<li>
                      {item}
                    </li>{/each}
                </ul>
              {/if}
              {#if source.links.length}
                <ul
                  class="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-meta"
                  aria-label="Evidence links"
                >
                  {#each visibleLinks as link (link.key)}
                    <li class="min-w-0 break-words">
                      {#if link.href}<a
                          class="text-accent-text hover:underline"
                          href={link.href}
                          target="_blank"
                          rel="noreferrer"
                          title={link.href}>{link.label} ↗</a
                        >{:else}<span class="text-fg-muted">{link.label}</span
                        >{/if}
                    </li>
                  {/each}
                  {#if source.links.length > LINK_PREVIEW}
                    <li>
                      <button
                        class="ui-prose-link text-micro"
                        type="button"
                        aria-expanded={showAllLinks}
                        onclick={() => (showAllLinks = !showAllLinks)}
                        >{showAllLinks
                          ? "Show fewer"
                          : `${source.links.length - LINK_PREVIEW} more`}</button
                      >
                    </li>
                  {/if}
                </ul>
              {/if}
            {:else}
              <p class="text-meta text-fg-muted">
                {lastCheckedAt ? "No reads loaded" : `Never read ${sourceName}`}
              </p>
            {/each}
            <div class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-2">
              {#if !nexusOwned}
                <button
                  class="ui-btn-secondary"
                  onclick={refresh}
                  disabled={refreshing || refreshPending}
                  >{refreshing
                    ? "Checking…"
                    : refreshPending
                      ? "Check pending"
                      : `Check ${sourceName} now`}</button
                >
              {/if}
              {#if lastAttemptAt && failedAttempts && !(sources[0]?.latest?.status === "error")}
                <span class="text-micro text-fg-muted"
                  >Last attempt <time
                    datetime={lastAttemptAt}
                    title={formatAbsoluteDateTime(lastAttemptAt)}
                    >{formatTimestamp(lastAttemptAt)}</time
                  >, {failedAttempts} failed</span
                >
              {/if}
            </div>
          {/if}
          {#if refreshError && sources[0]?.latest?.status !== "error"}
            <p class="mt-2 break-words text-micro text-warn-text">
              {refreshError}
            </p>
          {/if}
          {#if notice}<p class="mt-2 text-micro text-fg-muted" role="status">
              {notice}
            </p>{/if}
          {#if evidenceError}<div class="mt-3">
              <StateError
                title="Evidence history unavailable"
                message={evidenceError}
                onretry={() => load()}
              />
            </div>{/if}
          {#if observations.length}
            <details class="mt-3 text-micro text-fg-muted">
              <summary class="w-fit cursor-pointer"
                >Observation history{nextCursor
                  ? ""
                  : ` (${observations.length})`}</summary
              >
              <ol
                class="mt-2 divide-y divide-line-subtle border-t border-line-subtle"
              >
                {#each observationHistory(observations) as row, index (row.observation.id || index)}
                  {@const observation = row.observation}
                  <li class="flex flex-wrap items-center gap-x-2 gap-y-1 py-2">
                    <SignalBadge tone={observationStatusTone(observation)}
                      >{observationStatusLabel(
                        observation,
                      )}{#if row.count > 1}{" "}× {row.count}{/if}</SignalBadge
                    >
                    <span class="text-micro text-fg-muted">
                      {#if row.count > 1}between <time
                          datetime={row.oldest}
                          title={formatAbsoluteDateTime(row.oldest)}
                          >{formatTimestamp(row.oldest)}</time
                        > and{/if}
                      <time
                        datetime={observation.observed_at}
                        title={formatAbsoluteDateTime(observation.observed_at)}
                        >{formatTimestamp(observation.observed_at) ||
                          "time unknown"}</time
                      >
                    </span>
                    {#if row.group}
                      {#if row.message}<span
                          class="basis-full break-words text-micro text-danger-text"
                          >{humanizeInstants(row.message)}</span
                        >{/if}
                    {:else if observation.evidence?.length}
                      <span class="text-micro text-fg-subtle"
                        >· {observation.evidence.length === 1
                          ? "1 link"
                          : `${observation.evidence.length} links`}</span
                      >
                    {/if}
                  </li>
                {/each}
              </ol>
              {#if nextCursor}<button
                  class="ui-btn-secondary mt-2"
                  disabled={loading}
                  onclick={loadMore}
                  >{loading ? "Loading…" : "Older observations"}</button
                >{/if}
            </details>
          {/if}
          {#if mirrors.length}
            <details class="mt-2 text-micro text-fg-muted">
              <summary class="w-fit cursor-pointer"
                >Also tracked through {mirrors.length === 1
                  ? "another connection"
                  : `${mirrors.length} other connections`}</summary
              >
              <ul class="mt-2 space-y-1">
                {#each mirrors as mirror (workKey(mirror))}
                  {@const mirrorRead = workFreshness(mirror)}
                  <li class="flex flex-wrap items-center gap-2">
                    <a
                      class="text-accent-text hover:underline"
                      href={workspaceHref(
                        `/tasks/${encodeURIComponent(workKey(mirror))}`,
                      )}
                      >{mirror.source?.connection_id
                        ? connectionName(mirror.source)
                        : "Other connection"}</a
                    >
                    <SignalBadge tone={mirrorRead.tone}
                      >{mirrorRead.label}</SignalBadge
                    >
                  </li>
                {/each}
              </ul>
            </details>
          {/if}
        </section>
        <section
          class={!decisionsLoading && !decisions.length && !decisionsError
            ? "hidden"
            : ""}
        >
          <h2 class="ui-label">Decisions</h2>
          {#if decisionsError}<div class="mt-3">
              <StateError
                title="Decisions unavailable"
                message={decisionsError}
                onretry={() => load()}
              />
            </div>{/if}
          {#if decisionsLoading && !decisions.length}<p
              class="mt-3 text-meta text-fg-muted"
              role="status"
            >
              Loading decisions…
            </p>{/if}
          <ul
            class="mt-3 divide-y divide-line-subtle border-t border-line-subtle"
          >
            {#each decisions as decision (decision.id)}
              {@const decisionBadge = receiptSignal(
                decisionRowStatus(decision, actions),
              )}
              <li class="py-3">
                <p class="whitespace-pre-wrap break-words text-meta text-fg">
                  {decisionTitle(decision, work?.title || "")}
                </p>
                {#if decisionPayload(decision)}
                  <details class="mt-1 text-micro text-fg-muted">
                    <summary class="cursor-pointer">Proposed payload</summary>
                    <pre
                      class="mt-2 max-h-80 overflow-auto whitespace-pre-wrap break-words rounded bg-bg-soft p-3 font-mono">{decisionPayload(
                        decision,
                      )}</pre>
                  </details>
                {/if}
                <div class="mt-1.5 flex flex-wrap items-center gap-2">
                  <ReceiptSignal signal={decisionBadge} /><time
                    class="text-micro text-fg-muted"
                    datetime={decision.created_at}
                    title={formatAbsoluteDateTime(decision.created_at)}
                    >{formatTimestamp(decision.created_at) ||
                      "time unknown"}</time
                  >
                  {#if decision.status === "awaiting_answer"}<a
                      class="ui-prose-link text-micro"
                      href={workspaceHref(
                        `/inbox?item=decision:${encodeURIComponent(decision.id)}`,
                      )}>Answer</a
                    >{/if}
                </div>
              </li>
            {/each}
          </ul>
        </section>
        <!--
          One Details disclosure. Relations, run provenance and the raw reader
          reports are the answers to "prove it" — real, occasionally needed,
          and not what the page is for.
        -->
        {#if hasDetails || observations.length}
          <details class="text-micro text-fg-muted">
            <summary class="w-fit cursor-pointer">Details</summary>
            <div class="mt-3 space-y-5">
              {#if work.relations?.length}
                <section>
                  <h2 class="ui-label">Related</h2>
                  <ul class="space-y-1 text-meta">
                    {#each work.relations as relation}<li class="break-words">
                        <span class="text-fg-muted">{relation.kind}</span>
                        {#if relation.ref?.startsWith("card:")}<a
                            class="font-mono text-accent-text hover:underline"
                            href={workspaceHref(
                              `/tasks/${encodeURIComponent(relation.ref)}`,
                            )}>{relation.ref}</a
                          >{:else}<span class="font-mono text-fg"
                            >{relation.ref}</span
                          >{/if}
                      </li>{/each}
                  </ul>
                </section>
              {/if}
              {#if work.executions?.length}
                <section>
                  <h2 class="ui-label">Runs</h2>
                  <ul class="divide-y divide-line-subtle">
                    {#each work.executions as execution}<li
                        class="break-words py-2 text-meta text-fg"
                      >
                        {#if safeSourceHref(execution.url)}<a
                            class="text-accent-text hover:underline"
                            href={safeSourceHref(execution.url)}
                            target="_blank"
                            rel="noreferrer"
                            >{execution.authority} · {execution.run_id} ↗</a
                          >{:else}{execution.authority} · {execution.run_id}{/if}
                        <p class="mt-0.5 text-micro text-fg-muted">
                          {[
                            execution.host,
                            execution.harness,
                            execution.agent,
                            execution.model,
                          ]
                            .filter(Boolean)
                            .join(" · ") ||
                            "Context not reported"}{#if execution.result_ref}
                            · <span class="font-mono"
                              >{execution.result_ref}</span
                            >{/if}
                        </p>
                      </li>{/each}
                  </ul>
                </section>
              {/if}
              {#if observations.length}
                <section>
                  <h2 class="ui-label">Reader reports</h2>
                  <ul class="space-y-3">
                    {#each observations as observation, index (observation.id || index)}
                      <li>
                        <p class="break-words text-micro text-fg-subtle">
                          {observation.reader_id ||
                            "unknown reader"}{#if observation.reader_revision}
                            @{observation.reader_revision}{/if}{#if observation.source_revision}
                            · <span class="font-mono"
                              >{observation.source_revision}</span
                            >{/if}
                        </p>
                        <pre
                          class="mt-1 max-h-72 overflow-auto whitespace-pre-wrap break-words rounded bg-bg-soft p-3 font-mono text-micro">{JSON.stringify(
                            {
                              facts: observation.facts,
                              coverage: observation.coverage,
                              actor_id: observation.actor_id,
                              received_at: observation.received_at,
                              status: observation.status,
                              verification: observation.verification,
                            },
                            null,
                            2,
                          )}</pre>
                      </li>
                    {/each}
                  </ul>
                </section>
              {/if}
            </div>
          </details>
        {/if}
      </div>
      <!-- `break-words` wraps text but does not shrink the column's intrinsic
           minimum, so a long project ref or source id used to widen the whole
           grid past the viewport. -->
      <aside
        class="min-w-0 space-y-6 text-meta"
        aria-label="Source and follow-through"
      >
        <section>
          <h2 class="ui-label">Source</h2>
          <dl class="mt-2 space-y-2">
            <div>
              <dt class="text-micro text-fg-subtle">Authority</dt>
              <dd class="break-words text-fg">
                {sourceLabel(work.source)}{#if work.source?.native_id}
                  <span class="text-fg-subtle"> · </span><span
                    class="font-mono text-fg-muted"
                    >{work.source.native_id}</span
                  >{/if}
              </dd>
              {#if safeSourceHref(work.source?.url)}<dd>
                  <a
                    class="text-accent-text hover:underline"
                    href={safeSourceHref(work.source.url)}
                    target="_blank"
                    rel="noreferrer"
                    data-task-source-link
                    aria-keyshortcuts="O"
                    title="Open source record (O)">Open source record ↗</a
                  >
                </dd>{/if}
            </div>
            <div>
              <dt class="text-micro text-fg-subtle">Owner</dt>
              <dd>
                {#if work.owner}<ActorLabel
                    label={actorDisplayLabel(
                      work.owner,
                      $actorRegistry,
                      $principalRegistry,
                    )}
                    seed={work.owner}
                    size="xs"
                  />{:else}<span class="text-fg-muted">Not assigned</span>{/if}
              </dd>
            </div>
            {#if work.project_ref}
              <div>
                <dt class="text-micro text-fg-subtle">Project</dt>
                <dd class="break-words text-fg">{work.project_ref}</dd>
              </div>
            {/if}
            {#each [["start_at", "Start"], ["due_at", "Due"]] as [field, title]}{#if work[field]}<div
                >
                  <dt class="text-micro text-fg-subtle">{title}</dt>
                  <dd class="text-fg">
                    {formatAbsoluteDateTime(work[field])}
                  </dd>
                </div>{/if}{/each}
          </dl>
          {#if work.source?.authority !== "nexus"}
            <p class="mt-2 text-micro text-fg-subtle">
              Status and workflow are owned by the source. Ask PM to request a
              change.
            </p>
          {/if}
        </section>
        <section>
          <h2 class="ui-label">Inbox</h2>
          <a
            class="mt-2 inline-block text-accent-text hover:underline"
            href={workspaceHref(
              `/inbox?mailbox=watching&work_ref=${encodeURIComponent(work.ref || workId)}`,
            )}>Inbox for this task →</a
          >
        </section>
      </aside>
    </div>
  {/if}
</WorkspacePageShell>
