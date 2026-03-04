<script>
  import { page } from "$app/stores";

  import { coreClient } from "$lib/coreClient";
  import ProvenanceBadge from "$lib/components/ProvenanceBadge.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import UnknownObjectPanel from "$lib/components/UnknownObjectPanel.svelte";
  import { buildReviewPayload } from "$lib/reviewUtils";
  import { toTimelineView } from "$lib/timelineUtils";

  const KNOWN_PACKET_ARTIFACT_KINDS = new Set([
    "work_order",
    "receipt",
    "review",
  ]);

  $: artifactId = $page.params.artifactId;
  let artifact = null;
  let artifactContent = null;
  let artifactContentType = "";
  let loading = false;
  let loadError = "";
  let loadedArtifactId = "";
  let reviewDraft = null;
  let submittingReview = false;
  let reviewErrors = [];
  let reviewNotice = "";
  let createdReview = null;
  let reviseFollowupLink = "";
  let threadTimeline = [];
  let timelineLoading = false;
  let timelineError = "";

  $: if (artifactId && artifactId !== loadedArtifactId) {
    loadArtifact(artifactId);
  }

  $: receiptPacket =
    artifact?.kind === "receipt" &&
    artifactContentType.includes("application/json") &&
    artifactContent &&
    typeof artifactContent === "object" &&
    !Array.isArray(artifactContent)
      ? artifactContent
      : null;
  $: workOrderPacket =
    artifact?.kind === "work_order" &&
    artifactContentType.includes("application/json") &&
    artifactContent &&
    typeof artifactContent === "object" &&
    !Array.isArray(artifactContent)
      ? artifactContent
      : null;
  $: reviewPacket =
    artifact?.kind === "review" &&
    artifactContentType.includes("application/json") &&
    artifactContent &&
    typeof artifactContent === "object" &&
    !Array.isArray(artifactContent)
      ? artifactContent
      : null;
  $: textContent =
    artifactContentType.startsWith("text/") &&
    typeof artifactContent === "string"
      ? artifactContent
      : "";
  $: isKnownPacketArtifactKind = KNOWN_PACKET_ARTIFACT_KINDS.has(
    String(artifact?.kind ?? ""),
  );
  $: timelineView = toTimelineView(threadTimeline, {
    threadId: artifact?.thread_id ?? "",
  });

  function blankReviewDraft() {
    return {
      outcome: "accept",
      notes: "",
      evidenceRefsInput: "",
    };
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
      const response = await coreClient.listThreadTimeline(threadId);
      threadTimeline = response.events ?? [];
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      timelineError = `Failed to load thread timeline: ${reason}`;
      threadTimeline = [];
    } finally {
      timelineLoading = false;
    }
  }

  async function submitReview() {
    if (!artifact || !receiptPacket || !reviewDraft) {
      return;
    }

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
        reviseFollowupLink = `/threads/${encodeURIComponent(
          artifact.thread_id,
        )}?${params.toString()}#work-order-composer`;
      }

      await loadThreadTimeline(artifact.thread_id);
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      reviewErrors = [`Failed to submit review: ${reason}`];
    } finally {
      submittingReview = false;
    }
  }

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
        artifactContentType = "";
        return;
      }

      const contentResponse =
        await coreClient.getArtifactContent(targetArtifactId);
      artifactContent = contentResponse.content ?? null;
      artifactContentType = contentResponse.contentType ?? "";
      reviewDraft = blankReviewDraft();
      reviewErrors = [];
      reviewNotice = "";
      createdReview = null;
      reviseFollowupLink = "";

      if (artifact?.kind === "receipt" && artifact?.thread_id) {
        await loadThreadTimeline(artifact.thread_id);
      } else {
        threadTimeline = [];
        timelineError = "";
      }
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      loadError = `Failed to load artifact: ${reason}`;
      artifact = null;
      artifactContent = null;
      artifactContentType = "";
      threadTimeline = [];
      timelineError = "";
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

    {#if !isKnownPacketArtifactKind}
      <p
        class="mt-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800"
      >
        Unknown artifact kind: <span class="font-mono">{artifact.kind}</span>.
        Rendering raw metadata/content below.
      </p>
    {/if}

    {#if workOrderPacket}
      <div class="mt-4 rounded-md border border-slate-200 bg-slate-50 p-3">
        <h2
          class="text-sm font-semibold uppercase tracking-wide text-slate-600"
        >
          Work Order Packet
        </h2>
        <p class="mt-2 text-xs text-slate-600">
          work_order_id: {workOrderPacket.work_order_id}
        </p>
        <p class="mt-1 text-xs text-slate-600">
          thread_id:
          <RefLink
            refValue={`thread:${workOrderPacket.thread_id}`}
            threadId={workOrderPacket.thread_id}
          />
        </p>
        <p class="mt-3 text-sm text-slate-800">
          {workOrderPacket.objective || "No objective"}
        </p>

        <div class="mt-3">
          <p
            class="text-xs font-semibold uppercase tracking-wide text-slate-500"
          >
            Constraints
          </p>
          <ul class="mt-1 list-disc space-y-1 pl-5 text-sm text-slate-700">
            {#if (workOrderPacket.constraints ?? []).length === 0}
              <li>none</li>
            {:else}
              {#each workOrderPacket.constraints ?? [] as item}
                <li>{item}</li>
              {/each}
            {/if}
          </ul>
        </div>

        <div class="mt-3">
          <p
            class="text-xs font-semibold uppercase tracking-wide text-slate-500"
          >
            Context Refs
          </p>
          <ul class="mt-1 list-disc space-y-1 pl-5 text-sm text-slate-700">
            {#if (workOrderPacket.context_refs ?? []).length === 0}
              <li>none</li>
            {:else}
              {#each workOrderPacket.context_refs ?? [] as refValue}
                <li>
                  <RefLink {refValue} threadId={workOrderPacket.thread_id} />
                </li>
              {/each}
            {/if}
          </ul>
        </div>

        <div class="mt-3">
          <p
            class="text-xs font-semibold uppercase tracking-wide text-slate-500"
          >
            Acceptance Criteria
          </p>
          <ul class="mt-1 list-disc space-y-1 pl-5 text-sm text-slate-700">
            {#if (workOrderPacket.acceptance_criteria ?? []).length === 0}
              <li>none</li>
            {:else}
              {#each workOrderPacket.acceptance_criteria ?? [] as item}
                <li>{item}</li>
              {/each}
            {/if}
          </ul>
        </div>

        <div class="mt-3">
          <p
            class="text-xs font-semibold uppercase tracking-wide text-slate-500"
          >
            Definition Of Done
          </p>
          <ul class="mt-1 list-disc space-y-1 pl-5 text-sm text-slate-700">
            {#if (workOrderPacket.definition_of_done ?? []).length === 0}
              <li>none</li>
            {:else}
              {#each workOrderPacket.definition_of_done ?? [] as item}
                <li>{item}</li>
              {/each}
            {/if}
          </ul>
        </div>
      </div>
    {/if}

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

        <div class="mt-4 rounded-md border border-slate-200 bg-white p-3">
          <h3
            class="text-sm font-semibold uppercase tracking-wide text-slate-600"
          >
            Review
          </h3>
          <p class="mt-1 text-xs text-slate-600">
            Submit a lightweight review for this receipt.
          </p>

          {#if reviewErrors.length > 0}
            <ul
              class="mt-2 list-disc space-y-1 rounded-md border border-rose-200 bg-rose-50 px-4 py-2 text-xs text-rose-800"
            >
              {#each reviewErrors as errorLine}
                <li>{errorLine}</li>
              {/each}
            </ul>
          {/if}

          {#if reviewNotice}
            <p
              class="mt-2 rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2 text-xs text-emerald-800"
            >
              {reviewNotice}
            </p>
          {/if}

          {#if reviseFollowupLink}
            <p
              class="mt-2 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800"
            >
              Outcome is `revise`. Create a follow-up work order:
              <a
                class="font-semibold underline decoration-amber-300 underline-offset-2 hover:text-amber-700"
                href={reviseFollowupLink}
              >
                open work order composer
              </a>
            </p>
          {/if}

          {#if reviewDraft}
            <form
              class="mt-3 rounded-md border border-slate-200 bg-slate-50 p-3"
              on:submit|preventDefault={submitReview}
            >
              <div class="grid gap-3">
                <label
                  class="text-xs font-semibold uppercase tracking-wide text-slate-600"
                >
                  Review outcome
                  <select
                    bind:value={reviewDraft.outcome}
                    class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
                  >
                    <option value="accept">accept</option>
                    <option value="revise">revise</option>
                    <option value="escalate">escalate</option>
                  </select>
                </label>

                <label
                  class="text-xs font-semibold uppercase tracking-wide text-slate-600"
                >
                  Review notes
                  <textarea
                    bind:value={reviewDraft.notes}
                    class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
                    rows="3"
                  ></textarea>
                </label>

                <label
                  class="text-xs font-semibold uppercase tracking-wide text-slate-600"
                >
                  Review evidence refs (typed refs, comma/newline separated;
                  optional)
                  <textarea
                    bind:value={reviewDraft.evidenceRefsInput}
                    class="mt-1 w-full rounded-md border border-slate-300 px-2 py-1.5 text-sm"
                    rows="3"
                  ></textarea>
                </label>
              </div>

              <div class="mt-3">
                <button
                  class="rounded-md bg-slate-900 px-3 py-1.5 text-xs font-semibold text-white hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-60"
                  disabled={submittingReview}
                  type="submit"
                >
                  {submittingReview ? "Submitting..." : "Submit review"}
                </button>
              </div>
            </form>
          {/if}

          {#if createdReview}
            <div class="mt-3 rounded-md border border-slate-200 bg-white p-3">
              <p
                class="text-xs font-semibold uppercase tracking-wide text-slate-600"
              >
                Latest submitted review
              </p>
              <p class="mt-1 text-sm text-slate-800">
                artifact id:
                <RefLink refValue={`artifact:${createdReview.id}`} />
              </p>
              <div class="mt-2 flex flex-wrap gap-2 text-xs">
                {#each createdReview.refs ?? [] as refValue}
                  <span class="rounded bg-slate-100 px-2 py-1">
                    <RefLink {refValue} threadId={artifact.thread_id} />
                  </span>
                {/each}
              </div>
              <div class="mt-2">
                <UnknownObjectPanel
                  objectData={createdReview}
                  title="Raw Review Artifact JSON"
                />
              </div>
            </div>
          {/if}
        </div>

        <div class="mt-4 rounded-md border border-slate-200 bg-white p-3">
          <h3
            class="text-sm font-semibold uppercase tracking-wide text-slate-600"
          >
            Thread Timeline
          </h3>

          {#if timelineLoading}
            <p class="mt-2 text-xs text-slate-600">Loading timeline...</p>
          {:else if timelineError}
            <p
              class="mt-2 rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-800"
            >
              {timelineError}
            </p>
          {:else if timelineView.length === 0}
            <p class="mt-2 text-xs text-slate-600">No timeline events found.</p>
          {:else}
            <ul class="mt-2 space-y-2 text-xs text-slate-700">
              {#each timelineView.slice(0, 10) as event}
                <li class="rounded border border-slate-200 bg-slate-50 p-2">
                  <p class="font-semibold text-slate-800">{event.summary}</p>
                  <p class="mt-1">
                    type: {event.typeLabel} | actor:
                    {event.actor_id}
                  </p>
                  <div class="mt-1 flex flex-wrap gap-2">
                    {#each event.refs ?? [] as refValue}
                      <span class="rounded bg-white px-2 py-0.5">
                        <RefLink {refValue} threadId={artifact.thread_id} />
                      </span>
                    {/each}
                  </div>
                </li>
              {/each}
            </ul>
          {/if}
        </div>
      </div>
    {/if}

    {#if reviewPacket}
      <div class="mt-4 rounded-md border border-slate-200 bg-slate-50 p-3">
        <h2
          class="text-sm font-semibold uppercase tracking-wide text-slate-600"
        >
          Review Packet
        </h2>
        <p class="mt-2 text-xs text-slate-600">
          review_id: {reviewPacket.review_id}
        </p>
        <p class="mt-1 text-xs text-slate-600">
          outcome:
          <span class="font-semibold text-slate-700"
            >{reviewPacket.outcome}</span
          >
        </p>
        <p class="mt-1 text-xs text-slate-600">
          receipt_id:
          <RefLink
            refValue={`artifact:${reviewPacket.receipt_id}`}
            threadId={artifact.thread_id}
          />
        </p>
        <p class="mt-1 text-xs text-slate-600">
          work_order_id:
          <RefLink
            refValue={`artifact:${reviewPacket.work_order_id}`}
            threadId={artifact.thread_id}
          />
        </p>

        <div class="mt-3">
          <p
            class="text-xs font-semibold uppercase tracking-wide text-slate-500"
          >
            Notes
          </p>
          <p class="mt-1 text-sm text-slate-700">
            {reviewPacket.notes || "none"}
          </p>
        </div>

        <div class="mt-3">
          <p
            class="text-xs font-semibold uppercase tracking-wide text-slate-500"
          >
            Evidence Refs
          </p>
          <ul class="mt-1 list-disc space-y-1 pl-5 text-sm text-slate-700">
            {#if (reviewPacket.evidence_refs ?? []).length === 0}
              <li>none</li>
            {:else}
              {#each reviewPacket.evidence_refs ?? [] as refValue}
                <li><RefLink {refValue} threadId={artifact.thread_id} /></li>
              {/each}
            {/if}
          </ul>
        </div>
      </div>
    {/if}

    {#if textContent}
      <div class="mt-4 rounded-md border border-slate-200 bg-slate-50 p-3">
        <h2
          class="text-sm font-semibold uppercase tracking-wide text-slate-600"
        >
          Text Content
        </h2>
        <p class="mt-1 text-xs text-slate-500">
          content-type: {artifactContentType}
        </p>
        <pre
          class="mt-2 max-h-96 overflow-auto whitespace-pre-wrap rounded bg-slate-900 p-3 text-xs text-slate-100">{textContent}</pre>
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
{:else}
  <p class="mt-4 rounded-md bg-white p-3 text-sm text-slate-700 shadow-sm">
    Artifact not found.
  </p>
{/if}
