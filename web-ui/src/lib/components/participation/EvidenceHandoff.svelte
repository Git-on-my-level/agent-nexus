<script>
  import ActorLabel from "$lib/components/ActorLabel.svelte";
  import InfoTip from "$lib/components/InfoTip.svelte";
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
  let {
    observations = [],
    unavailable = false,
    /**
     * One more sentence for the "What evidence means" tip, when the page
     * knows something this component does not — "created here, so there is
     * nothing to read back from". A second glyph beside the first would be
     * two tips a thumb apart saying halves of one answer.
     */
    note = "",
  } = $props();
  let latest = $derived(observations[0]);
  let report = $derived(observations.find((entry) => entry.status !== "error"));
  let uncertaintyCount = $derived(
    Array.isArray(report?.uncertainty) ? report.uncertainty.length : 0,
  );
  let quietTip = $derived(
    [
      "Nobody has shared a report on this task yet. A run finishing is not evidence that the work is done.",
      String(note ?? "").trim(),
    ]
      .filter(Boolean)
      .join(" "),
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
    <!-- One line for an absence, with the caveat on the line itself. -->
    <p class="flex items-center gap-1.5" data-evidence-quiet>
      No activity<InfoTip label="What evidence means" text={quietTip} />
    </p>
  {/if}
</div>
