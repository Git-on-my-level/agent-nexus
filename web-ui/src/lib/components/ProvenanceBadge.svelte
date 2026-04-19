<script>
  import {
    getProvenancePresentation,
    getProvenanceSources,
  } from "$lib/provenanceUtils";

  let { provenance = undefined } = $props();

  let sources = $derived(getProvenanceSources(provenance));
  let presentation = $derived(getProvenancePresentation(provenance));
  let hasDetails = $derived(
    sources.length > 0 || provenance?.notes || provenance?.by_field,
  );
  let label = $derived(
    presentation.unknown
      ? "No provenance"
      : presentation.inferred
        ? "Inferred"
        : "Evidence-backed",
  );
  let dotClass = $derived(
    presentation.unknown
      ? "bg-slate-400"
      : presentation.inferred
        ? "bg-warn-text"
        : "bg-ok-text",
  );
</script>

{#if hasDetails}
  <details class="group inline-block">
    <summary
      class="inline-flex cursor-pointer list-none items-center gap-1.5 text-micro text-[var(--fg-muted)] select-none hover:text-[var(--fg)]"
    >
      <span class={`h-1.5 w-1.5 rounded-full ${dotClass}`}></span>
      {label}
    </summary>
    <div
      class="mt-1 rounded border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-micro text-[var(--fg-muted)]"
    >
      {#if sources.length > 0}
        <p>Based on: {sources.join(", ")}</p>
      {/if}
      {#if provenance?.notes}
        <p class="mt-1">{provenance.notes}</p>
      {/if}
      {#if provenance?.by_field}
        <details class="mt-1">
          <summary class="cursor-pointer text-micro text-[var(--fg-muted)]"
            >Field details</summary
          >
          <pre
            class="mt-1 overflow-auto rounded bg-[var(--bg-soft)] p-2 text-micro">{JSON.stringify(
              provenance.by_field,
              null,
              2,
            )}</pre>
        </details>
      {/if}
    </div>
  </details>
{:else}
  <span
    class="inline-flex items-center gap-1.5 text-micro text-[var(--fg-muted)]"
  >
    <span class={`h-1.5 w-1.5 rounded-full ${dotClass}`}></span>
    {label}
  </span>
{/if}
