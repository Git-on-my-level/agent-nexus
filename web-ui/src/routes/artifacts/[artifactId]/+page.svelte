<script>
  import { page } from "$app/stores";

  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import ProvenanceBadge from "$lib/components/ProvenanceBadge.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import { buildReviewPayload } from "$lib/reviewUtils";
  import { toTimelineView } from "$lib/timelineUtils";
  import { lookupActorDisplayName, actorRegistry } from "$lib/actorSession";

  const KNOWN_PACKET_ARTIFACT_KINDS = new Set([
    "work_order",
    "receipt",
    "review",
  ]);

  let artifactId = $derived($page.params.artifactId);
  let actorName = $derived((id) => lookupActorDisplayName(id, $actorRegistry));
  let artifact = $state(null);
  let artifactContent = $state(null);
  let artifactContentType = $state("");
  let loading = $state(false);
  let loadError = $state("");
  let loadedArtifactId = $state("");
  let reviewDraft = $state(null);
  let submittingReview = $state(false);
  let reviewErrors = $state([]);
  let reviewNotice = $state("");
  let createdReview = $state(null);
  let reviseFollowupLink = $state("");
  let threadTimeline = $state([]);
  let timelineLoading = $state(false);
  let timelineError = $state("");

  $effect(() => {
    const id = artifactId;
    if (id && id !== loadedArtifactId) loadArtifact(id);
  });
  let receiptPacket = $derived(
    artifact?.kind === "receipt" &&
      artifactContentType.includes("application/json") &&
      artifactContent &&
      typeof artifactContent === "object" &&
      !Array.isArray(artifactContent)
      ? artifactContent
      : null,
  );
  let workOrderPacket = $derived(
    artifact?.kind === "work_order" &&
      artifactContentType.includes("application/json") &&
      artifactContent &&
      typeof artifactContent === "object" &&
      !Array.isArray(artifactContent)
      ? artifactContent
      : null,
  );
  let reviewPacket = $derived(
    artifact?.kind === "review" &&
      artifactContentType.includes("application/json") &&
      artifactContent &&
      typeof artifactContent === "object" &&
      !Array.isArray(artifactContent)
      ? artifactContent
      : null,
  );
  let textContent = $derived(
    artifactContentType.startsWith("text/") &&
      typeof artifactContent === "string"
      ? artifactContent
      : "",
  );
  let isKnownPacketArtifactKind = $derived(
    KNOWN_PACKET_ARTIFACT_KINDS.has(String(artifact?.kind ?? "")),
  );
  let timelineView = $derived(
    toTimelineView(threadTimeline, { threadId: artifact?.thread_id ?? "" }),
  );

  function blankReviewDraft() {
    return { outcome: "accept", notes: "", evidenceRefsInput: "" };
  }
  function generateReviewId() {
    return `artifact-review-${Math.random().toString(36).slice(2, 10)}`;
  }

  async function loadThreadTimeline(threadId) {
    if (!threadId) {
      threadTimeline = [];
      return;
    }
    timelineLoading = true;
    timelineError = "";
    try {
      threadTimeline =
        (await coreClient.listThreadTimeline(threadId)).events ?? [];
    } catch (e) {
      timelineError = `Failed to load timeline: ${e instanceof Error ? e.message : String(e)}`;
      threadTimeline = [];
    } finally {
      timelineLoading = false;
    }
  }

  async function submitReview(event) {
    if (event?.preventDefault) {
      event.preventDefault();
    }
    if (!artifact || !receiptPacket || !reviewDraft) return;
    reviewErrors = [];
    reviewNotice = "";
    reviseFollowupLink = "";
    submittingReview = true;
    const reviewId = generateReviewId();
    const payload = buildReviewPayload(reviewDraft, {
      threadId: artifact.thread_id,
      receiptId: artifact.id,
      workOrderId: receiptPacket.work_order_id,
      reviewId,
    });
    if (!payload.valid) {
      reviewErrors = payload.errors;
      submittingReview = false;
      return;
    }
    try {
      const response = await coreClient.createReview({
        artifact: payload.artifact,
        packet: payload.packet,
      });
      createdReview = response.artifact ?? null;
      reviewNotice = "Review submitted.";
      reviewDraft = blankReviewDraft();
      if (payload.packet.outcome === "revise") {
        const params = new URLSearchParams();
        params.set("compose", "work-order");
        params.append("context_ref", `artifact:${artifact.id}`);
        params.append("context_ref", `artifact:${receiptPacket.work_order_id}`);
        reviseFollowupLink = `/threads/${encodeURIComponent(artifact.thread_id)}?${params.toString()}#work-order-composer`;
      }
      await loadThreadTimeline(artifact.thread_id);
    } catch (e) {
      reviewErrors = [
        `Failed to submit review: ${e instanceof Error ? e.message : String(e)}`,
      ];
    } finally {
      submittingReview = false;
    }
  }

  async function loadArtifact(targetId) {
    if (!targetId) return;
    loading = true;
    loadError = "";
    loadedArtifactId = targetId;
    try {
      artifact = (await coreClient.getArtifact(targetId)).artifact ?? null;
      if (!artifact) {
        loadError = "Artifact not found.";
        artifactContent = null;
        artifactContentType = "";
        return;
      }
      const contentResponse = await coreClient.getArtifactContent(targetId);
      artifactContent = contentResponse.content ?? null;
      artifactContentType = contentResponse.contentType ?? "";
      reviewDraft = blankReviewDraft();
      reviewErrors = [];
      reviewNotice = "";
      createdReview = null;
      reviseFollowupLink = "";
      if (artifact?.kind === "receipt" && artifact?.thread_id)
        await loadThreadTimeline(artifact.thread_id);
      else {
        threadTimeline = [];
        timelineError = "";
      }
    } catch (e) {
      loadError = `Failed to load artifact: ${e instanceof Error ? e.message : String(e)}`;
      artifact = null;
      artifactContent = null;
      artifactContentType = "";
      threadTimeline = [];
      timelineError = "";
    } finally {
      loading = false;
    }
  }

  function kindLabel(kind) {
    const labels = {
      work_order: "Work Order",
      receipt: "Receipt",
      review: "Review",
      doc: "Document",
    };
    return labels[kind] ?? kind;
  }
