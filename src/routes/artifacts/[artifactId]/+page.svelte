<script>
  import { page } from "$app/stores";

  import { coreClient } from "$lib/coreClient";
  import ProvenanceBadge from "$lib/components/ProvenanceBadge.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import UnknownObjectPanel from "$lib/components/UnknownObjectPanel.svelte";

  $: artifactId = $page.params.artifactId;
  let artifact = null;
  let artifactContent = null;
  let loading = false;
  let loadError = "";
  let loadedArtifactId = "";

  $: if (artifactId && artifactId !== loadedArtifactId) {
    loadArtifact(artifactId);
  }

  $: receiptPacket =
    artifact?.kind === "receipt" &&
    artifactContent &&
    typeof artifactContent === "object" &&
    !Array.isArray(artifactContent)
      ? artifactContent
      : null;

  async function loadArtifact(targetArtifactId) {
    if (!targetArtifactId) {
      return;
    }

    loading = true;
    loadError = "";
    loadedArtifactId = targetArtifactId;

    try {
      const metaResponse = await coreClient.getArtifact(targetArtifactId);
      artifact = metaResponse.artifact ?? null;

      if (!artifact) {
        loadError = "Artifact not found.";
        artifactContent = null;
        return;
      }

      const contentResponse =
        await coreClient.getArtifactContent(targetArtifactId);
      artifactContent = contentResponse.content ?? null;
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      loadError = `Failed to load artifact: ${reason}`;
      artifact = null;
      artifactContent = null;
    } finally {
      loading = false;
    }
  }
</script>

<h1 class="text-2xl font-semibold">Artifact Detail: {artifactId}</h1>

{#if loading}
  <p class="mt-4 rounded-md bg-white p-3 text-sm text-slate-700 shadow-sm">
    Loading artifact...
  </p>
{:else if loadError}
  <p
    class="mt-4 rounded-md border border-rose-200 bg-rose-50 p-3 text-sm text-rose-800"
  >
    {loadError}
  </p>
{:else if artifact}
  <p class="mt-2 max-w-2xl text-slate-700">{artifact.summary || artifact.id}</p>

  <section
    class="mt-6 rounded-lg border border-slate-200 bg-white p-4 shadow-sm"
  >
    <p class="text-xs uppercase tracking-wide text-slate-500">
      kind: {artifact.kind}
    </p>

    <div class="mt-3 flex flex-wrap gap-2 text-xs">
      {#each artifact.refs ?? [] as refValue}
        <span class="rounded bg-slate-100 px-2 py-1">
          <RefLink {refValue} threadId={artifact.thread_id} />
        </span>
      {/each}
    </div>

    <div class="mt-3">
      <ProvenanceBadge provenance={artifact.provenance ?? { sources: [] }} />
    </div>

    {#if receiptPacket}
      <div class="mt-4 rounded-md border border-slate-200 bg-slate-50 p-3">
        <h2
          class="text-sm font-semibold uppercase tracking-wide text-slate-600"
        >
          Receipt Packet
        </h2>
        <p class="mt-2 text-xs text-slate-600">
          receipt_id: {receiptPacket.receipt_id}
        </p>
        <p class="mt-1 text-xs text-slate-600">
          work_order_id:
          <RefLink refValue={`artifact:${receiptPacket.work_order_id}`} />
        </p>
        <p class="mt-1 text-xs text-slate-600">
          thread_id: <RefLink refValue={`thread:${receiptPacket.thread_id}`} />
        </p>

        <div class="mt-3">
          <p
            class="text-xs font-semibold uppercase tracking-wide text-slate-500"
          >
            Outputs
          </p>
          <ul class="mt-1 list-disc space-y-1 pl-5 text-sm text-slate-700">
            {#each receiptPacket.outputs ?? [] as refValue}
              <li><RefLink {refValue} threadId={receiptPacket.thread_id} /></li>
            {/each}
          </ul>
        </div>

        <div class="mt-3">
          <p
            class="text-xs font-semibold uppercase tracking-wide text-slate-500"
          >
            Verification Evidence
          </p>
          <ul class="mt-1 list-disc space-y-1 pl-5 text-sm text-slate-700">
            {#each receiptPacket.verification_evidence ?? [] as refValue}
              <li><RefLink {refValue} threadId={receiptPacket.thread_id} /></li>
            {/each}
          </ul>
        </div>

        <div class="mt-3">
          <p
            class="text-xs font-semibold uppercase tracking-wide text-slate-500"
          >
            Changes Summary
          </p>
          <p class="mt-1 text-sm text-slate-700">
            {receiptPacket.changes_summary || "none"}
          </p>
        </div>

        <div class="mt-3">
          <p
            class="text-xs font-semibold uppercase tracking-wide text-slate-500"
          >
            Known Gaps
          </p>
          <ul class="mt-1 list-disc space-y-1 pl-5 text-sm text-slate-700">
            {#if (receiptPacket.known_gaps ?? []).length === 0}
              <li>none</li>
            {:else}
              {#each receiptPacket.known_gaps ?? [] as gap}
                <li>{gap}</li>
              {/each}
            {/if}
          </ul>
        </div>
      </div>
    {/if}

    <div class="mt-3">
      <UnknownObjectPanel
        objectData={artifact}
        title="Raw Artifact Metadata JSON"
        open={true}
      />
    </div>

    <div class="mt-3">
      <UnknownObjectPanel
        objectData={artifactContent}
        title="Raw Artifact Content JSON"
      />
    </div>
  </section>
{/if}
