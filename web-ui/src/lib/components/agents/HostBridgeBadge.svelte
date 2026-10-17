<script>
  import { page } from "$app/stores";
  import { onMount } from "svelte";
  import { authenticatedAgent } from "$lib/authSession";
  import { actorRegistry, lookupActorDisplayName } from "$lib/actorSession.js";
  import CopyButton from "$lib/components/CopyButton.svelte";
  import {
    BRIDGE_JOB_LABELS,
    BRIDGE_STATE_HELP,
    bridgeBadge,
    hostBridgeStatus,
    observeHostBridge,
  } from "$lib/hostBridgeStatus.js";
  import Time from "$lib/time/Time.svelte";
  /**
   * `pill` shows only the state; `details` only the setup hint and agent
   * jobs (for a card body under a pill in its header); `full` both. `detail`
   * keeps the original boolean API: true means `full`.
   */
  let { host, detail = false, variant = "" } = $props();
  let mode = $derived(variant || (detail ? "full" : "pill"));
  let showPill = $derived(mode !== "details");
  let showDetails = $derived(mode !== "pill");
  // Live expires on a five-minute window; a 15 s clock ages it in time
  // without re-rendering every badge every second.
  let now = $state(Date.now());
  let element = $state();
  let visible = $state(false);
  let workspace = $derived(
    `${$page.params.organization}/${$page.params.workspace}/${$authenticatedAgent?.agent_id ?? ""}`,
  );
  let observation = $derived(
    $hostBridgeStatus.workspace === workspace
      ? $hostBridgeStatus.hosts[host.id]
      : null,
  );
  let badge = $derived(
    bridgeBadge(
      host,
      observation,
      now,
      $hostBridgeStatus.workspace !== workspace ||
        !$hostBridgeStatus.checked.includes(host.id),
    ),
  );
  let tone = $derived(
    {
      live: "bg-ok-soft text-ok-text",
      stale: "bg-warn-soft text-warn-text",
      revoked: "bg-danger-soft text-danger-text",
      none: "border border-line text-fg-muted",
    }[badge.state] ?? "text-fg-subtle",
  );
  let dot = $derived(
    {
      live: "bg-ok",
      stale: "bg-warn",
      revoked: "bg-danger",
    }[badge.state] ?? "bg-fg-subtle",
  );
  let needsSetup = $derived(badge.state === "none" || badge.state === "stale");
  $effect(() => (visible ? observeHostBridge(workspace, host.id) : undefined));
  onMount(() => {
    let observer;
    if (typeof IntersectionObserver !== "undefined") {
      observer = new IntersectionObserver(
        ([entry]) => {
          visible = entry.isIntersecting;
        },
        { rootMargin: "200px" },
      );
      observer.observe(element);
    } else {
      visible = true;
    }
    const timer = setInterval(() => (now = Date.now()), 15_000);
    return () => {
      clearInterval(timer);
      observer?.disconnect();
    };
  });
  function jobState(job) {
    if (!job.enabled) return { label: "off", class: "text-fg-subtle" };
    return job.ready
      ? { label: "ready", class: "text-ok-text" }
      : { label: "unavailable", class: "text-warn-text" };
  }
</script>

<div
  class="text-micro"
  bind:this={element}
  data-host-bridge={showPill ? host.id : undefined}
  data-host-bridge-details={showPill ? undefined : host.id}
  data-bridge-state={badge.state}
>
  {#if showPill}
    <span
      class="inline-flex items-center gap-1.5 whitespace-nowrap rounded-full px-2 py-0.5 font-medium {tone}"
      title={BRIDGE_STATE_HELP[badge.state]}
    >
      <span
        class="h-1.5 w-1.5 rounded-full {dot} {badge.state === 'loading'
          ? 'animate-pulse'
          : ''}"
        aria-hidden="true"
      ></span>
      {badge.label}
    </span>
    {#if observation?.bridge && badge.state === "stale"}
      <span class="ml-1 text-fg-subtle">
        last seen <Time value={observation.bridge.last_seen_at} {now} /></span
      >
    {/if}
  {/if}
  {#if showDetails && needsSetup}
    <div
      class="{showPill
        ? 'mt-1.5'
        : ''} flex flex-wrap items-center gap-x-2 gap-y-1"
    >
      <span class="text-fg-muted"
        >{badge.state === "none"
          ? "Answers aren't delivered automatically and commits aren't captured."
          : "Restart it on that machine, or reinstall:"}</span
      >
      <code class="rounded bg-panel-hover px-1.5 py-0.5 text-fg"
        >anx bridge install</code
      >
      <CopyButton value="anx bridge install" iconOnly title="Copy command" />
    </div>
  {/if}
  {#if showDetails && observation?.agents?.length}
    <details class="mt-1.5 text-fg-muted">
      <summary class="cursor-pointer">
        {observation.agents.length === 1
          ? "1 agent on this bridge"
          : `${observation.agents.length} agents on this bridge`}
      </summary>
      <ul class="mt-1 space-y-0.5">
        {#each observation.agents as agent (agent.actor_id)}<li>
            <span class="text-fg"
              >{lookupActorDisplayName(agent.actor_id, $actorRegistry)}</span
            >
            {#each agent.jobs as job, i (job.kind)}{i
                ? " · "
                : " — "}{BRIDGE_JOB_LABELS[job.kind] ?? job.kind}
              <span class={jobState(job).class}>{jobState(job).label}</span
              >{/each}
          </li>{/each}
      </ul>
      {#if observation.agents_truncated}<p>Showing some agents only.</p>{/if}
    </details>
  {/if}
</div>
