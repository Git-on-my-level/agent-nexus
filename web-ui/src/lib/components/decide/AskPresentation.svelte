<script>
  import { safeSourceHref } from "$lib/pm/presentation.js";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  let { decision, organizationSlug, workspaceSlug } = $props();
  const hasProposalCopy = $derived(
    decision.situation ||
      decision.impact ||
      decision.recommendation ||
      decision.alternatives?.length ||
      decision.if_no_answer ||
      decision.details,
  );
</script>

<div class="text-meta leading-relaxed text-fg" data-decision-presentation>
  {#if !hasProposalCopy && decision.body}
    <MarkdownRenderer
      source={decision.body}
      {organizationSlug}
      {workspaceSlug}
    />
  {/if}
  {#if decision.situation}
    <h4 class="font-semibold">What's happening</h4>
    {#each decision.situation.split(/\n\s*\n/) as paragraph}
      <p>{paragraph}</p>
    {/each}
  {/if}
  {#if decision.impact}
    <h4 class="font-semibold">Why it matters</h4>
    {#each decision.impact.split(/\n\s*\n/) as paragraph}
      <p>{paragraph}</p>
    {/each}
  {/if}
  {#if decision.recommendation}
    <h4 class="font-semibold">What I recommend</h4>
    <p>{decision.recommendation.action}</p>
    <p>{decision.recommendation.why}</p>{/if}
  {#if decision.alternatives?.length}
    <h4 class="font-semibold">Alternatives</h4>
  {/if}
  {#each decision.alternatives ?? [] as alternative}<p>
      {alternative.action}
      {alternative.consequence}
    </p>{/each}
  {#if decision.if_no_answer}
    <h4 class="font-semibold">If no answer</h4>
    <p>{decision.if_no_answer}</p>
  {/if}
  {#if decision.details || (hasProposalCopy && decision.body) || decision.evidence?.length}
    <details>
      <summary>Details and evidence</summary>
      <MarkdownRenderer
        source={[decision.details, hasProposalCopy && decision.body]
          .filter(Boolean)
          .join("\n\n")}
        {organizationSlug}
        {workspaceSlug}
      />
      {#each decision.evidence ?? [] as evidence}<p>
          {#if safeSourceHref(evidence.url)}<a
              href={safeSourceHref(evidence.url)}
              rel="noreferrer">{evidence.label || evidence.url || "Evidence"}</a
            >{:else}{evidence.label || evidence.url || "Evidence"}{/if}
        </p>{/each}
    </details>
  {/if}
</div>

<style>
  [data-decision-presentation] > h4:not(:first-child),
  [data-decision-presentation] > p + p,
  [data-decision-presentation] > details {
    margin-top: 0.75rem;
  }
</style>
