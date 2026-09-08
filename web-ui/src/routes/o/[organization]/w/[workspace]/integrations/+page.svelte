<script>
  import { onMount } from "svelte";
  import { page } from "$app/stores";
  import { coreClient } from "$lib/coreClient";
  import { initializeAuthSession } from "$lib/authSession";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import {
    errorMessage,
    workFreshness,
    workKey,
  } from "$lib/pm/presentation.js";
  import { integrationGroups } from "$lib/pm/health.js";
  import { formatTimestamp } from "$lib/formatDate";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import SignalBadge from "$lib/components/pm/SignalBadge.svelte";
  let work = $state([]),
    capabilities = $state(null),
    loading = $state(true),
    error = $state(""),
    capabilityError = $state(""),
    nextCursor = $state(""),
    now = $state(Date.now());
  let requestId = 0;
  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  let groups = $derived(integrationGroups(work, now));
  async function load(append = false) {
    const id = ++requestId;
    loading = true;
    error = "";
    capabilityError = "";
    try {
      await initializeAuthSession({
        fetchFn: globalThis.fetch.bind(globalThis),
        workspaceSlug: $page.params.workspace,
        authDriver: "integration-health",
      });
      const results = await Promise.allSettled([
        coreClient.listWork({
          limit: 100,
          cursor: append ? nextCursor : undefined,
        }),
        coreClient.getWorkCapabilities(),
      ]);
      if (id !== requestId) return;
      if (results[0].status === "fulfilled") {
        const result = results[0].value;
        if (!Array.isArray(result.work))
          throw new Error("Invalid work coverage response.");
        const records = append ? [...work, ...result.work] : result.work;
        work = [
          ...new Map(
            records.map((record) => [workKey(record), record]),
          ).values(),
        ];
        nextCursor = result.next_cursor || "";
      } else error = errorMessage(results[0].reason);
      if (results[1].status === "fulfilled")
        capabilities = results[1].value.capabilities;
      else capabilityError = errorMessage(results[1].reason);
    } catch (err) {
      if (id === requestId) error = errorMessage(err);
    } finally {
      if (id === requestId) loading = false;
    }
  }
  onMount(() => {
    void load();
    const timer = setInterval(() => {
      now = Date.now();
    }, 30_000);
    return () => {
      requestId++;
      clearInterval(timer);
    };
  });
</script>

<svelte:head><title>Integration health · Agent Nexus</title></svelte:head>
<WorkspacePageShell>
  <WorkspacePageHeader title="Integration health"
    >{#snippet subtitle()}Collection health and evidence coverage, separate from
      work progress.{/snippet}{#snippet actions()}<button
        class="ui-btn-secondary"
        onclick={() => load()}
        disabled={loading}
        >{loading ? "Loading health…" : "Reload health"}</button
      ><a class="ui-btn-secondary" href={workspaceHref("/work")}>View work</a
      >{/snippet}</WorkspacePageHeader
  >
  {#if error}<StateError
      title="Coverage could not be refreshed"
      message={error}
      onretry={() => load()}
      retrying={loading}
    />{/if}
  <div
    class="rounded-md border border-line bg-bg-soft p-3 text-meta text-fg-muted"
  >
    Coverage of {work.length} loaded commitments{nextCursor
      ? "; more records are available"
      : ""}. Connections with no visible tracked work are not represented here.
    An empty inbox does not establish source health.
  </div>
  {#if capabilities?.refresh_executor_configured === false}<div
      class="rounded-md bg-warn-soft p-3 text-meta text-warn-text"
      role="status"
    >
      A refresh reader is not configured. Requests may be queued, but no
      successful source read has been established.
    </div>{/if}
  {#if loading && !work.length}<p class="py-8 text-fg-muted" role="status">
      Loading source coverage…
    </p>{:else if !groups.length && !error}<section
      class="rounded-md border border-line bg-panel p-6"
    >
      <h2 class="text-subtitle text-fg">No external source coverage yet</h2>
      <p class="mt-2 text-meta text-fg-muted">
        Register source-owned work and an approved reader to begin collecting
        observations. Native commitments remain available in Work.
      </p>
    </section>{/if}
  {#each groups as group (group.key)}
    <section class="overflow-hidden rounded-md border border-line bg-panel">
      <header
        class="flex flex-wrap items-center justify-between gap-3 border-b border-line p-4"
      >
        <div>
          <h2 class="text-meta font-semibold text-fg">{group.label}</h2>
          <p class="mt-1 break-words text-micro text-fg-muted">
            Connection: {group.source.connection_id || "not established"}
          </p>
        </div>
        <div class="flex flex-wrap gap-2">
          {#each [["fresh", "fresh"], ["stale", "stale"], ["error", "failed"], ["unknown", "unknown"]] as [key, title]}{#if group.counts[key]}<SignalBadge
                tone={key === "error" || key === "stale" ? "warn" : "neutral"}
                >{group.counts[key]} {title}</SignalBadge
              >{/if}{/each}
        </div>
      </header>
      <ul class="divide-y divide-line">
        {#each group.items as item (workKey(item))}{@const signal =
            workFreshness(item, now)}
          <li
            class="grid gap-3 px-4 py-3 sm:grid-cols-[minmax(0,1fr)_12rem_12rem]"
          >
            <div class="min-w-0">
              <a
                class="break-words text-meta font-medium text-fg hover:text-accent-text"
                href={workspaceHref(
                  `/work/${encodeURIComponent(workKey(item))}`,
                )}>{item.title || item.ref}</a
              >{#if item.refresh?.last_error}<p
                  class="mt-1 break-words text-micro text-warn-text"
                >
                  {typeof item.refresh.last_error === "string"
                    ? item.refresh.last_error
                    : JSON.stringify(item.refresh.last_error)}
                </p>{/if}
            </div>
            <div>
              <SignalBadge tone={signal.tone}>{signal.label}</SignalBadge>
              <p class="mt-1 text-micro text-fg-muted">
                Observed {formatTimestamp(item.freshness?.last_observed_at) ||
                  "never"}
              </p>
            </div>
            <div class="text-micro text-fg-muted">
              <p>Refresh: {item.refresh?.state || "unknown"}</p>
              <p class="mt-1">
                Next due: {formatTimestamp(item.refresh?.next_due_at) ||
                  "not scheduled"}
              </p>
            </div>
          </li>{/each}
      </ul>
    </section>
  {/each}
  {#if nextCursor}<button
      class="ui-btn-secondary mx-auto"
      disabled={loading}
      onclick={() => load(true)}>Load more coverage</button
    >{/if}
  <section class="rounded-md border border-line bg-panel p-4">
    <h2 class="text-meta font-semibold text-fg">Available capabilities</h2>
    {#if capabilityError}<StateError
        message={capabilityError}
        onretry={() => load()}
      />{:else if capabilities}<p class="mt-1 text-micro text-fg-muted">
        Reported by this workspace. Capability support is separate from a
        successful source connection.
      </p>
      <details class="mt-3 text-micro text-fg-muted">
        <summary class="cursor-pointer">Inspect capability details</summary>
        <pre
          class="mt-2 max-h-80 overflow-auto whitespace-pre-wrap break-words rounded bg-bg-soft p-3 font-mono">{JSON.stringify(
            capabilities,
            null,
            2,
          )}</pre>
      </details>{:else}<p class="mt-2 text-micro text-fg-muted">
        Capabilities have not been established.
      </p>{/if}
  </section>
</WorkspacePageShell>
