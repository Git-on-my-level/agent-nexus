<script>
  import ActorLabel from "$lib/components/ActorLabel.svelte";
  import FinePrint from "$lib/components/FinePrint.svelte";
  import SignalBadge from "$lib/components/pm/SignalBadge.svelte";
  import {
    actorDisplayLabel,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import Time from "$lib/time/Time.svelte";
  import {
    observationStatusLabel,
    observationStatusTone,
    distinctEvidenceLinks,
  } from "$lib/pm/evidence.js";
  let { observations = [], unavailable = false } = $props();
  let latest = $derived(observations[0]);
  let report = $derived(observations.find((entry) => entry.status !== "error"));
  let uncertaintyCount = $derived(
    Array.isArray(report?.uncertainty) ? report.uncertainty.length : 0,
  );
  let links = $derived(
    report ? distinctEvidenceLinks([report]).filter((entry) => entry.href) : [],
  );
</script>

<div
  class="mb-3 space-y-1 text-micro text-fg-muted"
  aria-label="Evidence for handoff"
>
  {#if unavailable}
    <p>
      Handoff evidence is unavailable. Current activity cannot confirm progress
      or completion.
    </p>
  {:else if report}
    <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
      <span>Latest shared report</span>
      <SignalBadge tone={observationStatusTone(report)}
        >{observationStatusLabel(report)}</SignalBadge
      >
      {#if report.actor_id}
        <ActorLabel
          label={actorDisplayLabel(
            report.actor_id,
            $actorRegistry,
            $principalRegistry,
          )}
          seed={report.actor_id}
          size="xs"
          prefix="by"
        />
      {:else}<span>Attribution unavailable</span>{/if}
      {#if report.observed_at}<Time value={report.observed_at} />{/if}
    </div>
    <p>
      {links.length} linked evidence {links.length === 1 ? "item" : "items"} in this
      report{uncertaintyCount
        ? ` · ${uncertaintyCount} uncertainties reported`
        : ""}. Review the evidence and acceptance criteria before handoff.
    </p>
    {#if latest?.status === "error"}<p class="text-warn-text">
        The latest read failed. This is an earlier report.
      </p>{/if}
  {:else}
    <!-- One line for an absence, and the caveat behind a toggle. -->
    <p data-evidence-quiet>No activity</p>
    <FinePrint label="What evidence means">
      No shared evidence report for handoff yet. Participation and completed
      runs do not establish task completion.
    </FinePrint>
  {/if}
</div>
