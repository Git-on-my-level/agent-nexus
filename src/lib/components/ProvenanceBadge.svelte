<script>
  import {
    getProvenancePresentation,
    getProvenanceSources,
  } from "$lib/provenanceUtils";

  export let provenance = { sources: [] };

  $: sources = getProvenanceSources(provenance);
  $: presentation = getProvenancePresentation(provenance);
</script>

<div class={`rounded-md border px-3 py-2 text-xs ${presentation.toneClass}`}>
  <p class="font-semibold">
    {presentation.title}
  </p>
  <p class="mt-1">sources: {sources.join(", ") || "none"}</p>

  {#if provenance?.notes}
    <p class="mt-1">notes: {provenance.notes}</p>
  {/if}

  {#if provenance?.by_field}
    <details class="mt-1">
      <summary class="cursor-pointer">by_field</summary>
      <pre
        class="mt-1 overflow-auto rounded bg-white/70 p-2 text-[11px]">{JSON.stringify(
          provenance.by_field,
          null,
          2,
        )}</pre>
    </details>
  {/if}
</div>
