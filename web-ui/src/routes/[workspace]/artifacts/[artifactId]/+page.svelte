<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import GuidedTypedRefsInput from "$lib/components/GuidedTypedRefsInput.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import { coreClient } from "$lib/coreClient";
  import { kindLabel, kindDescription, kindColor } from "$lib/artifactKinds";
  import { formatTimestamp } from "$lib/formatDate";
  import { workspacePath } from "$lib/workspacePaths";
  import ProvenanceBadge from "$lib/components/ProvenanceBadge.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import { buildReviewPayload } from "$lib/reviewUtils";
  import { toTimelineView } from "$lib/timelineUtils";
  import { parseRef } from "$lib/typedRefs";
  import {
    lookupActorDisplayName,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";

  const KNOWN_PACKET_ARTIFACT_KINDS = new Set([
    "work_order",
    "receipt",
    "review",
  ]);

  let artifactId = $derived($page.params.artifactId);
  let workspaceSlug = $derived($page.params.workspace);
  let actorName = $derived((id) =>
    lookupActorDisplayName(id, $actorRegistry, $principalRegistry),
  );
  let artifact = $state(null);
  let artifactContent = $state(null);
  let artifactContentType = $state("");
  let loading = $state(false);
  let loadError = $state("");
  let contentLoadError = $state("");
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
  let confirmModal = $state({ open: false, action: "" });
  let lifecycleBusy = $state(false);

  $effect(() => {
    const id = artifactId;
    if (id && id !== loadedArtifactId) loadArtifact(id);
  });

  $effect(() => {
    artifactId;
    confirmModal = { open: false, action: "" };
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
  let hasTextContent = $derived(
    typeof textContent === "string" && textContent.length > 0,
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

  function workspaceHref(pathname = "/") {
    return workspacePath(workspaceSlug, pathname);
  }

  let reviewOutcomeGuidance = $derived(
    reviewDraft?.outcome === "accept"
      ? "Accept records that this receipt is sufficient and closes review without follow-up."
      : reviewDraft?.outcome === "revise"
        ? "Revise records that more work is required. You can open a follow-up work order after."
        : reviewDraft?.outcome === "escalate"
          ? "Escalate marks this as requiring higher-level intervention."
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
    if (!Array.isArray(candidates) || candidates.length === 0) return "";
    return candidates[0];
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
    if (artifact.thread_id)
      hints[`thread:${artifact.thread_id}`] = "Related thread";
    if (workOrderPacket?.work_order_id)
      hints[`artifact:${workOrderPacket.work_order_id}`] = "Work order";
    if (receiptPacket?.receipt_id)
      hints[`artifact:${receiptPacket.receipt_id}`] = "Receipt";
    else if (artifact.kind === "receipt")
      hints[`artifact:${artifact.id}`] = "Receipt";
    if (receiptPacket?.work_order_id)
      hints[`artifact:${receiptPacket.work_order_id}`] = "Work order";
    if (reviewPacket?.review_id)
      hints[`artifact:${reviewPacket.review_id}`] = "Review";
    if (reviewPacket?.receipt_id)
      hints[`artifact:${reviewPacket.receipt_id}`] = "Reviewed receipt";
    if (reviewPacket?.work_order_id)
      hints[`artifact:${reviewPacket.work_order_id}`] = "Related work order";
    timelineView.slice(0, 30).forEach((event) => {
      hints[`event:${event.id}`] =
        `${event.typeLabel}: ${truncateLabel(event.summary, 52)}`;
    });
    return hints;
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
    if (event?.preventDefault) event.preventDefault();
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
        reviseFollowupLink = workspaceHref(
          `/threads/${encodeURIComponent(artifact.thread_id)}?${params.toString()}#work-order-composer`,
        );
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
    contentLoadError = "";
    loadedArtifactId = targetId;

    let loadedArtifact = null;
    try {
      loadedArtifact =
        (await coreClient.getArtifact(targetId)).artifact ?? null;
    } catch (e) {
      loadError = `Failed to load artifact: ${e instanceof Error ? e.message : String(e)}`;
      artifact = null;
      artifactContent = null;
      artifactContentType = "";
      threadTimeline = [];
      timelineError = "";
      loading = false;
      return;
    }

    if (!loadedArtifact) {
      loadError = "Artifact not found.";
      artifact = null;
      artifactContent = null;
      artifactContentType = "";
      loading = false;
      return;
    }

    artifact = loadedArtifact;
    reviewDraft = blankReviewDraft();
    reviewErrors = [];
    reviewFieldErrors = {};
    reviewNotice = "";
    createdReview = null;
    reviseFollowupLink = "";

    try {
      const contentResponse = await coreClient.getArtifactContent(targetId);
      artifactContent = contentResponse.content ?? null;
      artifactContentType = contentResponse.contentType ?? "";
    } catch (e) {
      artifactContent = null;
      artifactContentType = "";
      contentLoadError = `Content unavailable: ${e instanceof Error ? e.message : String(e)}`;
    }

    try {
      if (artifact?.kind === "receipt" && artifact?.thread_id)
        await loadThreadTimeline(artifact.thread_id);
      else {
        threadTimeline = [];
        timelineError = "";
      }
    } catch {
      threadTimeline = [];
    }

    loading = false;
  }

  async function handleArchiveArtifact() {
    if (!artifact?.id || lifecycleBusy || artifact.tombstoned_at) return;
    lifecycleBusy = true;
    try {
      await coreClient.archiveArtifact(artifact.id, {});
      await loadArtifact(artifact.id);
    } finally {
      lifecycleBusy = false;
    }
  }

  async function handleUnarchiveArtifact() {
    confirmModal = { open: false, action: "" };
    if (!artifact?.id || lifecycleBusy || artifact.tombstoned_at) return;
    lifecycleBusy = true;
    try {
      await coreClient.unarchiveArtifact(artifact.id, {});
      await loadArtifact(artifact.id);
    } finally {
      lifecycleBusy = false;
    }
  }

  function handleConfirm() {
    const action = confirmModal.action;
    confirmModal = { open: false, action: "" };
    if (action === "archive") handleArchiveArtifact();
    else if (action === "trash") handleTombstoneArtifact();
  }

  async function handleTombstoneArtifact() {
    if (!artifact?.id || lifecycleBusy) return;
    lifecycleBusy = true;
    try {
      await coreClient.tombstoneArtifact(artifact.id, {});
      await goto(workspaceHref("/artifacts"));
    } finally {
      lifecycleBusy = false;
    }
  }

  async function handleRestoreArtifact() {
    confirmModal = { open: false, action: "" };
    if (!artifact?.id || lifecycleBusy) return;
    lifecycleBusy = true;
    try {
      await coreClient.restoreArtifact(artifact.id, {});
      await loadArtifact(artifact.id);
    } finally {
      lifecycleBusy = false;
    }
  }
</script>

<nav
  class="mb-3 flex items-center gap-1.5 text-[12px] text-[var(--ui-text-muted)]"
  aria-label="Breadcrumb"
>
  <a
    class="transition-colors hover:text-[var(--ui-text)]"
    href={workspaceHref("/artifacts")}>Artifacts</a
  >
  <span class="text-[var(--ui-text-subtle)]">/</span>
  <span class="truncate text-[var(--ui-text-muted)]"
    >{artifact?.summary || artifactId}</span
  >
</nav>

{#if loading}
  <div
    class="mt-8 flex items-center justify-center gap-2 text-[13px] text-[var(--ui-text-muted)]"
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
  <div class="rounded-md bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
    {loadError}
  </div>
{:else if artifact}
  {#if artifact?.tombstoned_at}
    <div
      class="tombstone-banner mb-4 flex flex-wrap items-start justify-between gap-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-[13px] text-red-400"
    >
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2 font-semibold">
          <span>⚠</span>
          <span>This artifact has been tombstoned</span>
        </div>
        {#if artifact.tombstone_reason}
          <p class="mt-2">Reason: {artifact.tombstone_reason}</p>
        {/if}
        <p class="mt-1 text-[11px] text-red-400/80">
          Tombstoned {#if artifact.tombstoned_by}by {actorName(
              artifact.tombstoned_by,
            )}{/if}
          {#if artifact.tombstoned_at}
            {formatTimestamp(artifact.tombstoned_at)}
          {/if}
        </p>
      </div>
      <button
        class="shrink-0 cursor-pointer rounded-md border border-red-500/40 bg-red-500/15 px-2 py-1 text-[12px] font-medium text-red-400 hover:bg-red-500/25 disabled:opacity-50"
        disabled={lifecycleBusy}
        onclick={handleRestoreArtifact}
        type="button"
      >
        {lifecycleBusy ? "…" : "Restore"}
      </button>
    </div>
  {:else if artifact?.archived_at}
    <div
      class="mb-4 flex flex-wrap items-start justify-between gap-3 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-[13px] text-amber-400"
    >
      <p class="min-w-0 flex-1">
        This artifact was archived on {formatTimestamp(artifact.archived_at) ||
          "—"}{#if artifact.archived_by}
          by {actorName(artifact.archived_by)}{/if}.
      </p>
      <button
        class="shrink-0 cursor-pointer rounded-md border border-amber-500/40 bg-amber-500/15 px-2 py-1 text-[12px] font-medium text-amber-400 hover:bg-amber-500/25 disabled:opacity-50"
        disabled={lifecycleBusy}
        onclick={handleUnarchiveArtifact}
        type="button"
      >
        {lifecycleBusy ? "…" : "Unarchive"}
      </button>
    </div>
  {/if}
  <section
    class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] p-4"
  >
    <div class="flex items-start justify-between gap-3">
      <div class="min-w-0 flex-1">
        <h1 class="text-lg font-semibold text-[var(--ui-text)]">
          {artifactHeaderTitle}
        </h1>
        <p class="mt-0.5 text-[13px] text-[var(--ui-text-muted)]">
          {kindDescription(artifact.kind)}
        </p>
      </div>
      {#if !artifact.tombstoned_at}
        <div class="flex shrink-0 items-center gap-1.5">
          {#if !artifact.archived_at}
            <button
              aria-label="Archive"
              class="cursor-pointer rounded-md p-1.5 text-[var(--ui-text-muted)] hover:bg-[var(--ui-border)] hover:text-[var(--ui-accent)] disabled:opacity-50"
              disabled={lifecycleBusy}
              onclick={() => (confirmModal = { open: true, action: "archive" })}
              type="button"
            >
              <svg
                class="h-4 w-4"
                fill="currentColor"
                viewBox="0 0 24 24"
                aria-hidden="true"
              >
                <path
                  d="M20.25 7.5l-.625 10.632a2.25 2.25 0 01-2.247 2.118H6.622a2.25 2.25 0 01-2.247-2.118L3.75 7.5m8.25 3v6.75m0 0l-3-3m3 3l3-3M3.375 7.5h17.25c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125H3.375c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125z"
                />
              </svg>
            </button>
          {/if}
          <button
            aria-label="Move artifact to trash"
            class="cursor-pointer rounded-md p-1.5 text-[var(--ui-text-muted)] hover:bg-[var(--ui-border)] hover:text-red-400 disabled:opacity-50"
            disabled={lifecycleBusy}
            onclick={() => (confirmModal = { open: true, action: "trash" })}
            type="button"
          >
            <svg
              class="h-4 w-4"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0"
              />
            </svg>
          </button>
        </div>
      {/if}
    </div>

    <div class="mt-2 flex flex-wrap items-center gap-2 text-[12px]">
      <span class="rounded px-1.5 py-0.5 font-medium {kindColor(artifact.kind)}"
        >{kindLabel(artifact.kind)}</span
      >
      <span class="text-[var(--ui-text-muted)]"
        >{formatTimestamp(artifact.created_at) || "—"}</span
      >
      <span class="text-[var(--ui-text-muted)]"
        >by {actorName(artifact.created_by)}</span
      >
    </div>
    {#if artifact.thread_id}
      <div class="mt-1.5 text-[12px] text-[var(--ui-text-muted)]">
        <span class="text-[var(--ui-text-subtle)]">Thread</span>
        <a
          class="ml-1 text-indigo-400 transition-colors hover:text-indigo-300"
          href={workspaceHref(
            `/threads/${encodeURIComponent(artifact.thread_id)}`,
          )}
        >
          {artifact.thread_id}
        </a>
      </div>
    {/if}
    <div class="mt-1.5">
      <ProvenanceBadge provenance={artifact.provenance} />
    </div>
  </section>

  {#if artifact.content_hash}
    <details
      class="mt-3 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)]"
    >
      <summary
        class="cursor-pointer px-4 py-2.5 text-[11px] text-[var(--ui-text-muted)] hover:text-[var(--ui-text)]"
        >Hashes</summary
      >
      <div class="px-4 pb-3 pt-1">
        <p
          class="text-[11px] uppercase tracking-[0.12em] text-[var(--ui-text-subtle)]"
        >
          Content hash
        </p>
        <p
          class="mt-1 break-all font-mono text-[12px] text-[var(--ui-text-muted)]"
        >
          {artifact.content_hash}
        </p>
      </div>
    </details>
  {/if}

  {@const nonThreadRefs = (artifact.refs ?? []).filter(
    (r) => r !== `thread:${artifact.thread_id}`,
  )}
  {#if nonThreadRefs.length > 0}
    <div
      class="mt-3 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] p-3"
    >
      <h2 class="text-[13px] font-medium text-[var(--ui-text)]">
        Linked references
      </h2>
      <div class="mt-1.5 flex flex-wrap gap-1.5 text-[11px]">
        {#each nonThreadRefs as refValue}
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

  {#if contentLoadError}
    <div
      class="mt-3 rounded-md border border-[var(--ui-border)] px-3 py-2 text-[12px] text-[var(--ui-text-muted)]"
    >
      Content unavailable for this artifact.
    </div>
  {/if}

  {#if !contentLoadError && !isKnownPacketArtifactKind && artifact.kind !== "doc" && !hasTextContent}
    <div
      class="mt-3 rounded-md bg-amber-500/10 px-3 py-2 text-[12px] text-amber-400"
    >
      No structured view available for this artifact.
    </div>
  {/if}

  {#if workOrderPacket}
    <div
      class="mt-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)]"
    >
      <div class="border-b border-[var(--ui-border)] px-4 py-2.5">
        <h2 class="text-[13px] font-medium text-[var(--ui-text)]">
          Work Order
        </h2>
      </div>
      <div class="px-4 py-3 text-[13px] text-[var(--ui-text)]">
        <p class="font-medium">{workOrderPacket.objective || "No objective"}</p>
        {#if (workOrderPacket.constraints ?? []).length > 0}
          <div class="mt-3">
            <p class="text-[11px] font-medium text-[var(--ui-text-muted)]">
              Constraints
            </p>
            <ul class="mt-1 space-y-0.5">
              {#each workOrderPacket.constraints as c}
                <li class="flex items-start gap-2">
                  <span class="mt-1.5 h-1 w-1 shrink-0 rounded-full bg-gray-300"
                  ></span>{c}
                </li>
              {/each}
            </ul>
          </div>
        {/if}
        {#if (workOrderPacket.context_refs ?? []).length > 0}
          <div class="mt-3">
            <p class="text-[11px] font-medium text-[var(--ui-text-muted)]">
              Context
            </p>
            <div class="mt-1 flex flex-wrap gap-1.5 text-[11px]">
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
          <div class="mt-3">
            <p class="text-[11px] font-medium text-[var(--ui-text-muted)]">
              Acceptance criteria
            </p>
            <ul class="mt-1 space-y-0.5">
              {#each workOrderPacket.acceptance_criteria as c}
                <li class="flex items-start gap-2">
                  <span class="mt-1.5 h-1 w-1 shrink-0 rounded-full bg-gray-300"
                  ></span>{c}
                </li>
              {/each}
            </ul>
          </div>
        {/if}
        {#if (workOrderPacket.definition_of_done ?? []).length > 0}
          <div class="mt-3">
            <p class="text-[11px] font-medium text-[var(--ui-text-muted)]">
              Definition of done
            </p>
            <ul class="mt-1 space-y-0.5">
              {#each workOrderPacket.definition_of_done as d}
                <li class="flex items-start gap-2">
                  <span class="mt-1.5 h-1 w-1 shrink-0 rounded-full bg-gray-300"
                  ></span>{d}
                </li>
              {/each}
            </ul>
          </div>
        {/if}
      </div>
    </div>
  {/if}

  {#if receiptPacket}
    <div
      class="mt-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)]"
    >
      <div class="border-b border-[var(--ui-border)] px-4 py-2.5">
        <h2 class="text-[13px] font-medium text-[var(--ui-text)]">Receipt</h2>
      </div>
      <div class="px-4 py-3 text-[13px]">
        <div
          class="flex flex-wrap gap-3 text-[12px] text-[var(--ui-text-muted)]"
        >
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
          <div class="mt-3">
            <p class="text-[11px] font-medium text-[var(--ui-text-muted)]">
              Outputs
            </p>
            <div class="mt-1 flex flex-wrap gap-1.5 text-[11px]">
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
          <div class="mt-3">
            <p class="text-[11px] font-medium text-[var(--ui-text-muted)]">
              Verification evidence
            </p>
            <div class="mt-1 flex flex-wrap gap-1.5 text-[11px]">
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
        <div class="mt-3">
          <p class="text-[11px] font-medium text-[var(--ui-text-muted)]">
            Changes summary
          </p>
          {#if receiptPacket.changes_summary}
            <MarkdownRenderer
              source={receiptPacket.changes_summary}
              class="mt-1 leading-relaxed text-[var(--ui-text)]"
            />
          {:else}
            <p class="mt-1 leading-relaxed text-[var(--ui-text)]">—</p>
          {/if}
        </div>
        {#if (receiptPacket.known_gaps ?? []).length > 0}
          <div class="mt-3">
            <p class="text-[11px] font-medium text-[var(--ui-text-muted)]">
              Known gaps
            </p>
            <ul class="mt-1 space-y-0.5 text-[var(--ui-text-muted)]">
              {#each receiptPacket.known_gaps as g}
                <li class="flex items-start gap-2">
                  <span
                    class="mt-1.5 h-1 w-1 shrink-0 rounded-full bg-amber-300"
                  ></span>{g}
                </li>
              {/each}
            </ul>
          </div>
        {/if}
      </div>

      <div class="border-t border-[var(--ui-border)] px-4 py-3">
        <h3 class="text-[13px] font-medium text-[var(--ui-text)]">
          Submit Review
        </h3>
        {#if reviewErrors.length > 0}
          <ul
            class="mt-2 list-inside list-disc rounded-md bg-red-500/10 px-3 py-2 text-[12px] text-red-400"
          >
            {#each reviewErrors as e}<li>{e}</li>{/each}
          </ul>
        {/if}
        {#if reviewNotice}
          <div
            class="mt-2 rounded-md bg-emerald-500/10 px-3 py-1.5 text-[12px] text-emerald-400"
          >
            {reviewNotice}
          </div>
        {/if}
        {#if reviseFollowupLink}
          <div
            class="mt-2 rounded-md bg-amber-500/10 px-3 py-1.5 text-[12px] text-amber-400"
          >
            Outcome is revise.
            <a class="font-medium underline" href={reviseFollowupLink}
              >Create follow-up work order</a
            >
          </div>
        {/if}
        {#if reviewDraft}
          <form class="mt-2 grid gap-3" onsubmit={submitReview}>
            <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
              >Outcome
              <select
                aria-label="Review outcome"
                bind:value={reviewDraft.outcome}
                class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-2.5 py-1.5 text-[13px] focus:bg-[var(--ui-panel)]"
              >
                <option value="accept">Accept</option><option value="revise"
                  >Revise</option
                ><option value="escalate">Escalate</option>
              </select>
            </label>
            {#if firstFieldError(reviewFieldErrors, "outcome")}<p
                class="-mt-1 text-[11px] text-red-400"
              >
                {firstFieldError(reviewFieldErrors, "outcome")}
              </p>{/if}
            {#if reviewOutcomeGuidance}
              <p
                class="-mt-1 rounded-md bg-[var(--ui-bg-soft)] px-3 py-1.5 text-[12px] text-[var(--ui-text-muted)]"
              >
                {reviewOutcomeGuidance}
              </p>
            {/if}
            <label class="text-[12px] font-medium text-[var(--ui-text-muted)]"
              >Notes
              <textarea
                aria-label="Review notes"
                bind:value={reviewDraft.notes}
                class="mt-1 w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-1.5 text-[13px] focus:bg-[var(--ui-panel)]"
                placeholder="Review notes..."
                rows="2"
              ></textarea>
            </label>
            {#if firstFieldError(reviewFieldErrors, "notes")}<p
                class="-mt-1 text-[11px] text-red-400"
              >
                {firstFieldError(reviewFieldErrors, "notes")}
              </p>{/if}
            <div class="text-[12px] font-medium text-[var(--ui-text-muted)]">
              Evidence refs
              <GuidedTypedRefsInput
                addButtonLabel="Add review evidence ref"
                addInputLabel="Add review evidence ref"
                addInputPlaceholder="artifact:artifact-evidence-123 or event:event-456"
                advancedHint="Paste typed refs separated by commas or new lines."
                advancedLabel="Advanced raw review evidence refs"
                advancedToggleLabel="Use advanced raw review evidence input"
                bind:value={reviewDraft.evidenceRefsInput}
                fieldError={firstFieldError(reviewFieldErrors, "evidence_refs")}
                helperText="Optional supporting refs."
                hideAdvancedToggleLabel="Hide advanced raw review evidence input"
                suggestions={reviewEvidenceSuggestions}
                textareaAriaLabel="Review evidence refs (typed refs, comma/newline separated; optional)"
              />
            </div>
            <div class="flex justify-end">
              <button
                class="cursor-pointer rounded-md bg-indigo-600 px-3 py-1.5 text-[12px] font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
                disabled={submittingReview}
                type="submit"
                >{submittingReview ? "Submitting..." : "Submit review"}</button
              >
            </div>
          </form>
        {/if}
        {#if createdReview}
          <div class="mt-2 text-[12px] text-[var(--ui-text-muted)]">
            Review submitted: <a
              class="font-medium text-indigo-400 hover:text-indigo-400"
              href={workspaceHref(`/artifacts/${createdReview.id}`)}
              >{createdReview.summary || createdReview.id}</a
            >
          </div>
        {/if}
      </div>

      {#if threadTimeline.length > 0 || timelineLoading}
        <div class="border-t border-[var(--ui-border)] px-4 py-3">
          <h3 class="text-[13px] font-medium text-[var(--ui-text)]">
            Thread Timeline
          </h3>
          {#if timelineLoading}
            <div class="mt-2 text-[12px] text-[var(--ui-text-muted)]">
              Loading...
            </div>
          {:else if timelineError}
            <p class="mt-2 text-[12px] text-red-400">{timelineError}</p>
          {:else}
            <div class="mt-2 space-y-1">
              {#each timelineView.slice(0, 10) as event}
                <div
                  class="rounded-md bg-[var(--ui-bg-soft)] px-3 py-2 text-[12px]"
                >
                  <MarkdownRenderer
                    source={event.summary}
                    class="font-medium text-[var(--ui-text)]"
                  />
                  <p class="text-[11px] text-[var(--ui-text-muted)]">
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
    <div
      class="mt-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)]"
    >
      <div class="border-b border-[var(--ui-border)] px-4 py-2.5">
        <h2 class="text-[13px] font-medium text-[var(--ui-text)]">Review</h2>
      </div>
      <div class="px-4 py-3 text-[13px]">
        <div class="flex items-center gap-3">
          <span
            class="rounded px-1.5 py-0.5 text-[12px] font-medium {reviewPacket.outcome ===
            'accept'
              ? 'bg-emerald-500/10 text-emerald-400'
              : reviewPacket.outcome === 'revise'
                ? 'bg-amber-500/10 text-amber-400'
                : 'bg-red-500/10 text-red-400'}">{reviewPacket.outcome}</span
          >
          <span class="text-[12px] text-[var(--ui-text-muted)]"
            >Receipt: <RefLink
              humanize
              labelHints={artifactRefHints}
              refValue={`artifact:${reviewPacket.receipt_id}`}
              showRaw
              threadId={artifact.thread_id}
            /></span
          >
          <span class="text-[12px] text-[var(--ui-text-muted)]"
            >Work order: <RefLink
              humanize
              labelHints={artifactRefHints}
              refValue={`artifact:${reviewPacket.work_order_id}`}
              showRaw
              threadId={artifact.thread_id}
            /></span
          >
        </div>
        {#if reviewPacket.notes}
          <MarkdownRenderer
            source={reviewPacket.notes}
            class="mt-2 leading-relaxed text-[var(--ui-text)]"
          />
        {/if}
        {#if (reviewPacket.evidence_refs ?? []).length > 0}
          <div class="mt-3">
            <p class="text-[11px] font-medium text-[var(--ui-text-muted)]">
              Evidence
            </p>
            <div class="mt-1 flex flex-wrap gap-1.5 text-[11px]">
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

  {#if hasTextContent}
    <div
      class="mt-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)]"
    >
      <div
        class="flex items-center justify-between border-b border-[var(--ui-border)] px-4 py-2.5"
      >
        <h2 class="text-[13px] font-medium text-[var(--ui-text)]">
          Text Content
        </h2>
        <span class="text-[11px] text-[var(--ui-text-muted)]"
          >{artifactContentType}</span
        >
      </div>
      <pre
        class="max-h-[30rem] overflow-auto whitespace-pre-wrap break-words px-4 py-3 font-mono text-[12px] leading-relaxed text-[var(--ui-text)]">{textContent}</pre>
    </div>
  {/if}

  <details
    class="mt-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)]"
  >
    <summary
      class="cursor-pointer px-4 py-2.5 text-[11px] text-[var(--ui-text-muted)] hover:text-[var(--ui-text)]"
      >Raw metadata — ID: {artifact.id}</summary
    >
    <pre
      class="overflow-auto px-4 pb-3 text-[11px] text-[var(--ui-text-muted)]">{JSON.stringify(
        artifact,
        null,
        2,
      )}</pre>
  </details>

  {#if artifactContent && !textContent}
    <details
      class="mt-2 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)]"
    >
      <summary
        class="cursor-pointer px-4 py-2.5 text-[11px] text-[var(--ui-text-muted)] hover:text-[var(--ui-text)]"
        >Raw content JSON</summary
      >
      <pre
        class="overflow-auto px-4 pb-3 text-[11px] text-[var(--ui-text-muted)]">{JSON.stringify(
          artifactContent,
          null,
          2,
        )}</pre>
    </details>
  {/if}
{:else}
  <div class="mt-8 text-center text-[13px] text-[var(--ui-text-muted)]">
    Artifact not found.
  </div>
{/if}

<ConfirmModal
  open={confirmModal.open}
  title={confirmModal.action === "trash" ? "Move to trash" : "Archive artifact"}
  message={confirmModal.action === "trash"
    ? "This artifact will be tombstoned. You can restore it from trash later."
    : "This artifact will be hidden from default views. You can unarchive it later."}
  confirmLabel={confirmModal.action === "trash" ? "Trash" : "Archive"}
  variant={confirmModal.action === "trash" ? "danger" : "warning"}
  busy={lifecycleBusy}
  onconfirm={handleConfirm}
  oncancel={() => (confirmModal = { open: false, action: "" })}
/>
