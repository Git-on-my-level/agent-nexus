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
  import EvidenceTimes from "$lib/components/pm/EvidenceTimes.svelte";
  import {
    safeSourceHref,
    sourceLabel,
    workFreshness,
    label,
    errorMessage,
  } from "$lib/pm/presentation.js";
  let work = $state(null),
    observations = $state([]),
    nextCursor = $state(""),
    loading = $state(true),
    error = $state(""),
    evidenceError = $state(""),
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
  $effect(() => {
    const id = workId;
    if (ready) void load(id);
  });
  async function load(id = workId) {
    const ticket = ++requestId;
    loading = true;
    error = "";
    evidenceError = "";
    refreshing = false;
    notice = "";
    work = null;
    observations = [];
    nextCursor = "";
    const results = await Promise.allSettled([
      coreClient.getWork(id),
      coreClient.listWorkObservations(id, { limit: 30 }),
    ]);
    if (ticket !== requestId) return;
    if (results[0].status === "fulfilled") {
      work = results[0].value.work;
      if (!work) error = "The workspace did not return this commitment.";
    } else error = errorMessage(results[0].reason);
    if (results[1].status === "fulfilled") {
      observations = results[1].value.observations || [];
      nextCursor = results[1].value.next_cursor || "";
    } else evidenceError = errorMessage(results[1].reason);
    loading = false;
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

<svelte:head><title>{work?.title || "Work"} · Agent Nexus</title></svelte:head>
<WorkspacePageShell>
  <a
    class="w-fit text-micro text-fg-muted hover:text-fg"
    href={workspaceHref("/work")}>← Work</a
  >
  {#if error}<StateError
      title="Work unavailable"
      message={error}
      onretry={() => load()}
      retrying={loading}
    />{/if}
  {#if loading && !work}<p class="py-10 text-meta text-fg-muted" role="status">
      Loading…
    </p>{/if}
  {#if work}
    <WorkspacePageHeader title={work.title || "Untitled work"}>
      {#snippet subtitle()}
        <span class="flex flex-wrap items-center gap-1.5">
          <SignalBadge tone={work.phase === "blocked" ? "warn" : "neutral"}
            >{label(work.phase)}</SignalBadge
          >
          {#if work.source?.authority !== "nexus"}
            <SignalBadge tone={signal.tone}>{signal.label}</SignalBadge>
          {/if}
          {#if work.source?.native_status}
            <SignalBadge>{work.source.native_status}</SignalBadge>
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
        <section>
          {#if work.summary}
            <p class="whitespace-pre-wrap break-words text-meta text-fg">
              {work.summary}
            </p>
          {/if}
          <h2
            class="mt-4 text-micro font-semibold uppercase tracking-wide text-fg-muted"
          >
            Acceptance criteria
          </h2>
          {#if work.definition_of_done?.length}<ul
              class="mt-2 list-disc space-y-1 pl-5 text-meta text-fg"
            >
              {#each work.definition_of_done as criterion}<li>
                  {criterion}
                </li>{/each}
            </ul>{:else}<p class="mt-1 text-meta text-warn-text">
              None recorded. A finished run cannot complete this work.
            </p>{/if}
        </section>
        <section>
          <h2
            class="text-micro font-semibold uppercase tracking-wide text-fg-muted"
          >
            Next
          </h2>
          <p class="mt-2 text-meta text-fg">
            {#if work.next_actor}<ActorLabel
                label={work.next_actor}
                size="xs"
              />{:else}<span class="text-fg-muted">Nobody assigned</span>{/if}
            {#if work.next_action}
              <span class="ml-1">— {work.next_action}</span>
            {/if}
          </p>
          {#if work.blockers?.length}
            <ul class="mt-2 list-disc space-y-1 pl-5 text-meta text-warn-text">
              {#each work.blockers as blocker}<li>{blocker}</li>{/each}
            </ul>
          {/if}
          {#if work.wake_condition}<p class="mt-2 text-micro text-fg-muted">
              Wakes when: {work.wake_condition}
            </p>{/if}
        </section>
        <section>
          <div class="flex flex-wrap items-end justify-between gap-2">
            <h2
              class="text-micro font-semibold uppercase tracking-wide text-fg-muted"
            >
              Evidence
            </h2>
          </div>
          <div class="mt-2"><EvidenceTimes freshness={work.freshness} /></div>
          {#if evidenceError}<div class="mt-3">
              <StateError
                title="Evidence history unavailable"
                message={evidenceError}
                onretry={() => load()}
              />
            </div>{/if}
          {#if !observations.length && !evidenceError}<p
              class="mt-3 text-meta text-fg-muted"
            >
              No observations yet.
            </p>{/if}
          <ol
            class="mt-3 divide-y divide-line-subtle border-t border-line-subtle"
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
                  <span class="text-micro text-fg-subtle">
                    {observation.reader_id ||
                      "unknown reader"}{#if observation.reader_revision}
                      @{observation.reader_revision}{/if}{#if observation.source_revision}
                      · <span class="font-mono"
                        >{observation.source_revision}</span
                      >{/if}
                  </span>
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
                <details class="mt-2 text-micro text-fg-muted">
                  <summary class="cursor-pointer">Facts and coverage</summary>
                  <pre
                    class="mt-2 max-h-72 overflow-auto whitespace-pre-wrap break-words rounded bg-bg-soft p-3 font-mono text-micro">{JSON.stringify(
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
                </details>
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
        {#if work.executions?.length}<section>
            <h2
              class="text-micro font-semibold uppercase tracking-wide text-fg-muted"
            >
              Runs
            </h2>
            <ul class="mt-2 divide-y divide-line-subtle">
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
                      · <span class="font-mono">{execution.result_ref}</span
                      >{/if}
                  </p>
                </li>{/each}
            </ul>
          </section>{/if}
        {#if work.relations?.length}<section>
            <h2
              class="text-micro font-semibold uppercase tracking-wide text-fg-muted"
            >
              Related
            </h2>
            <ul class="mt-2 space-y-1 text-meta">
              {#each work.relations as relation}<li class="break-words">
                  <span class="text-fg-muted">{relation.kind}</span>
                  {#if relation.ref?.startsWith("card:")}<a
                      class="font-mono text-accent-text hover:underline"
                      href={workspaceHref(
                        `/work/${encodeURIComponent(relation.ref)}`,
                      )}>{relation.ref}</a
                    >{:else}<span class="font-mono text-fg">{relation.ref}</span
                    >{/if}
                </li>{/each}
            </ul>
          </section>{/if}
      </div>
      <aside class="space-y-6 text-meta" aria-label="Source and follow-through">
        <section>
          <h2
            class="text-micro font-semibold uppercase tracking-wide text-fg-muted"
          >
            Source
          </h2>
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
          <h2
            class="text-micro font-semibold uppercase tracking-wide text-fg-muted"
          >
            Collection
          </h2>
          <dl class="mt-2 space-y-2">
            <div>
              <dt class="text-micro text-fg-subtle">State</dt>
              <dd class="text-fg">{work.refresh?.state || "unknown"}</dd>
              {#if work.refresh?.last_error}<dd
                  class="break-words text-micro text-warn-text"
                >
                  {typeof work.refresh.last_error === "string"
                    ? work.refresh.last_error
                    : JSON.stringify(work.refresh.last_error)}
                </dd>{/if}
            </div>
            {#each [["last_attempt_at", "Last attempt"], ["last_success_at", "Last successful read"], ["next_due_at", "Next due"]] as [field, title]}<div
              >
                <dt class="text-micro text-fg-subtle">{title}</dt>
                <dd class="text-fg">
                  {formatTimestamp(work.refresh?.[field]) || "—"}
                </dd>
              </div>{/each}
          </dl>
          <button
            class="ui-btn-secondary mt-3"
            onclick={refresh}
            disabled={refreshing ||
              ["queued", "running"].includes(work.refresh?.state)}
            >{refreshing
              ? "Requesting refresh…"
              : ["queued", "running"].includes(work.refresh?.state)
                ? "Refresh pending"
                : "Request source refresh"}</button
          >{#if notice}<p class="mt-2 text-micro text-fg-muted" role="status">
              {notice}
            </p>{/if}
        </section>
        <section>
          <h2
            class="text-micro font-semibold uppercase tracking-wide text-fg-muted"
          >
            Decisions
          </h2>
          <a
            class="mt-2 inline-block text-accent-text hover:underline"
            href={workspaceHref(
              `/decisions?work_ref=${encodeURIComponent(work.ref || workId)}`,
            )}>Decisions and receipts for this work →</a
          >
        </section>
      </aside>
    </div>
  {/if}
</WorkspacePageShell>
