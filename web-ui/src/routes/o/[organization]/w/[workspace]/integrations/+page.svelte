<script>
  import { onMount } from "svelte";
  import { page } from "$app/stores";
  import { coreClient } from "$lib/coreClient";
  import { initializeAuthSession } from "$lib/authSession";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import {
    errorMessage,
    sourceLabel,
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

  /**
   * A source error is a JSON payload as often as it is a sentence. Printing
   * `{"code":"permission_denied","detail":…}` at a reader tells them nothing
   * they can act on, so the row says which source we cannot reach and when we
   * will try again; the payload goes under a disclosure for whoever needs it.
   */
  function refreshErrorPayload(item) {
    const raw = item?.refresh?.last_error;
    if (!raw) return "";
    if (typeof raw === "string") return raw;
    try {
      return JSON.stringify(raw, null, 2);
    } catch {
      return String(raw);
    }
  }
  function retryLine(item) {
    const next = formatTimestamp(item?.refresh?.next_due_at);
    return next ? ` · retrying ${next}` : "";
  }
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

<svelte:head><title>Integrations · Agent Nexus</title></svelte:head>
<WorkspacePageShell>
  <WorkspacePageHeader title="Integrations"
    >{#snippet subtitle()}How recently we read each connected tool. Tools with
      no tasks yet aren't listed.{/snippet}{#snippet actions()}<button
        class="ui-btn-secondary"
        onclick={() => load()}
        disabled={loading}
        >{loading ? "Loading health…" : "Reload health"}</button
      ><a class="ui-btn-secondary" href={workspaceHref("/tasks")}>Tasks</a
      >{/snippet}</WorkspacePageHeader
  >
  {#if error}<StateError
      title="Coverage could not be refreshed"
      message={error}
      onretry={() => load()}
      retrying={loading}
    />{/if}
  {#if capabilities?.refresh_executor_configured === false}<p
      class="text-micro text-warn-text"
      role="status"
    >
      No refresh reader is configured: refresh requests queue but nothing reads
      sources.
    </p>{/if}
  {#if loading && !work.length}<p
      class="py-8 text-center text-meta text-fg-muted"
      role="status"
    >
      Loading source coverage…
    </p>{:else if !groups.length && !error}<section class="py-12 text-center">
      <h2 class="text-subtitle font-semibold text-fg">
        No external sources yet
      </h2>
      <p class="mx-auto mt-2 max-w-md text-meta text-fg-muted">
        Register source-owned work and an approved reader to start collecting
        observations.
      </p>
    </section>{/if}
  {#each groups as group (group.key)}
    <section class="overflow-hidden rounded-md border border-line bg-panel">
      <header
        class="flex flex-wrap items-center justify-between gap-3 border-b border-line-subtle px-4 py-3"
      >
        <div class="min-w-0">
          <h2 class="text-meta font-semibold text-fg">{group.label}</h2>
          <p class="mt-0.5 truncate font-mono text-micro text-fg-muted">
            {group.source.connection_id || "no connection id"}
          </p>
        </div>
        <div class="flex flex-wrap gap-1.5">
          {#each [["fresh", "fresh", "ok"], ["stale", "stale", "warn"], ["error", "failed", "warn"], ["unknown", "unknown", "neutral"]] as [key, title, tone]}{#if group.counts[key]}<SignalBadge
                {tone}>{group.counts[key]} {title}</SignalBadge
              >{/if}{/each}
        </div>
      </header>
      <ul class="divide-y divide-line-subtle">
        {#each group.items as item (workKey(item))}{@const signal =
            workFreshness(item, now)}
          <li
            class="grid gap-2 px-4 py-2.5 sm:grid-cols-[minmax(0,1fr)_11rem_13rem] sm:items-center"
          >
            <div class="min-w-0">
              <a
                class="break-words text-meta font-medium text-fg hover:text-accent-text"
                href={workspaceHref(
                  `/tasks/${encodeURIComponent(workKey(item))}`,
                )}>{item.title || item.ref}</a
              >{#if item.refresh?.last_error}
                <p class="mt-0.5 text-micro text-warn-text">
                  Can't reach {sourceLabel(item.source)}{retryLine(item)}
                </p>
                <details class="mt-0.5 text-micro text-fg-muted">
                  <summary class="w-fit cursor-pointer">Details</summary>
                  <pre
                    class="mt-1 max-h-48 overflow-auto whitespace-pre-wrap break-words rounded bg-bg-soft p-2 font-mono text-micro">{refreshErrorPayload(
                      item,
                    )}</pre>
                </details>
              {/if}
            </div>
            <div class="text-micro text-fg-muted">
              <SignalBadge tone={signal.tone}>{signal.label}</SignalBadge>
              <span class="ml-1.5"
                >{formatTimestamp(item.freshness?.last_observed_at) ||
                  "never"}</span
              >
            </div>
            <p class="text-micro text-fg-muted">
              Refresh {item.refresh?.state || "unknown"} · next {formatTimestamp(
                item.refresh?.next_due_at,
              ) || "not scheduled"}
            </p>
          </li>{/each}
      </ul>
    </section>
  {/each}
  {#if nextCursor}<button
      class="ui-btn-secondary mx-auto"
      disabled={loading}
      onclick={() => load(true)}>Load more</button
    >{/if}
  <details class="text-micro text-fg-muted">
    <summary class="cursor-pointer">Reported capabilities</summary>
    {#if capabilityError}<StateError
        message={capabilityError}
        onretry={() => load()}
        class="mt-2"
      />{:else if capabilities}<pre
        class="mt-2 max-h-80 overflow-auto whitespace-pre-wrap break-words rounded bg-bg-soft p-3 font-mono">{JSON.stringify(
          capabilities,
          null,
          2,
        )}</pre>{:else}<p class="mt-2">
        Capabilities have not been reported.
      </p>{/if}
  </details>
</WorkspacePageShell>
