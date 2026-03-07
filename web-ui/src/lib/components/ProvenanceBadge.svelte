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
        ? "bg-amber-400"
        : "bg-emerald-400",
  );
</script>

{#if hasDetails}
  <details class="group inline-block">
    <summary
      class="inline-flex cursor-pointer list-none items-center gap-1.5 text-[11px] text-gray-400 select-none hover:text-gray-600"
    >
      <span class={`h-1.5 w-1.5 rounded-full ${dotClass}`}></span>
      {label}
    </summary>
    <div
      class="mt-1 rounded border border-gray-200 bg-gray-50 px-3 py-2 text-xs text-gray-600"
    >
      {#if sources.length > 0}
        <p>Based on: {sources.join(", ")}</p>
      {/if}
      {#if provenance?.notes}
        <p class="mt-1">{provenance.notes}</p>
      {/if}
      {#if provenance?.by_field}
        <details class="mt-1">
          <summary class="cursor-pointer text-[11px] text-gray-400"
            >Field details</summary
          >
          <pre
            class="mt-1 overflow-auto rounded bg-gray-100 p-2 text-[11px]">{JSON.stringify(
              provenance.by_field,
              null,
              2,
            )}</pre>
        </details>
      {/if}
    </div>
  </details>
{:else}
  <span class="inline-flex items-center gap-1.5 text-[11px] text-gray-400">
    <span class={`h-1.5 w-1.5 rounded-full ${dotClass}`}></span>
    {label}
  </span>
{/if}