</script>

<nav
  class="mb-3 flex items-center gap-1.5 text-sm text-gray-400"
  aria-label="Breadcrumb"
>
  <a class="hover:text-gray-600" href="/artifacts">Artifacts</a>
  <span class="text-gray-300">/</span>
  <span class="truncate text-gray-700">{artifact?.summary || artifactId}</span>
</nav>

{#if loading}
  <p class="text-sm text-gray-400">Loading...</p>
{:else if loadError}
  <p class="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">{loadError}</p>
{:else if artifact}
  <h1 class="text-lg font-semibold text-gray-900">
    {artifact.summary || artifact.id}
  </h1>

  <div class="mt-3 flex flex-wrap items-center gap-2 text-xs">
    <span class="rounded bg-gray-100 px-2 py-0.5 font-medium text-gray-600"
      >{kindLabel(artifact.kind)}</span
    >
    <span class="text-gray-400"
      >{formatTimestamp(artifact.created_at) || "—"}</span
    >
    <span class="text-gray-400">by {actorName(artifact.created_by)}</span>
    {#if artifact.thread_id}
      <RefLink
        refValue={`thread:${artifact.thread_id}`}
        threadId={artifact.thread_id}
      />
    {/if}
  </div>

  {#if (artifact.refs ?? []).length > 0}
    <div class="mt-2 flex flex-wrap gap-1.5 text-xs">
      {#each artifact.refs ?? [] as refValue}
        <RefLink {refValue} threadId={artifact.thread_id} />
      {/each}
    </div>
  {/if}

  <div class="mt-2">
    <ProvenanceBadge provenance={artifact.provenance} />
  </div>

  {#if !isKnownPacketArtifactKind && artifact.kind !== "doc"}
    <p class="mt-3 rounded bg-amber-50 px-3 py-2 text-xs text-amber-700">
      Unknown artifact kind: {artifact.kind}
    </p>
  {/if}

  {#if workOrderPacket}
    <div class="mt-4 rounded-lg border border-gray-200 bg-white">
      <div class="border-b border-gray-100 px-4 py-2.5">
        <h2
          class="text-xs font-semibold uppercase tracking-wider text-gray-400"
        >
          Work Order
        </h2>
      </div>
      <div class="px-4 py-3 text-sm text-gray-800">
        <p class="font-medium">{workOrderPacket.objective || "No objective"}</p>
        {#if (workOrderPacket.constraints ?? []).length > 0}
          <div class="mt-3">
            <p class="text-xs text-gray-400">Constraints</p>
            <ul class="mt-1 list-inside list-disc text-sm">
              {#each workOrderPacket.constraints as c}<li>{c}</li>{/each}
            </ul>
          </div>
        {/if}
        {#if (workOrderPacket.context_refs ?? []).length > 0}
          <div class="mt-3">
            <p class="text-xs text-gray-400">Context</p>
            <div class="mt-1 flex flex-wrap gap-1.5 text-xs">
              {#each workOrderPacket.context_refs as r}<RefLink
                  refValue={r}
                  threadId={workOrderPacket.thread_id}
                />{/each}
            </div>
          </div>
        {/if}
        {#if (workOrderPacket.acceptance_criteria ?? []).length > 0}
          <div class="mt-3">
            <p class="text-xs text-gray-400">Acceptance criteria</p>
            <ul class="mt-1 list-inside list-disc text-sm">
              {#each workOrderPacket.acceptance_criteria as c}<li>
                  {c}
                </li>{/each}
            </ul>
          </div>
        {/if}
        {#if (workOrderPacket.definition_of_done ?? []).length > 0}
          <div class="mt-3">
            <p class="text-xs text-gray-400">Definition of done</p>
            <ul class="mt-1 list-inside list-disc text-sm">
              {#each workOrderPacket.definition_of_done as d}<li>{d}</li>{/each}
            </ul>
          </div>
        {/if}
      </div>
    </div>
  {/if}

  {#if receiptPacket}
    <div class="mt-4 rounded-lg border border-gray-200 bg-white">
      <div class="border-b border-gray-100 px-4 py-2.5">
        <h2
          class="text-xs font-semibold uppercase tracking-wider text-gray-400"
        >
          Receipt
        </h2>
      </div>
      <div class="px-4 py-3 text-sm">
        <div class="flex flex-wrap gap-2 text-xs text-gray-500">
          <span
            >Work order: <RefLink
              refValue={`artifact:${receiptPacket.work_order_id}`}
            /></span
          >
          <span
            >Thread: <RefLink
              refValue={`thread:${receiptPacket.thread_id}`}
            /></span
          >
        </div>
        {#if (receiptPacket.outputs ?? []).length > 0}
          <div class="mt-3">
            <p class="text-xs text-gray-400">Outputs</p>
            <div class="mt-1 flex flex-wrap gap-1.5 text-xs">
              {#each receiptPacket.outputs as r}<RefLink
                  refValue={r}
                  threadId={receiptPacket.thread_id}
                />{/each}
            </div>
          </div>
        {/if}
        {#if (receiptPacket.verification_evidence ?? []).length > 0}
          <div class="mt-3">
            <p class="text-xs text-gray-400">Verification evidence</p>
            <div class="mt-1 flex flex-wrap gap-1.5 text-xs">
              {#each receiptPacket.verification_evidence as r}<RefLink
                  refValue={r}
                  threadId={receiptPacket.thread_id}
                />{/each}
            </div>
          </div>
        {/if}
        <div class="mt-3">
          <p class="text-xs text-gray-400">Changes summary</p>
          <p class="mt-1 text-gray-800">
            {receiptPacket.changes_summary || "—"}
          </p>
        </div>
        {#if (receiptPacket.known_gaps ?? []).length > 0}
          <div class="mt-3">
            <p class="text-xs text-gray-400">Known gaps</p>
            <ul class="mt-1 list-inside list-disc text-gray-600">
              {#each receiptPacket.known_gaps as g}<li>{g}</li>{/each}
            </ul>
          </div>
        {/if}
      </div>

      <!-- Review form -->
      <div class="border-t border-gray-100 px-4 py-3">
        <h3
          class="text-xs font-semibold uppercase tracking-wider text-gray-400"
        >
          Review
        </h3>
        {#if reviewErrors.length > 0}<ul
            class="mt-2 list-inside list-disc rounded bg-red-50 px-3 py-1.5 text-xs text-red-700"
          >
            {#each reviewErrors as e}<li>{e}</li>{/each}
          </ul>{/if}
        {#if reviewNotice}<p
            class="mt-2 rounded bg-emerald-50 px-3 py-1.5 text-xs text-emerald-700"
          >
            {reviewNotice}
          </p>{/if}
        {#if reviseFollowupLink}
          <p
            class="mt-2 rounded bg-amber-50 px-3 py-1.5 text-xs text-amber-700"
          >
            Outcome is revise. <a
              class="font-medium underline"
              href={reviseFollowupLink}>Create follow-up work order</a
            >
          </p>
        {/if}
        {#if reviewDraft}
          <form class="mt-2 grid gap-2" onsubmit={submitReview}>
            <label class="text-xs font-medium text-gray-600"
              >Outcome <select
                bind:value={reviewDraft.outcome}
                class="mt-1 w-full rounded border border-gray-200 px-2 py-1.5 text-sm"
                ><option value="accept">Accept</option><option value="revise"
                  >Revise</option
                ><option value="escalate">Escalate</option></select
              ></label
            >
            <label class="text-xs font-medium text-gray-600"
              >Notes <textarea
                bind:value={reviewDraft.notes}
                class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
                rows="2"
              ></textarea></label
            >
            <label class="text-xs font-medium text-gray-600"
              >Evidence refs (optional, one per line) <textarea
                bind:value={reviewDraft.evidenceRefsInput}
                class="mt-1 w-full rounded border border-gray-200 px-2.5 py-1.5 text-sm"
                rows="2"
              ></textarea></label
            >
            <button
              class="w-fit rounded bg-indigo-600 px-4 py-1.5 text-xs font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
              disabled={submittingReview}
              type="submit"
              >{submittingReview ? "Submitting..." : "Submit review"}</button
            >
          </form>
        {/if}
        {#if createdReview}
          <p class="mt-2 text-xs text-gray-500">
            Review submitted: <a
              class="text-indigo-600 underline"
              href={`/artifacts/${createdReview.id}`}
              >{createdReview.summary || createdReview.id}</a
            >
          </p>
        {/if}
      </div>

      <!-- Thread timeline -->
      {#if threadTimeline.length > 0 || timelineLoading}
        <div class="border-t border-gray-100 px-4 py-3">
          <h3
            class="text-xs font-semibold uppercase tracking-wider text-gray-400"
          >
            Thread Timeline
          </h3>
          {#if timelineLoading}
            <p class="mt-2 text-xs text-gray-400">Loading...</p>
          {:else if timelineError}
            <p class="mt-2 text-xs text-red-600">{timelineError}</p>
          {:else}
            <div class="mt-2 space-y-1">
              {#each timelineView.slice(0, 10) as event}
                <div
                  class="rounded border border-gray-100 bg-gray-50 px-3 py-2 text-xs"
                >
                  <p class="font-medium text-gray-800">{event.summary}</p>
                  <p class="mt-0.5 text-gray-400">
                    {actorName(event.actor_id)} · {event.typeLabel} · {formatTimestamp(
                      event.ts,
                    ) || "—"}
                  </p>
                </div>
              {/each}
            </div>
          {/if}
        </div>
      {/if}
    </div>
  {/if}

  {#if reviewPacket}
    <div class="mt-4 rounded-lg border border-gray-200 bg-white">
      <div class="border-b border-gray-100 px-4 py-2.5">
        <h2
          class="text-xs font-semibold uppercase tracking-wider text-gray-400"
        >
          Review
        </h2>
      </div>
      <div class="px-4 py-3 text-sm">
        <div class="flex items-center gap-3">
          <span
            class="rounded px-2 py-0.5 text-xs font-medium {reviewPacket.outcome ===
            'accept'
              ? 'bg-emerald-50 text-emerald-700'
              : reviewPacket.outcome === 'revise'
                ? 'bg-amber-50 text-amber-700'
                : 'bg-red-50 text-red-700'}">{reviewPacket.outcome}</span
          >
          <span class="text-xs text-gray-500"
            >Receipt: <RefLink
              refValue={`artifact:${reviewPacket.receipt_id}`}
              threadId={artifact.thread_id}
            /></span
          >
          <span class="text-xs text-gray-500"
            >Work order: <RefLink
              refValue={`artifact:${reviewPacket.work_order_id}`}
              threadId={artifact.thread_id}
            /></span
          >
        </div>
        {#if reviewPacket.notes}<p class="mt-3 text-gray-700">
            {reviewPacket.notes}
          </p>{/if}
        {#if (reviewPacket.evidence_refs ?? []).length > 0}
          <div class="mt-3">
            <p class="text-xs text-gray-400">Evidence</p>
            <div class="mt-1 flex flex-wrap gap-1.5 text-xs">
              {#each reviewPacket.evidence_refs as r}<RefLink
                  refValue={r}
                  threadId={artifact.thread_id}
                />{/each}
            </div>
          </div>
        {/if}
      </div>
    </div>
  {/if}

  {#if textContent}
    <div class="mt-4 rounded-lg border border-gray-200 bg-white">
      <div class="border-b border-gray-100 px-4 py-2.5">
        <h2
          class="text-xs font-semibold uppercase tracking-wider text-gray-400"
        >
          Content
        </h2>
        <span class="text-[11px] text-gray-400">{artifactContentType}</span>
      </div>
      <pre
        class="max-h-96 overflow-auto whitespace-pre-wrap px-4 py-3 text-xs text-gray-800">{textContent}</pre>
    </div>
  {/if}

  <details class="mt-4 rounded-lg border border-gray-200 bg-white">
    <summary
      class="cursor-pointer px-4 py-2.5 text-xs text-gray-400 hover:text-gray-600"
      >Raw metadata JSON</summary
    >
    <pre
      class="overflow-auto px-4 pb-3 text-[11px] text-gray-600">{JSON.stringify(
        artifact,
        null,
        2,
      )}</pre>
  </details>

  {#if artifactContent && !textContent}
    <details class="mt-2 rounded-lg border border-gray-200 bg-white">
      <summary
        class="cursor-pointer px-4 py-2.5 text-xs text-gray-400 hover:text-gray-600"
        >Raw content JSON</summary
      >
      <pre
        class="overflow-auto px-4 pb-3 text-[11px] text-gray-600">{JSON.stringify(
          artifactContent,
          null,
          2,
        )}</pre>
    </details>
  {/if}
{:else}
  <p class="text-sm text-gray-400">Artifact not found.</p>
{/if}
