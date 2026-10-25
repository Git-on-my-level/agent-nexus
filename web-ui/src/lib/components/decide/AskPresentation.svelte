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
  {#if decision.situation}<p>{decision.situation}</p>{/if}
  {#if decision.impact}<p>
      <strong>Impact:</strong>
      {decision.impact}
    </p>{/if}
  {#if decision.recommendation}<p>
      <strong>I recommend:</strong>
      {decision.recommendation.action}
    </p>
    <p>{decision.recommendation.why}</p>{/if}
  {#each decision.alternatives ?? [] as alternative}<p>
      <strong>Alternative:</strong>
      {alternative.action}
      {alternative.consequence}
    </p>{/each}
  {#if decision.if_no_answer}<p>
      <strong>If no answer:</strong>
      {decision.if_no_answer}
    </p>{/if}
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
