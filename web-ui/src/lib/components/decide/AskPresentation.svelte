<script>
  import { safeSourceHref } from "$lib/pm/presentation.js";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  let {
    decision,
    organizationSlug,
    workspaceSlug,
    allowRevisionToggle = false,
  } = $props();
  let showOriginal = $state(false);
  const shown = $derived(
    showOriginal && decision.presentationRewrite
      ? {
          ...decision,
          ...decision.presentationRewrite.previous,
          body: decision.body,
          evidence: decision.evidence,
        }
      : decision,
  );
  const hasProposalCopy = $derived(
    shown.situation ||
      shown.impact ||
      shown.recommendation ||
      shown.alternatives?.length ||
      shown.if_no_answer ||
      shown.details,
  );
</script>

<div class="text-meta leading-relaxed text-fg" data-decision-presentation>
  {#if allowRevisionToggle && decision.presentationRewrite}
    <p class="text-fg-muted" data-presentation-rewrite>
      Rewritten for clarity by {decision.presentationRewrite.actorDisplay ||
        decision.presentationRewrite.actorId} ·
      <button
        type="button"
        class="underline"
        aria-expanded={showOriginal}
        onclick={() => (showOriginal = !showOriginal)}
        >{showOriginal ? "Show rewrite" : "Show original"}</button
      >
    </p>
    {#if showOriginal}<p><strong>{shown.title}</strong></p>{/if}
  {/if}

  {#if !hasProposalCopy && shown.body}
    <MarkdownRenderer source={shown.body} {organizationSlug} {workspaceSlug} />
  {/if}
  {#if shown.situation}
    <h4 class="font-semibold">What's happening</h4>
    {#each shown.situation.split(/\n\s*\n/) as paragraph}
      <p>{paragraph}</p>
    {/each}
  {/if}
  {#if shown.impact}
    <h4 class="font-semibold">Why it matters</h4>
    {#each shown.impact.split(/\n\s*\n/) as paragraph}
      <p>{paragraph}</p>
    {/each}
  {/if}
  {#if shown.recommendation}
    <h4 class="font-semibold">What I recommend</h4>
    <p>{shown.recommendation.action}</p>
    <p>{shown.recommendation.why}</p>{/if}
  {#if shown.alternatives?.length}
    <h4 class="font-semibold">Alternatives</h4>
  {/if}
  {#each shown.alternatives ?? [] as alternative}<p>
      {alternative.action}
      {alternative.consequence}
    </p>{/each}
  {#if shown.if_no_answer}
    <h4 class="font-semibold">If no answer</h4>
    <p>{shown.if_no_answer}</p>
  {/if}
  {#if shown.details || (hasProposalCopy && shown.body) || shown.evidence?.length}
    <details>
      <summary>Details and evidence</summary>
      <MarkdownRenderer
        source={[shown.details, hasProposalCopy && shown.body]
          .filter(Boolean)
          .join("\n\n")}
        {organizationSlug}
        {workspaceSlug}
      />
      {#each shown.evidence ?? [] as evidence}<p>
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
