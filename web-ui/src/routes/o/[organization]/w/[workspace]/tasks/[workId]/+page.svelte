<script>
  import { onMount } from "svelte";
  import { page } from "$app/stores";
  import { coreClient } from "$lib/coreClient";
  import { initializeAuthSession } from "$lib/authSession";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { formatAbsoluteDateTime, formatTimestamp } from "$lib/formatDate";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import ActorLabel from "$lib/components/ActorLabel.svelte";
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
    receiptSignal,
  } from "$lib/pm/presentation.js";
  let work = $state(null),
    observations = $state([]),
    nextCursor = $state(""),
    loading = $state(true),
    error = $state(""),
    evidenceError = $state(""),
    decisions = $state([]),
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
  let refreshError = $derived.by(() => {
    const raw = work?.refresh?.last_error;
    if (!raw) return "";
    return typeof raw === "string" ? raw : JSON.stringify(raw);
  });
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
    nextCursor = "";
    const results = await Promise.allSettled([
      coreClient.getWork(id),
      coreClient.listWorkObservations(id, { limit: 30 }),
    ]);
    if (ticket !== requestId) return;
    if (results[0].status === "fulfilled") {
      work = results[0].value.work;
      if (!work) error = "The workspace did not return this commitment.";
      else void loadDecisions(ticket, work);
    } else error = errorMessage(results[0].reason);
    if (results[1].status === "fulfilled") {
      observations = results[1].value.observations || [];
      nextCursor = results[1].value.next_cursor || "";
    } else evidenceError = errorMessage(results[1].reason);
    loading = false;
  }
  async function loadDecisions(ticket, loadedWork) {
    decisionsLoading = true;
    decisionsError = "";
    decisions = [];
    try {
      const result = await coreClient.listPmDecisions({ limit: 200 });
      if (ticket !== requestId) return;
      const ref = workKey(loadedWork);
      decisions = (result.items || [])
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
        notice = `Refresh request failed: ${errorMessage(err)}`;
    } finally {
      if (ticket === requestId) refreshing = false;
    }
  }
  onMount(() => {
    let disposed = false;
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
          <span class="font-mono text-micro text-fg-subtle">{work.ref}</span>
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
            <ul class="list-disc space-y-1 pl-5 text-meta text-fg">
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
                  label={work.next_actor}
                  size="xs"
                />{:else}<span class="text-fg-muted">Nobody assigned</span>{/if}
              {#if work.next_action}
                <span class="ml-1">— {work.next_action}</span>
              {/if}
            </p>
            {#if work.blockers?.length}
              <ul
                class="mt-2 list-disc space-y-1 pl-5 text-meta text-warn-text"
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
          <!--
            One line, not a three-up grid of timestamps beside a rail of
            collection internals. A reader wants to know when we last read the
            source and how to read it again; "Source activity", "Meaningful
            progress", "Next due" and the collector's state machine were
            answering a question nobody asked.
          -->
          <div class="flex flex-wrap items-center gap-x-3 gap-y-2">
            {#if nexusOwned && !lastCheckedAt}
              <p class="text-meta text-fg-muted">
                Created here — nothing to check
              </p>
            {:else}
              <p class="text-meta text-fg">
                Last checked {#if lastCheckedAt}<time
                    datetime={lastCheckedAt}
                    title={formatAbsoluteDateTime(lastCheckedAt)}
                    >{formatTimestamp(lastCheckedAt)}</time
                  >{:else}never{/if} · {sourceName}
              </p>
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
          </div>
          {#if refreshError}
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
          <ol
            class="mt-3 divide-y divide-line-subtle {observations.length
              ? 'border-t border-line-subtle'
              : ''}"
          >
            {#each observations as observation, index (observation.id || index)}
              <li class="py-3">
                <div class="flex flex-wrap items-center gap-2">
                  <SignalBadge
                    tone={observation.verification === "verified"
                      ? "ok"
                      : observation.status === "error"
                        ? "danger"
                        : observation.status === "uncertain"
                          ? "warn"
                          : "neutral"}
                    >{observation.verification === "verified"
                      ? "Verified evidence"
                      : observation.status === "error"
                        ? "Read failed"
                        : observation.status === "uncertain"
                          ? "Uncertain report"
                          : "Reported claim"}</SignalBadge
                  ><time
                    class="text-micro text-fg-muted"
                    datetime={observation.observed_at}
                    title={formatAbsoluteDateTime(observation.observed_at)}
                    >{formatTimestamp(observation.observed_at) ||
                      "time unknown"}</time
                  >
                </div>
                {#if observation.error}<p
                    class="mt-1.5 text-meta text-danger-text"
                  >
                    {typeof observation.error === "string"
                      ? observation.error
                      : observation.error.message || observation.error.code}
                  </p>{/if}
                {#if observation.uncertainty?.length}<ul
                    class="mt-1.5 list-disc pl-5 text-meta text-warn-text"
                  >
                    {#each observation.uncertainty as item}<li>
                        {item}
                      </li>{/each}
                  </ul>{/if}
                {#if observation.evidence?.length}
                  <ul class="mt-1.5 space-y-1">
                    {#each observation.evidence as evidence}{@const url =
                        safeSourceHref(evidence.url)}
                      <li class="break-words text-meta text-fg">
                        {#if url}<a
                            class="text-accent-text hover:underline"
                            href={url}
                            target="_blank"
                            rel="noreferrer"
                            >{evidence.summary ||
                              evidence.ref ||
                              "Open source evidence"} ↗</a
                          >{:else}{evidence.summary ||
                            evidence.ref ||
                            "Unlinked evidence"}{/if}
                      </li>{/each}
                  </ul>
                {/if}
              </li>
            {/each}
          </ol>
          {#if nextCursor}<button
              class="ui-btn-secondary mt-3"
              disabled={loading}
              onclick={loadMore}
              >{loading ? "Loading…" : "Older observations"}</button
            >{/if}
        </section>
        <section>
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
          {#if !decisionsLoading && !decisions.length && !decisionsError}<p
              class="mt-3 text-meta text-fg-muted"
            >
              No decisions recorded for this task.
            </p>{/if}
          <ul
            class="mt-3 divide-y divide-line-subtle border-t border-line-subtle"
          >
            {#each decisions as decision (decision.id)}
              {@const decisionBadge = receiptSignal(decision.status)}
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
                        class="py-2 text-meta text-fg"
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
                        <p class="text-micro text-fg-subtle">
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
      <aside class="space-y-6 text-meta" aria-label="Source and follow-through">
        <section>
          <h2 class="ui-label">Source</h2>
          <dl class="mt-2 space-y-2">
            <div>
              <dt class="text-micro text-fg-subtle">Authority</dt>
              <dd class="text-fg">
                {sourceLabel(work.source)}{#if work.source?.native_id}
                  <span class="font-mono text-fg-muted"
                    >{work.source.native_id}</span
                  >{/if}
              </dd>
              {#if safeSourceHref(work.source?.url)}<dd>
                  <a
                    class="text-accent-text hover:underline"
                    href={safeSourceHref(work.source.url)}
                    target="_blank"
                    rel="noreferrer">Open source record ↗</a
                  >
                </dd>{/if}
            </div>
            <div>
              <dt class="text-micro text-fg-subtle">Owner</dt>
              <dd>
                {#if work.owner}<ActorLabel
                    label={work.owner}
                    size="xs"
                  />{:else}<span class="text-fg-muted">Not assigned</span>{/if}
              </dd>
            </div>
            <div>
              <dt class="text-micro text-fg-subtle">Project</dt>
              <dd class="break-words text-fg">
                {work.project_ref || "—"}
              </dd>
            </div>
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
          <h2 class="ui-label">Decisions</h2>
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
