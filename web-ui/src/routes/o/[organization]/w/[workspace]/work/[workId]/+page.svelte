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

<svelte:head
  ><title>{work?.title || "Commitment"} · Agent Nexus</title></svelte:head
>
<WorkspacePageShell>
  <a
    class="w-fit text-micro text-accent-text hover:underline"
    href={workspaceHref("/work")}>← All work</a
  >
  {#if error}<StateError
      title="Commitment unavailable"
      message={error}
      onretry={() => load()}
      retrying={loading}
    />{/if}
  {#if loading && !work}<p class="py-10 text-fg-muted" role="status">
      Loading commitment and evidence…
    </p>{/if}
  {#if work}
    <WorkspacePageHeader title={work.title || "Untitled commitment"}>
      {#snippet subtitle()}<span class="font-mono text-micro">{work.ref}</span
        >{/snippet}
      {#snippet actions()}<button
          class="ui-btn-secondary"
          onclick={() => load()}
          disabled={loading}>{loading ? "Reloading…" : "Reload"}</button
        ><a class="ui-btn-primary" href={pmHref}>Discuss with PM</a>{/snippet}
    </WorkspacePageHeader>
    <div class="flex flex-wrap gap-2">
      <SignalBadge>{label(work.phase)}</SignalBadge><SignalBadge
        tone={signal.tone}>{signal.label}</SignalBadge
      >{#if work.source?.native_status}<SignalBadge
          >Source: {work.source.native_status}</SignalBadge
        >{/if}
    </div>
    <div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_20rem]">
      <div class="min-w-0 space-y-5">
        <section class="rounded-md border border-line bg-panel p-4">
          <h2 class="text-meta font-semibold text-fg">Commitment</h2>
          <p
            class="mt-2 whitespace-pre-wrap break-words text-meta text-fg-muted"
          >
            {work.summary || "No description has been recorded."}
          </p>
          <h3 class="mt-4 text-micro font-semibold text-fg">
            Acceptance criteria
          </h3>
          {#if work.definition_of_done?.length}<ul
              class="mt-2 list-disc space-y-1 pl-5 text-meta text-fg-muted"
            >
              {#each work.definition_of_done as criterion}<li>
                  {criterion}
                </li>{/each}
            </ul>{:else}<p class="mt-1 text-meta text-warn-text">
              Acceptance criteria have not been established.
            </p>{/if}
        </section>
        <section class="rounded-md border border-line bg-panel p-4">
          <h2 class="text-meta font-semibold text-fg">What happens next</h2>
          <div class="mt-3 grid gap-4 sm:grid-cols-2">
            <div>
              <p class="mb-1 text-micro text-fg-muted">Next actor</p>
              {#if work.next_actor}<ActorLabel
                  label={work.next_actor}
                />{:else}<p class="text-fg-muted">Not assigned</p>{/if}
            </div>
            <div>
              <p class="text-micro text-fg-muted">Next action</p>
              <p class="mt-1 break-words text-meta text-fg">
                {work.next_action || "Not established"}
              </p>
            </div>
          </div>
          {#if work.blockers?.length}<div
              class="mt-4 border-t border-line pt-3"
            >
              <h3 class="text-micro font-semibold text-warn-text">Blockers</h3>
              <ul class="mt-1 list-disc space-y-1 pl-5 text-meta text-fg-muted">
                {#each work.blockers as blocker}<li>{blocker}</li>{/each}
              </ul>
            </div>{/if}
          {#if work.wake_condition}<p class="mt-3 text-meta text-fg-muted">
              <strong class="text-fg">Wake condition:</strong>
              {work.wake_condition}
            </p>{/if}
        </section>
        <section class="overflow-hidden rounded-md border border-line bg-panel">
          <header class="border-b border-line p-4">
            <h2 class="text-meta font-semibold text-fg">
              Evidence and observations
            </h2>
            <p class="mt-1 text-micro text-fg-muted">
              A recent check is not proof of progress. Agent reports remain
              claims unless independently verified.
            </p>
            <div class="mt-4"><EvidenceTimes freshness={work.freshness} /></div>
          </header>
          {#if evidenceError}<div class="p-4">
              <StateError
                title="Evidence history unavailable"
                message={evidenceError}
                onretry={() => load()}
              />
            </div>{/if}
          {#if !observations.length && !evidenceError}<p
              class="p-4 text-meta text-fg-muted"
            >
              No observations yet. Completion and source health are not
              established by an empty history.
            </p>{/if}
          <ol class="divide-y divide-line">
            {#each observations as observation, index (observation.id || index)}
              <li class="p-4">
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
                      "Observation time unknown"}</time
                  >
                </div>
                <p class="mt-2 break-words text-micro text-fg-muted">
                  Reader {observation.reader_id || "unknown"} · revision {observation.reader_revision ||
                    "unknown"}{#if observation.source_revision}
                    · source revision <span class="font-mono"
                      >{observation.source_revision}</span
                    >{/if}
                </p>
                {#if observation.error}<p
                    class="mt-2 text-meta text-danger-text"
                  >
                    {typeof observation.error === "string"
                      ? observation.error
                      : observation.error.message || observation.error.code}
                  </p>{/if}
                {#if observation.uncertainty?.length}<ul
                    class="mt-2 list-disc pl-5 text-meta text-warn-text"
                  >
                    {#each observation.uncertainty as item}<li>
                        {item}
                      </li>{/each}
                  </ul>{/if}
                <ul class="mt-2 space-y-2">
                  {#each observation.evidence || [] as evidence}{@const url =
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
                {#if !observation.evidence?.length}<p
                    class="mt-2 text-micro text-fg-muted"
                  >
                    No supporting evidence attached.
                  </p>{/if}
                <details class="mt-3 text-micro text-fg-muted">
                  <summary class="cursor-pointer"
                    >Facts, coverage and provenance</summary
                  >
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
          {#if nextCursor}<div class="border-t border-line p-3">
              <button
                class="ui-btn-secondary"
                disabled={loading}
                onclick={loadMore}
                >{loading ? "Loading evidence…" : "Older observations"}</button
              >
            </div>{/if}
        </section>
        {#if work.executions?.length}<section
            class="rounded-md border border-line bg-panel p-4"
          >
            <h2 class="text-meta font-semibold text-fg">Linked executions</h2>
            <p class="mt-1 text-micro text-fg-muted">
              A completed execution does not establish that acceptance criteria
              were met.
            </p>
            <ul class="mt-3 divide-y divide-line">
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
                  <p class="mt-1 text-micro text-fg-muted">
                    {[
                      execution.host,
                      execution.harness,
                      execution.agent,
                      execution.model,
                    ]
                      .filter(Boolean)
                      .join(" · ") || "Execution context not reported"}
                  </p>
                  {#if execution.result_ref}<p
                      class="mt-1 break-words font-mono text-micro text-fg-muted"
                    >
                      Result: {execution.result_ref}
                    </p>{/if}
                </li>{/each}
            </ul>
          </section>{/if}
        {#if work.relations?.length}<section
            class="rounded-md border border-line bg-panel p-4"
          >
            <h2 class="text-meta font-semibold text-fg">
              Related work and artifacts
            </h2>
            <ul class="mt-2 space-y-2 text-meta">
              {#each work.relations as relation}<li class="break-words">
                  <span class="text-fg-muted"
                    >{relation.kind}:
                  </span>{#if relation.ref?.startsWith("card:")}<a
                      class="text-accent-text hover:underline"
                      href={workspaceHref(
                        `/work/${encodeURIComponent(relation.ref)}`,
                      )}>{relation.ref}</a
                    >{:else}<span class="text-fg">{relation.ref}</span>{/if}
                </li>{/each}
            </ul>
          </section>{/if}
      </div>
      <aside class="space-y-4" aria-label="Authority and follow-through">
        <section class="rounded-md border border-line bg-panel p-4">
          <h2 class="text-meta font-semibold text-fg">Source authority</h2>
          <p class="mt-3 font-semibold text-fg">{sourceLabel(work.source)}</p>
          <p class="mt-1 break-words text-micro text-fg-muted">
            {work.source?.native_id || work.ref}
          </p>
          {#if safeSourceHref(work.source?.url)}<a
              class="mt-2 inline-block text-meta text-accent-text hover:underline"
              href={safeSourceHref(work.source.url)}
              target="_blank"
              rel="noreferrer">Open authoritative record ↗</a
            >{/if}
          <p class="mt-3 text-micro text-fg-muted">
            {work.source?.authority === "nexus"
              ? "Nexus owns this commitment."
              : "Source title, owner and workflow are managed by the authoritative source. Request changes through PM."}
          </p>
          <dl class="mt-4 space-y-3 text-micro">
            <div>
              <dt class="text-fg-muted">Accountable owner</dt>
              <dd class="mt-1">
                {#if work.owner}<ActorLabel
                    label={work.owner}
                    size="xs"
                  />{:else}<span class="text-fg-muted">Not assigned</span>{/if}
              </dd>
            </div>
            <div>
              <dt class="text-fg-muted">Project</dt>
              <dd class="mt-1 break-words text-fg">
                {work.project_ref || "No project"}
              </dd>
            </div>
            {#each [["start_at", "Start"], ["due_at", "Due"]] as [field, title]}{#if work[field]}<div
                >
                  <dt class="text-fg-muted">{title}</dt>
                  <dd class="mt-1 text-fg">
                    {formatAbsoluteDateTime(work[field])}
                  </dd>
                </div>{/if}{/each}
          </dl>
        </section>
        <section class="rounded-md border border-line bg-panel p-4">
          <h2 class="text-meta font-semibold text-fg">Collection health</h2>
          <p class="mt-2 text-meta text-fg">
            {work.refresh?.state || "Unknown"}
          </p>
          {#if work.refresh?.last_error}<p
              class="mt-2 break-words text-micro text-warn-text"
            >
              {typeof work.refresh.last_error === "string"
                ? work.refresh.last_error
                : JSON.stringify(work.refresh.last_error)}
            </p>{/if}
          <dl class="mt-3 space-y-2 text-micro">
            {#each [["last_attempt_at", "Last attempt"], ["last_success_at", "Last successful read"], ["next_due_at", "Next due"]] as [field, title]}<div
              >
                <dt class="text-fg-muted">{title}</dt>
                <dd class="text-fg">
                  {formatTimestamp(work.refresh?.[field]) ||
                    "Not scheduled / unknown"}
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
        <section class="rounded-md border border-line bg-panel p-4">
          <h2 class="text-meta font-semibold text-fg">
            Decisions and delivery
          </h2>
          <p class="mt-2 text-micro text-fg-muted">
            Follow instructions from answer to receipt and verified outcome.
          </p>
          <a
            class="mt-3 inline-block text-meta text-accent-text hover:underline"
            href={workspaceHref(
              `/decisions?work_ref=${encodeURIComponent(work.ref || workId)}`,
            )}>View decisions and receipts →</a
          >
        </section>
      </aside>
    </div>
  {/if}
</WorkspacePageShell>
