<script>
  import { page } from "$app/stores";

  import GuidedTypedRefsInput from "$lib/components/GuidedTypedRefsInput.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import ProvenanceBadge from "$lib/components/ProvenanceBadge.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import { buildReviewPayload } from "$lib/reviewUtils";
  import { toTimelineView } from "$lib/timelineUtils";
  import { parseRef } from "$lib/typedRefs";
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
  let reviewFieldErrors = $state({});
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
  let isMarkdownContent = $derived(
    artifactContentType.includes("markdown") &&
      typeof textContent === "string" &&
      textContent.length > 0,
  );
  let renderedMarkdownBlocks = $derived(
    isMarkdownContent ? parseMarkdownLite(textContent) : [],
  );
  let artifactRefHints = $derived(buildArtifactRefHints());
  let reviewEvidenceSuggestions = $derived(
    buildRefSuggestions([
      receiptPacket?.receipt_id
        ? {
            value: `artifact:${receiptPacket.receipt_id}`,
            label: `Receipt · ${receiptPacket.receipt_id}`,
          }
        : artifact?.id
          ? {
              value: `artifact:${artifact.id}`,
              label: `Receipt · ${artifact.id}`,
            }
          : null,
      receiptPacket?.work_order_id
        ? {
            value: `artifact:${receiptPacket.work_order_id}`,
            label: `Work order · ${receiptPacket.work_order_id}`,
          }
        : null,
      ...(receiptPacket?.verification_evidence ?? []).map((refValue) => ({
        value: refValue,
        label: `Receipt evidence · ${refValue}`,
      })),
      ...(receiptPacket?.outputs ?? []).map((refValue) => ({
        value: refValue,
        label: `Receipt output · ${refValue}`,
      })),
      ...timelineView.slice(0, 8).map((event) => ({
        value: `event:${event.id}`,
        label: `Event · ${event.typeLabel}`,
      })),
    ]),
  );

  let reviewOutcomeGuidance = $derived(
    reviewDraft?.outcome === "accept"
      ? "Accept records that this receipt is sufficient and closes review without follow-up action."
      : reviewDraft?.outcome === "revise"
        ? "Revise records that more work is required. You can open a follow-up work order right after submit."
        : reviewDraft?.outcome === "escalate"
          ? "Escalate marks this as requiring higher-level intervention and should include clear justification in notes."
          : "",
  );

  let artifactHeaderTitle = $derived(
    String(artifact?.summary ?? "").trim() ||
      `${kindLabel(artifact?.kind ?? "artifact")} artifact`,
  );

  function blankReviewDraft() {
    return { outcome: "accept", notes: "", evidenceRefsInput: "" };
  }
  function generateReviewId() {
    return `artifact-review-${Math.random().toString(36).slice(2, 10)}`;
  }

  function buildRefSuggestions(candidates = []) {
    const seen = new Set();
    const suggestions = [];

    candidates.forEach((candidate) => {
      const value = String(candidate?.value ?? "").trim();
      if (!value || seen.has(value)) return;
      const parsed = parseRef(value);
      if (!parsed.prefix || !parsed.value) return;
      seen.add(value);
      suggestions.push({
        value,
        label: String(candidate?.label ?? "").trim() || value,
      });
    });

    return suggestions;
  }

  function firstFieldError(fieldErrors, fieldName) {
    const candidates = fieldErrors?.[fieldName];
    if (!Array.isArray(candidates) || candidates.length === 0) {
      return "";
    }
    return candidates[0];
  }

  function kindDescription(kind) {
    if (kind === "work_order") return "Execution plan and acceptance criteria";
    if (kind === "receipt")
      return "Recorded outcomes and verification evidence";
    if (kind === "review") return "Human review decision on a receipt";
    if (kind === "doc") return "Readable source document";
    if (kind === "evidence") return "Supporting evidence artifact";
    if (kind === "log") return "Operational log artifact";
    return "Artifact payload";
  }

  function truncateLabel(value, max = 72) {
    const text = String(value ?? "").trim();
    if (!text) return "";
    if (text.length <= max) return text;
    return `${text.slice(0, max)}...`;
  }

  function buildArtifactRefHints() {
    const hints = {};

    if (!artifact) return hints;

    hints[`artifact:${artifact.id}`] =
      `This ${kindLabel(artifact.kind).toLowerCase()}`;
    if (artifact.thread_id) {
      hints[`thread:${artifact.thread_id}`] = "Related thread";
    }

    if (workOrderPacket?.work_order_id) {
      hints[`artifact:${workOrderPacket.work_order_id}`] = "Work order";
    }

    if (receiptPacket?.receipt_id) {
      hints[`artifact:${receiptPacket.receipt_id}`] = "Receipt";
    } else if (artifact.kind === "receipt") {
      hints[`artifact:${artifact.id}`] = "Receipt";
    }

    if (receiptPacket?.work_order_id) {
      hints[`artifact:${receiptPacket.work_order_id}`] = "Work order";
    }

    if (reviewPacket?.review_id) {
      hints[`artifact:${reviewPacket.review_id}`] = "Review";
    }
    if (reviewPacket?.receipt_id) {
      hints[`artifact:${reviewPacket.receipt_id}`] = "Reviewed receipt";
    }
    if (reviewPacket?.work_order_id) {
      hints[`artifact:${reviewPacket.work_order_id}`] = "Related work order";
    }

    timelineView.slice(0, 30).forEach((event) => {
      hints[`event:${event.id}`] =
        `${event.typeLabel}: ${truncateLabel(event.summary, 52)}`;
    });

    return hints;
  }

  function parseMarkdownLite(markdown) {
    const blocks = [];
    const lines = String(markdown ?? "").split(/\r?\n/);
    let listItems = [];
    let inCode = false;
    let codeLines = [];
    let paragraphLines = [];

    const flushParagraph = () => {
      if (paragraphLines.length === 0) return;
      blocks.push({
        type: "paragraph",
        text: paragraphLines.join(" "),
      });
      paragraphLines = [];
    };

    const closeList = () => {
      if (listItems.length === 0) return;
      blocks.push({
        type: "list",
        items: listItems,
      });
      listItems = [];
    };

    const closeCodeBlock = () => {
      if (!inCode) return;
      blocks.push({
        type: "code",
        text: codeLines.join("\n"),
      });
      inCode = false;
      codeLines = [];
    };

    for (const rawLine of lines) {
      const line = String(rawLine ?? "");
      const trimmed = line.trim();

      if (trimmed.startsWith("```")) {
        flushParagraph();
        closeList();
        if (!inCode) {
          inCode = true;
          codeLines = [];
        } else {
          closeCodeBlock();
        }
        continue;
      }

      if (inCode) {
        codeLines.push(line);
        continue;
      }

      if (!trimmed) {
        flushParagraph();
        closeList();
        continue;
      }

      const headingMatch = trimmed.match(/^(#{1,3})\s+(.+)$/);
      if (headingMatch) {
        flushParagraph();
        closeList();
        blocks.push({
          type: "heading",
          level: headingMatch[1].length,
          text: headingMatch[2],
        });
        continue;
      }

      const bulletMatch = trimmed.match(/^[-*]\s+(.+)$/);
      if (bulletMatch) {
        flushParagraph();
        listItems.push(bulletMatch[1]);
        continue;
      }

      closeList();
      paragraphLines.push(trimmed);
    }

    flushParagraph();
    closeList();
    closeCodeBlock();

    return blocks;
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
    reviewFieldErrors = {};
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
      reviewFieldErrors = payload.fieldErrors ?? {};
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
      reviewFieldErrors = {};
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
      reviewFieldErrors = {};
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

  function kindBadge(kind) {
    const styles = {
      work_order: "bg-blue-50 text-blue-700",
      receipt: "bg-emerald-50 text-emerald-700",
      review: "bg-purple-50 text-purple-700",
      doc: "bg-amber-50 text-amber-700",
    };
    return styles[kind] ?? "bg-gray-100 text-gray-600";
  }
</script>

<nav
  class="mb-4 flex items-center gap-1.5 text-sm text-gray-400"
  aria-label="Breadcrumb"
>
  <a class="transition-colors hover:text-gray-600" href="/artifacts"
    >Artifacts</a
  >
  <svg
    class="h-3 w-3 text-gray-300"
    fill="none"
    viewBox="0 0 24 24"
    stroke="currentColor"
    stroke-width="2"
  >
    <path stroke-linecap="round" stroke-linejoin="round" d="M9 5l7 7-7 7" />
  </svg>
  <span class="truncate text-gray-600">{artifact?.summary || artifactId}</span>
</nav>

{#if loading}
  <div
    class="mt-12 flex items-center justify-center gap-2 text-sm text-gray-400"
  >
    <svg class="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
      <circle
        class="opacity-25"
        cx="12"
        cy="12"
        r="10"
        stroke="currentColor"
        stroke-width="4"
      ></circle>
      <path
        class="opacity-75"
        fill="currentColor"
        d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
      ></path>
    </svg>
    Loading...
  </div>
{:else if loadError}
  <div
    class="flex items-start gap-2 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700"
  >
    <svg
      class="mt-0.5 h-4 w-4 shrink-0 text-red-400"
      fill="none"
      viewBox="0 0 24 24"
      stroke="currentColor"
      stroke-width="2"
    >
      <path
        stroke-linecap="round"
        stroke-linejoin="round"
        d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L4.082 16.5c-.77.833.192 2.5 1.732 2.5z"
      />
    </svg>
    {loadError}
  </div>
{:else if artifact}
  <section class="rounded-xl border border-gray-200/80 bg-white p-5 shadow-sm">
    <p
      class="text-[11px] font-semibold uppercase tracking-[0.08em] text-gray-500"
    >
      Artifact detail
    </p>
    <h1
      aria-label={`Artifact Detail: ${artifact.id}`}
      class="mt-1 text-xl font-semibold text-gray-900"
    >
      {artifactHeaderTitle}
    </h1>
    <p class="mt-1 text-sm text-gray-600">{kindDescription(artifact.kind)}</p>

    <div class="mt-3 flex flex-wrap items-center gap-2 text-xs">
      <span
        class={`rounded-md px-2 py-0.5 font-medium ${kindBadge(artifact.kind)}`}
        >{kindLabel(artifact.kind)}</span
      >
      <span class="text-gray-400"
        >{formatTimestamp(artifact.created_at) || "—"}</span
      >
      <span class="text-gray-400">by {actorName(artifact.created_by)}</span>
      {#if artifact.thread_id}
        <RefLink
          humanize
          labelHints={artifactRefHints}
          refValue={`thread:${artifact.thread_id}`}
          showRaw
          threadId={artifact.thread_id}
        />
      {/if}
    </div>
    <p class="mt-1 text-[11px] text-gray-400">ID: {artifact.id}</p>
    <div class="mt-2">
      <ProvenanceBadge provenance={artifact.provenance} />
    </div>
  </section>

  {#if (artifact.refs ?? []).length > 0}
    <div
      class="mt-3 rounded-xl border border-gray-200/80 bg-white p-4 shadow-sm"
    >
      <h2 class="text-sm font-medium text-gray-900">Linked references</h2>
      <div class="mt-2 flex flex-wrap gap-1.5 text-xs">
        {#each artifact.refs ?? [] as refValue}
          <RefLink
            humanize
            labelHints={artifactRefHints}
            {refValue}
            showRaw
            threadId={artifact.thread_id}
          />
        {/each}
      </div>
    </div>
  {/if}

  {#if artifact.kind === "doc" && textContent}
    <div
      class="mt-3 rounded-xl border border-indigo-100 bg-indigo-50 px-4 py-3 text-xs text-indigo-700"
    >
      Document artifacts render in readable mode below. Raw content remains
      available in the debug panels.
    </div>
  {/if}

  {#if !isKnownPacketArtifactKind && artifact.kind !== "doc"}
    <div
      class="mt-3 flex items-center gap-2 rounded-lg bg-amber-50 px-4 py-3 text-xs text-amber-700"
    >
      <svg
        class="h-4 w-4 shrink-0 text-amber-400"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="2"
      >
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L4.082 16.5c-.77.833.192 2.5 1.732 2.5z"
        />
      </svg>
      Unknown artifact kind: {artifact.kind}
    </div>
  {/if}

  {#if workOrderPacket}
    <div class="mt-5 rounded-xl border border-gray-200/80 bg-white shadow-sm">
      <div class="border-b border-gray-100 px-5 py-3">
        <h2 class="text-sm font-medium text-gray-900">Work Order</h2>
      </div>
      <div class="px-5 py-4 text-sm text-gray-800">
        <p class="font-medium">{workOrderPacket.objective || "No objective"}</p>
        {#if (workOrderPacket.constraints ?? []).length > 0}
          <div class="mt-4">
            <p class="text-xs font-medium text-gray-400">Constraints</p>
            <ul class="mt-1.5 space-y-1 text-sm">
              {#each workOrderPacket.constraints as c}
                <li class="flex items-start gap-2">
                  <span
                    class="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-gray-300"
                  ></span>
                  {c}
                </li>
              {/each}
            </ul>
          </div>
        {/if}
        {#if (workOrderPacket.context_refs ?? []).length > 0}
          <div class="mt-4">
            <p class="text-xs font-medium text-gray-400">Context</p>
            <div class="mt-1.5 flex flex-wrap gap-1.5 text-xs">
              {#each workOrderPacket.context_refs as r}<RefLink
                  humanize
                  labelHints={artifactRefHints}
                  refValue={r}
                  showRaw
                  threadId={workOrderPacket.thread_id}
                />{/each}
            </div>
          </div>
        {/if}
        {#if (workOrderPacket.acceptance_criteria ?? []).length > 0}
          <div class="mt-4">
            <p class="text-xs font-medium text-gray-400">Acceptance criteria</p>
            <ul class="mt-1.5 space-y-1 text-sm">
              {#each workOrderPacket.acceptance_criteria as c}
                <li class="flex items-start gap-2">
                  <span
                    class="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-gray-300"
                  ></span>
                  {c}
                </li>
              {/each}
            </ul>
          </div>
        {/if}
        {#if (workOrderPacket.definition_of_done ?? []).length > 0}
          <div class="mt-4">
            <p class="text-xs font-medium text-gray-400">Definition of done</p>
            <ul class="mt-1.5 space-y-1 text-sm">
              {#each workOrderPacket.definition_of_done as d}
                <li class="flex items-start gap-2">
                  <span
                    class="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-gray-300"
                  ></span>
                  {d}
                </li>
              {/each}
            </ul>
          </div>
        {/if}
      </div>
    </div>
  {/if}

  {#if receiptPacket}
    <div class="mt-5 rounded-xl border border-gray-200/80 bg-white shadow-sm">
      <div class="border-b border-gray-100 px-5 py-3">
        <h2 class="text-sm font-medium text-gray-900">Receipt</h2>
      </div>
      <div class="px-5 py-4 text-sm">
        <div class="flex flex-wrap gap-3 text-xs text-gray-500">
          <span class="flex items-center gap-1"
            >Work order: <RefLink
              humanize
              labelHints={artifactRefHints}
              refValue={`artifact:${receiptPacket.work_order_id}`}
              showRaw
            /></span
          >
          <span class="flex items-center gap-1"
            >Thread: <RefLink
              humanize
              labelHints={artifactRefHints}
              refValue={`thread:${receiptPacket.thread_id}`}
              showRaw
            /></span
          >
        </div>
        {#if (receiptPacket.outputs ?? []).length > 0}
          <div class="mt-4">
            <p class="text-xs font-medium text-gray-400">Outputs</p>
            <div class="mt-1.5 flex flex-wrap gap-1.5 text-xs">
              {#each receiptPacket.outputs as r}<RefLink
                  humanize
                  labelHints={artifactRefHints}
                  refValue={r}
                  showRaw
                  threadId={receiptPacket.thread_id}
                />{/each}
            </div>
          </div>
        {/if}
        {#if (receiptPacket.verification_evidence ?? []).length > 0}
          <div class="mt-4">
            <p class="text-xs font-medium text-gray-400">
              Verification evidence
            </p>
            <div class="mt-1.5 flex flex-wrap gap-1.5 text-xs">
              {#each receiptPacket.verification_evidence as r}<RefLink
                  humanize
                  labelHints={artifactRefHints}
                  refValue={r}
                  showRaw
                  threadId={receiptPacket.thread_id}
                />{/each}
            </div>
          </div>
        {/if}
        <div class="mt-4">
          <p class="text-xs font-medium text-gray-400">Changes summary</p>
          <p class="mt-1.5 leading-relaxed text-gray-800">
            {receiptPacket.changes_summary || "—"}
          </p>
        </div>
        {#if (receiptPacket.known_gaps ?? []).length > 0}
          <div class="mt-4">
            <p class="text-xs font-medium text-gray-400">Known gaps</p>
            <ul class="mt-1.5 space-y-1 text-gray-600">
              {#each receiptPacket.known_gaps as g}
                <li class="flex items-start gap-2">
                  <span
                    class="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-amber-300"
                  ></span>
                  {g}
                </li>
              {/each}
            </ul>
          </div>
        {/if}
      </div>

      <!-- Review form -->
      <div class="border-t border-gray-100 px-5 py-4">
        <h3 class="text-sm font-medium text-gray-900">Submit Review</h3>
        {#if reviewErrors.length > 0}
          <ul
            class="mt-3 list-inside list-disc rounded-lg bg-red-50 px-4 py-2.5 text-xs text-red-700"
          >
            {#each reviewErrors as e}<li>{e}</li>{/each}
          </ul>
        {/if}
        {#if reviewNotice}
          <div
            class="mt-3 flex items-center gap-2 rounded-lg bg-emerald-50 px-3 py-2 text-xs text-emerald-700"
          >
            <svg
              class="h-3.5 w-3.5 shrink-0"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"
              />
            </svg>
            {reviewNotice}
          </div>
        {/if}
        {#if reviseFollowupLink}
          <div
            class="mt-3 flex items-center gap-2 rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-700"
          >
            <svg
              class="h-3.5 w-3.5 shrink-0"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
              />
            </svg>
            Outcome is revise.
            <a class="font-medium underline" href={reviseFollowupLink}
              >Create follow-up work order</a
            >
          </div>
        {/if}
        {#if reviewDraft}
          <form class="mt-3 grid gap-4" onsubmit={submitReview}>
            <label class="text-xs font-medium text-gray-600"
              >Outcome <select
                aria-label="Review outcome"
                bind:value={reviewDraft.outcome}
                class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-2.5 py-2 text-sm transition-colors focus:bg-white"
                ><option value="accept">Accept</option><option value="revise"
                  >Revise</option
                ><option value="escalate">Escalate</option></select
              ></label
            >
            {#if firstFieldError(reviewFieldErrors, "outcome")}
              <p class="-mt-2 text-xs text-red-700">
                {firstFieldError(reviewFieldErrors, "outcome")}
              </p>
            {/if}
            <p
              class="-mt-1 rounded-md border border-indigo-100 bg-indigo-50 px-3 py-2 text-xs text-indigo-700"
            >
              {reviewOutcomeGuidance}
            </p>
            <label class="text-xs font-medium text-gray-600"
              >Notes <textarea
                aria-label="Review notes"
                bind:value={reviewDraft.notes}
                class="mt-1.5 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm transition-colors focus:bg-white"
                placeholder="Review notes..."
                rows="2"
              ></textarea></label
            >
            {#if firstFieldError(reviewFieldErrors, "notes")}
              <p class="-mt-2 text-xs text-red-700">
                {firstFieldError(reviewFieldErrors, "notes")}
              </p>
            {/if}
            <div class="text-xs font-medium text-gray-600">
              Evidence refs
              <GuidedTypedRefsInput
                addButtonLabel="Add review evidence ref"
                addInputLabel="Add review evidence ref"
                addInputPlaceholder="artifact:artifact-evidence-123 or event:event-456"
                advancedHint="Paste typed refs separated by commas or new lines. This is for advanced/manual entry."
                advancedLabel="Advanced raw review evidence refs"
                advancedToggleLabel="Use advanced raw review evidence input"
                bind:value={reviewDraft.evidenceRefsInput}
                fieldError={firstFieldError(reviewFieldErrors, "evidence_refs")}
                helperText="Optional supporting refs. Quick picks include receipt outputs, evidence, and recent events."
                hideAdvancedToggleLabel="Hide advanced raw review evidence input"
                suggestions={reviewEvidenceSuggestions}
                textareaAriaLabel="Review evidence refs (typed refs, comma/newline separated; optional)"
              />
            </div>
            <div class="flex justify-end">
              <button
                class="rounded-md bg-indigo-600 px-4 py-2 text-xs font-medium text-white shadow-sm transition-colors hover:bg-indigo-500 disabled:opacity-50"
                disabled={submittingReview}
                type="submit"
                >{submittingReview ? "Submitting..." : "Submit review"}</button
              >
            </div>
          </form>
        {/if}
        {#if createdReview}
          <div class="mt-3 flex items-center gap-2 text-xs text-gray-500">
            <svg
              class="h-3.5 w-3.5 text-emerald-500"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"
              />
            </svg>
            Review submitted:
            <a
              class="font-medium text-indigo-600 hover:text-indigo-700"
              href={`/artifacts/${createdReview.id}`}
              >{createdReview.summary || createdReview.id}</a
            >
          </div>
        {/if}
      </div>

      <!-- Thread timeline -->
      {#if threadTimeline.length > 0 || timelineLoading}
        <div class="border-t border-gray-100 px-5 py-4">
          <h3 class="text-sm font-medium text-gray-900">Thread Timeline</h3>
          {#if timelineLoading}
            <div class="mt-3 flex items-center gap-2 text-xs text-gray-400">
              <svg
                class="h-3.5 w-3.5 animate-spin"
                fill="none"
                viewBox="0 0 24 24"
              >
                <circle
                  class="opacity-25"
                  cx="12"
                  cy="12"
                  r="10"
                  stroke="currentColor"
                  stroke-width="4"
                ></circle>
                <path
                  class="opacity-75"
                  fill="currentColor"
                  d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                ></path>
              </svg>
              Loading...
            </div>
          {:else if timelineError}
            <p class="mt-3 text-xs text-red-600">{timelineError}</p>
          {:else}
            <div class="mt-3 space-y-1.5">
              {#each timelineView.slice(0, 10) as event}
                <div
                  class="rounded-lg border border-gray-100 bg-gray-50 px-4 py-2.5 text-xs"
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
    <div class="mt-5 rounded-xl border border-gray-200/80 bg-white shadow-sm">
      <div class="border-b border-gray-100 px-5 py-3">
        <h2 class="text-sm font-medium text-gray-900">Review</h2>
      </div>
      <div class="px-5 py-4 text-sm">
        <div class="flex items-center gap-3">
          <span
            class="rounded-md px-2.5 py-0.5 text-xs font-medium {reviewPacket.outcome ===
            'accept'
              ? 'bg-emerald-50 text-emerald-700'
              : reviewPacket.outcome === 'revise'
                ? 'bg-amber-50 text-amber-700'
                : 'bg-red-50 text-red-700'}">{reviewPacket.outcome}</span
          >
          <span class="text-xs text-gray-500"
            >Receipt: <RefLink
              humanize
              labelHints={artifactRefHints}
              refValue={`artifact:${reviewPacket.receipt_id}`}
              showRaw
              threadId={artifact.thread_id}
            /></span
          >
          <span class="text-xs text-gray-500"
            >Work order: <RefLink
              humanize
              labelHints={artifactRefHints}
              refValue={`artifact:${reviewPacket.work_order_id}`}
              showRaw
              threadId={artifact.thread_id}
            /></span
          >
        </div>
        {#if reviewPacket.notes}<p class="mt-3 leading-relaxed text-gray-700">
            {reviewPacket.notes}
          </p>{/if}
        {#if (reviewPacket.evidence_refs ?? []).length > 0}
          <div class="mt-4">
            <p class="text-xs font-medium text-gray-400">Evidence</p>
            <div class="mt-1.5 flex flex-wrap gap-1.5 text-xs">
              {#each reviewPacket.evidence_refs as r}<RefLink
                  humanize
                  labelHints={artifactRefHints}
                  refValue={r}
                  showRaw
                  threadId={artifact.thread_id}
                />{/each}
            </div>
          </div>
        {/if}
      </div>
    </div>
  {/if}

  {#if textContent}
    <div class="mt-5 rounded-xl border border-gray-200/80 bg-white shadow-sm">
      <div
        class="flex items-center justify-between border-b border-gray-100 px-5 py-3"
      >
        <h2 class="text-sm font-medium text-gray-900">Text Content</h2>
        <span class="text-[11px] text-gray-400">{artifactContentType}</span>
      </div>
      {#if isMarkdownContent}
        <article
          class="markdown-lite max-h-[34rem] overflow-auto px-5 py-4 text-sm text-gray-800"
        >
          {#each renderedMarkdownBlocks as block}
            {#if block.type === "heading"}
              {#if block.level === 1}
                <h1>{block.text}</h1>
              {:else if block.level === 2}
                <h2>{block.text}</h2>
              {:else}
                <h3>{block.text}</h3>
              {/if}
            {:else if block.type === "list"}
              <ul>
                {#each block.items as item}
                  <li>{item}</li>
                {/each}
              </ul>
            {:else if block.type === "code"}
              <pre><code>{block.text}</code></pre>
            {:else}
              <p>{block.text}</p>
            {/if}
          {/each}
        </article>
      {:else}
        <article
          class="max-h-[34rem] overflow-auto px-5 py-4 text-sm leading-7 text-gray-800 whitespace-pre-wrap"
        >
          {textContent}
        </article>
      {/if}
    </div>
  {/if}

  <details class="mt-5 rounded-xl border border-gray-200/80 bg-white shadow-sm">
    <summary
      class="cursor-pointer px-5 py-3 text-xs text-gray-400 transition-colors hover:text-gray-600"
      >Raw metadata JSON</summary
    >
    <pre
      class="overflow-auto px-5 pb-4 text-[11px] text-gray-500">{JSON.stringify(
        artifact,
        null,
        2,
      )}</pre>
  </details>

  {#if artifactContent && !textContent}
    <details
      class="mt-2 rounded-xl border border-gray-200/80 bg-white shadow-sm"
    >
      <summary
        class="cursor-pointer px-5 py-3 text-xs text-gray-400 transition-colors hover:text-gray-600"
        >Raw content JSON</summary
      >
      <pre
        class="overflow-auto px-5 pb-4 text-[11px] text-gray-500">{JSON.stringify(
          artifactContent,
          null,
          2,
        )}</pre>
    </details>
  {/if}
{:else}
  <div class="mt-8 text-center text-sm text-gray-400">Artifact not found.</div>
{/if}
