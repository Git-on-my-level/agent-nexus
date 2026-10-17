<script>
  import { page } from "$app/stores";
  import { onMount } from "svelte";
  import { authenticatedAgent } from "$lib/authSession";
  import {
    bridgeBadge,
    hostBridgeStatus,
    observeHostBridge,
  } from "$lib/hostBridgeStatus.js";
  import Time from "$lib/time/Time.svelte";
  let { host, detail = false } = $props();
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
    const timer = setInterval(() => (now = Date.now()), 1000);
    return () => {
      clearInterval(timer);
      observer?.disconnect();
    };
  });
</script>

<div
  class="text-micro"
  bind:this={element}
  data-host-bridge={host.id}
  data-bridge-state={badge.state}
>
  <span class={badge.state === "live" ? "text-ok-text" : "text-fg-muted"}
    >Bridge: {badge.label}</span
  >
  {#if observation?.bridge && badge.state !== "live" && badge.state !== "revoked"}
    <span class="text-fg-subtle">
      · last seen <Time value={observation.bridge.last_seen_at} {now} /></span
    >
  {/if}
  {#if detail && observation?.agents?.length}
    <details class="mt-1 text-fg-muted">
      <summary>Agent jobs</summary>
      <ul>
        {#each observation.agents as agent (agent.actor_id)}<li>
            {agent.actor_id}: {agent.jobs
              .map(
                (j) =>
                  `${j.kind} ${!j.enabled ? "off" : j.ready ? "ready" : "unavailable"}`,
              )
              .join(", ")}
          </li>{/each}
      </ul>
      {#if observation.agents_truncated}<p>Job list is partial.</p>{/if}
    </details>
  {/if}
</div>
