<script>
  import { page } from "$app/stores";

  import { agentRegistry } from "$lib/actorSession";
  import {
    loadAttributedRun,
    normalizeRunAttribution,
    runAttributionLabel,
    runAttributionPath,
  } from "$lib/runAttribution.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";

  /**
   * "via run exec-…" beside the author of an event written inside a
   * launcher run. Renders nothing for events without run attribution.
   */
  let { attribution = null } = $props();

  let normalized = $derived(normalizeRunAttribution(attribution));
  let run = $state(null);
  $effect(() => {
    const id = normalized?.runId;
    run = null;
    if (!id) return;
    let live = true;
    void loadAttributedRun(id).then((value) => {
      if (live) run = value;
    });
    return () => {
      live = false;
    };
  });

  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  let path = $derived(
    normalized ? runAttributionPath(normalized, $agentRegistry) : "",
  );
  let label = $derived(normalized ? runAttributionLabel(normalized, run) : "");
</script>

{#if normalized}
  {#if path}
    <a
      class="shrink-0 text-micro text-fg-subtle hover:text-fg-muted hover:underline"
      href={workspaceHref(path)}
      title="Written inside this run. Open it on the agent's page."
      data-run-attribution={normalized.runId}>via run {label}</a
    >
  {:else}
    <span
      class="shrink-0 text-micro text-fg-subtle"
      data-run-attribution={normalized.runId}>via run {label}</span
    >
  {/if}
{/if}
