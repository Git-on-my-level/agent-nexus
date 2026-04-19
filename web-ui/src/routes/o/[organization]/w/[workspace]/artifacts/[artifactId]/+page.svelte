<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import ArchiveButton from "$lib/components/ArchiveButton.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import TrashButton from "$lib/components/TrashButton.svelte";
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
  import { topicDetailPathFromRef } from "$lib/topicRouteUtils";
  import { parseRef } from "$lib/typedRefs";
  import {
    lookupActorDisplayName,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";

  const KNOWN_PACKET_ARTIFACT_KINDS = new Set(["receipt", "review"]);

  let artifactId = $derived($page.params.artifactId);
  let organizationSlug = $derived($page.params.organization);
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
  let artifactTopicRef = $derived.by(() => {
    const candidates = [
      String(receiptPacket?.subject_ref ?? "").trim(),
      ...((artifact?.refs ?? []).map((ref) => String(ref ?? "").trim()) ?? []),
    ];
    return (
      candidates.find((refValue) => {
        const parsed = parseRef(refValue);
        return (
          (parsed.prefix === "topic" || parsed.prefix === "thread") &&
          String(parsed.value ?? "").trim()
        );
      }) ?? ""
    );
  });
  let artifactTopicHref = $derived(
    artifactTopicRef ? topicDetailPathFromRef(artifactTopicRef) : "",
  );
  let artifactTopicLabel = $derived(
    String(parseRef(artifactTopicRef).value ?? "").trim() ||
      String(artifact?.thread_id ?? "").trim(),
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
      String(receiptPacket?.subject_ref ?? "")
        .trim()
        .startsWith("card:")
        ? {
            value: String(receiptPacket.subject_ref).trim(),
            label: `Card · ${String(receiptPacket.subject_ref).trim()}`,
          }
        : null,
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
    return workspacePath(organizationSlug, workspaceSlug, pathname);
  }

  let reviewOutcomeGuidance = $derived(
    reviewDraft?.outcome === "accept"
      ? "Accept records that this receipt is sufficient and closes review without follow-up."
      : reviewDraft?.outcome === "revise"
        ? "Revise records that more work is required on the card before another receipt."
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
    return `rv-${Math.random().toString(36).slice(2, 10)}`;
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

  function firstTypedRefValue(refs, prefix) {
    const list = Array.isArray(refs) ? refs : [];
    const needle = `${String(prefix ?? "").trim()}:`;
    if (!needle || needle === ":") return "";
    const hit = list.find((r) => String(r ?? "").startsWith(needle));
    if (!hit) return "";
    return String(parseRef(String(hit)).value ?? "").trim();
  }

  let docArtifactDocPath = $derived.by(() => {
    if (!artifact || String(artifact.kind ?? "").trim() !== "doc") return "";
    const docId =
      String(artifact.document_id ?? "").trim() ||
      firstTypedRefValue(artifact.refs, "document");
    return docId ? `/docs/${encodeURIComponent(docId)}` : "";
  });

  let docArtifactRevisionPath = $derived.by(() => {
    if (!artifact || String(artifact.kind ?? "").trim() !== "doc") return "";
    const revId =
      String(artifact.revision_id ?? "").trim() ||
      firstTypedRefValue(artifact.refs, "document_revision");
    return revId ? `/docs/revisions/${encodeURIComponent(revId)}` : "";
  });

  function buildArtifactRefHints() {
    const hints = {};
    if (!artifact) return hints;
    hints[`artifact:${artifact.id}`] =
      `This ${kindLabel(artifact.kind).toLowerCase()}`;
    if (artifact.kind === "doc") {
      const docId = String(artifact.document_id ?? "").trim();
      if (docId) hints[`document:${docId}`] = "Document";
      const revId = String(artifact.revision_id ?? "").trim();
      if (revId) hints[`document_revision:${revId}`] = "Document revision";
    }
    if (artifact.thread_id)
      hints[`thread:${artifact.thread_id}`] = "Thread (timeline)";
    if (receiptPacket?.receipt_id)
      hints[`artifact:${receiptPacket.receipt_id}`] = "Receipt";
    else if (artifact.kind === "receipt")
      hints[`artifact:${artifact.id}`] = "Receipt";
    if (reviewPacket?.review_id)
      hints[`artifact:${reviewPacket.review_id}`] = "Review";
    if (reviewPacket?.receipt_id)
      hints[`artifact:${reviewPacket.receipt_id}`] = "Reviewed receipt";
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
    const subjectRef =
      String(receiptPacket?.subject_ref ?? "").trim() ||
      (() => {
        const first = (artifact.refs ?? []).find((r) =>
          /^(topic|thread|card):/.test(String(r)),
        );
        return first ? String(first).trim() : "";
      })();
    const payload = buildReviewPayload(reviewDraft, {
      subjectRef,
      receiptId: artifact.id,
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
        reviseFollowupLink = artifactTopicHref
          ? workspaceHref(artifactTopicHref)
          : "";
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
    if (!artifact?.id || lifecycleBusy || artifact.trashed_at) return;
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
    if (!artifact?.id || lifecycleBusy || artifact.trashed_at) return;
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
    else if (action === "trash") handleTrashArtifact();
  }

  async function handleTrashArtifact() {
    if (!artifact?.id || lifecycleBusy) return;
    lifecycleBusy = true;
    try {
      await coreClient.trashArtifact(artifact.id, {});
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
  class="mb-3 flex items-center gap-1.5 text-micro text-[var(--fg-muted)]"
  aria-label="Breadcrumb"
>
  <a
    class="transition-colors hover:text-[var(--fg)]"
    href={workspaceHref("/artifacts")}>Artifacts</a
  >
  <span class="text-[var(--fg-subtle)]">/</span>
  <span class="truncate text-[var(--fg-muted)]"
    >{artifact?.summary || artifactId}</span
  >
</nav>

{#if loading}
  <div
    class="mt-8 flex items-center justify-center gap-2 text-meta text-[var(--fg-muted)]"
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
  <div class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text">
    {loadError}
  </div>
{:else if artifact}
  {#if artifact?.trashed_at}
    <div
      class="trash-banner mb-4 flex flex-wrap items-start justify-between gap-3 rounded-md border border-danger/30 bg-danger-soft px-3 py-2 text-meta text-danger-text"
    >
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2 font-semibold">
          <span>⚠</span>
          <span>This artifact is in trash</span>
        </div>
        {#if artifact.trash_reason}
          <p class="mt-2">Reason: {artifact.trash_reason}</p>
        {/if}
        <p class="mt-1 text-micro text-danger-text/80">
          Trashed {#if artifact.trashed_by}by {actorName(
              artifact.trashed_by,
            )}{/if}
          {#if artifact.trashed_at}
            {formatTimestamp(artifact.trashed_at)}
          {/if}
        </p>
      </div>
      <button
        class="shrink-0 cursor-pointer rounded-md border border-danger/40 bg-danger-soft px-2 py-1 text-micro font-medium text-danger-text hover:bg-danger/25 disabled:opacity-50"
        disabled={lifecycleBusy}
        onclick={handleRestoreArtifact}
        type="button"
      >
        {lifecycleBusy ? "…" : "Restore"}
      </button>
    </div>
  {:else if artifact?.archived_at}
    <div
      class="mb-4 flex flex-wrap items-start justify-between gap-3 rounded-md border border-warn/30 bg-warn-soft px-3 py-2 text-meta text-warn-text"
    >
      <p class="min-w-0 flex-1">
        This artifact was archived on {formatTimestamp(artifact.archived_at) ||
          "—"}{#if artifact.archived_by}
          by {actorName(artifact.archived_by)}{/if}.
      </p>
      <button
        class="shrink-0 cursor-pointer rounded-md border border-warn/40 bg-warn-soft px-2 py-1 text-micro font-medium text-warn-text hover:bg-warn/25 disabled:opacity-50"
        disabled={lifecycleBusy}
        onclick={handleUnarchiveArtifact}
        type="button"
      >
        {lifecycleBusy ? "…" : "Unarchive"}
      </button>
    </div>
  {/if}
  <section
    class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] p-4"
  >
    <div class="flex items-start justify-between gap-3">
      <div class="min-w-0 flex-1">
        <h1 class="text-subtitle font-semibold text-[var(--fg)]">
          {artifactHeaderTitle}
        </h1>
        <p class="mt-0.5 text-meta text-[var(--fg-muted)]">
          {kindDescription(artifact.kind)}
        </p>
      </div>
      {#if !artifact.trashed_at}
        <div class="flex shrink-0 items-center gap-1">
          {#if !artifact.archived_at}
            <ArchiveButton
              busy={lifecycleBusy}
              size="md"
              onarchive={() =>
                (confirmModal = { open: true, action: "archive" })}
            />
          {/if}
          <TrashButton
            busy={lifecycleBusy}
            size="md"
            ontrash={() => (confirmModal = { open: true, action: "trash" })}
          />
        </div>
      {/if}
    </div>

    <div class="mt-2 flex flex-wrap items-center gap-2 text-micro">
      <span class="rounded px-1.5 py-0.5 font-medium {kindColor(artifact.kind)}"
        >{kindLabel(artifact.kind)}</span
      >
      <span class="text-[var(--fg-muted)]"
        >{formatTimestamp(artifact.created_at) || "—"}</span
      >
      <span class="text-[var(--fg-muted)]"
        >by {actorName(artifact.created_by)}</span
      >
    </div>
    {#if docArtifactDocPath}
      <div
        class="mt-1.5 flex flex-wrap items-center gap-2 text-micro text-[var(--fg-muted)]"
      >
        <a
          class="inline-flex items-center rounded-md border border-fuchsia-500/35 bg-fuchsia-500/10 px-2 py-0.5 font-medium text-fuchsia-300 transition-colors hover:bg-fuchsia-500/20"
          href={workspaceHref(docArtifactDocPath)}
        >
          Open in Docs
        </a>
        {#if docArtifactRevisionPath}
          <a
            class="text-accent-text underline decoration-dotted underline-offset-2 transition-colors hover:text-accent-text"
            href={workspaceHref(docArtifactRevisionPath)}>This revision</a
          >
        {/if}
      </div>
    {/if}
    {#if artifact.thread_id && artifactTopicHref}
      <div class="mt-1.5 text-micro text-[var(--fg-muted)]">
        <span class="text-[var(--fg-muted)]">Topic</span>
        <a
          class="ml-1 text-accent-text transition-colors hover:text-accent-text"
          href={workspaceHref(artifactTopicHref)}
        >
          {artifactTopicLabel}
        </a>
      </div>
    {/if}
    <div class="mt-1.5">
      <ProvenanceBadge provenance={artifact.provenance} />
    </div>
  </section>

  {#if artifact.content_hash}
    <details
      class="mt-3 rounded-md border border-[var(--line)] bg-[var(--bg-soft)]"
    >
      <summary
        class="cursor-pointer px-4 py-2.5 text-micro text-[var(--fg-muted)] hover:text-[var(--fg)]"
        >Hashes</summary
      >
      <div class="px-4 pb-3 pt-1">
        <p
          class="text-micro uppercase tracking-[0.12em] text-[var(--fg-muted)]"
        >
          Content hash
        </p>
        <p class="mt-1 break-all font-mono text-micro text-[var(--fg-muted)]">
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
      class="mt-3 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] p-3"
    >
      <h2 class="text-meta font-medium text-[var(--fg)]">Linked references</h2>
      <div class="mt-1.5 flex flex-wrap gap-1.5 text-micro">
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
      class="mt-3 rounded-md border border-[var(--line)] px-3 py-2 text-micro text-[var(--fg-muted)]"
    >
      Content unavailable for this artifact.
    </div>
  {/if}

  {#if !contentLoadError && !isKnownPacketArtifactKind && artifact.kind !== "doc" && !hasTextContent}
    <div
      class="mt-3 rounded-md bg-warn-soft px-3 py-2 text-micro text-warn-text"
    >
      No structured view available for this artifact.
    </div>
  {/if}

  {#if receiptPacket}
    <div
      class="mt-4 rounded-md border border-[var(--line)] bg-[var(--bg-soft)]"
    >
      <div class="border-b border-[var(--line)] px-4 py-2.5">
        <h2 class="text-meta font-medium text-[var(--fg)]">Receipt</h2>
      </div>
      <div class="px-4 py-3 text-meta">
        <div class="flex flex-wrap gap-3 text-micro text-[var(--fg-muted)]">
          <span class="flex items-center gap-1"
            >Subject: {#if String(receiptPacket.subject_ref ?? "").trim()}<RefLink
                humanize
                labelHints={artifactRefHints}
                refValue={String(receiptPacket.subject_ref).trim()}
                showRaw
              />{:else}<span class="text-[var(--fg-muted)]">—</span>{/if}</span
          >
        </div>
        {#if (receiptPacket.outputs ?? []).length > 0}
          <div class="mt-3">
            <p class="text-micro font-medium text-[var(--fg-muted)]">Outputs</p>
            <div class="mt-1 flex flex-wrap gap-1.5 text-micro">
              {#each receiptPacket.outputs as r}<RefLink
                  humanize
                  labelHints={artifactRefHints}
                  refValue={r}
                  showRaw
                  threadId={artifact?.thread_id ?? ""}
                />{/each}
            </div>
          </div>
        {/if}
        {#if (receiptPacket.verification_evidence ?? []).length > 0}
          <div class="mt-3">
            <p class="text-micro font-medium text-[var(--fg-muted)]">
              Verification evidence
            </p>
            <div class="mt-1 flex flex-wrap gap-1.5 text-micro">
              {#each receiptPacket.verification_evidence as r}<RefLink
                  humanize
                  labelHints={artifactRefHints}
                  refValue={r}
                  showRaw
                  threadId={artifact?.thread_id ?? ""}
                />{/each}
            </div>
          </div>
        {/if}
        <div class="mt-3">
          <p class="text-micro font-medium text-[var(--fg-muted)]">
            Changes summary
          </p>
          {#if receiptPacket.changes_summary}
            <MarkdownRenderer
              source={receiptPacket.changes_summary}
              class="mt-1 leading-relaxed text-[var(--fg)]"
            />
          {:else}
            <p class="mt-1 leading-relaxed text-[var(--fg)]">—</p>
          {/if}
        </div>
        {#if (receiptPacket.known_gaps ?? []).length > 0}
          <div class="mt-3">
            <p class="text-micro font-medium text-[var(--fg-muted)]">
              Known gaps
            </p>
            <ul class="mt-1 space-y-0.5 text-[var(--fg-muted)]">
              {#each receiptPacket.known_gaps as g}
                <li class="flex items-start gap-2">
                  <span
                    class="mt-1.5 h-1 w-1 shrink-0 rounded-full bg-warn-text"
                  ></span>{g}
                </li>
              {/each}
            </ul>
          </div>
        {/if}
      </div>

      <div class="border-t border-[var(--line)] px-4 py-3">
        <h3 class="text-meta font-medium text-[var(--fg)]">Submit Review</h3>
        {#if reviewErrors.length > 0}
          <ul
            class="mt-2 list-inside list-disc rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
          >
            {#each reviewErrors as e}<li>{e}</li>{/each}
          </ul>
        {/if}
        {#if reviewNotice}
          <div
            class="mt-2 rounded-md bg-ok-soft px-3 py-1.5 text-micro text-ok-text"
          >
            {reviewNotice}
          </div>
        {/if}
        {#if reviseFollowupLink}
          <div
            class="mt-2 rounded-md bg-warn-soft px-3 py-1.5 text-micro text-warn-text"
          >
            Outcome is revise.
            <a class="font-medium underline" href={reviseFollowupLink}
              >Open topic</a
            >
            to continue on the card.
          </div>
        {/if}
        {#if reviewDraft}
          <form class="mt-2 grid gap-3" onsubmit={submitReview}>
            <label class="text-micro font-medium text-[var(--fg-muted)]"
              >Outcome
              <select
                aria-label="Review outcome"
                bind:value={reviewDraft.outcome}
                class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-2.5 py-1.5 text-meta focus:bg-[var(--panel)]"
              >
                <option value="accept">Accept</option><option value="revise"
                  >Revise</option
                ><option value="escalate">Escalate</option>
              </select>
            </label>
            {#if firstFieldError(reviewFieldErrors, "outcome")}<p
                class="-mt-1 text-micro text-danger-text"
              >
                {firstFieldError(reviewFieldErrors, "outcome")}
              </p>{/if}
            {#if reviewOutcomeGuidance}
              <p
                class="-mt-1 rounded-md bg-[var(--bg-soft)] px-3 py-1.5 text-micro text-[var(--fg-muted)]"
              >
                {reviewOutcomeGuidance}
              </p>
            {/if}
            <label class="text-micro font-medium text-[var(--fg-muted)]"
              >Notes
              <textarea
                aria-label="Review notes"
                bind:value={reviewDraft.notes}
                class="mt-1 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-1.5 text-meta focus:bg-[var(--panel)]"
                placeholder="Review notes..."
                rows="2"
              ></textarea>
            </label>
            {#if firstFieldError(reviewFieldErrors, "notes")}<p
                class="-mt-1 text-micro text-danger-text"
              >
                {firstFieldError(reviewFieldErrors, "notes")}
              </p>{/if}
            <div class="text-micro font-medium text-[var(--fg-muted)]">
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
                helperText="At least one typed ref required."
                hideAdvancedToggleLabel="Hide advanced raw review evidence input"
                suggestions={reviewEvidenceSuggestions}
                textareaAriaLabel="Review evidence refs (typed refs, comma/newline separated)"
              />
            </div>
            <div class="flex justify-end">
              <button
                class="cursor-pointer rounded-md bg-accent-solid px-3 py-1.5 text-micro font-medium text-white hover:bg-accent disabled:opacity-50"
                disabled={submittingReview}
                type="submit"
                >{submittingReview ? "Submitting..." : "Submit review"}</button
              >
            </div>
          </form>
        {/if}
        {#if createdReview}
          <div class="mt-2 text-micro text-[var(--fg-muted)]">
            Review submitted: <a
              class="font-medium text-accent-text hover:text-accent-text"
              href={workspaceHref(`/artifacts/${createdReview.id}`)}
              >{createdReview.summary || createdReview.id}</a
            >
          </div>
        {/if}
      </div>

      {#if threadTimeline.length > 0 || timelineLoading}
        <div class="border-t border-[var(--line)] px-4 py-3">
          <h3 class="text-meta font-medium text-[var(--fg)]">Topic Timeline</h3>
          {#if timelineLoading}
            <div class="mt-2 text-micro text-[var(--fg-muted)]">Loading...</div>
          {:else if timelineError}
            <p class="mt-2 text-micro text-danger-text">{timelineError}</p>
          {:else}
            <div class="mt-2 space-y-1">
              {#each timelineView.slice(0, 10) as event}
                <div
                  class="rounded-md bg-[var(--bg-soft)] px-3 py-2 text-micro"
                >
                  <MarkdownRenderer
                    source={event.summary}
                    class="font-medium text-[var(--fg)]"
                  />
                  <p class="text-micro text-[var(--fg-muted)]">
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
      class="mt-4 rounded-md border border-[var(--line)] bg-[var(--bg-soft)]"
    >
      <div class="border-b border-[var(--line)] px-4 py-2.5">
        <h2 class="text-meta font-medium text-[var(--fg)]">Review</h2>
      </div>
      <div class="px-4 py-3 text-meta">
        <div class="flex items-center gap-3">
          <span
            class="rounded px-1.5 py-0.5 text-micro font-medium {reviewPacket.outcome ===
            'accept'
              ? 'bg-ok-soft text-ok-text'
              : reviewPacket.outcome === 'revise'
                ? 'bg-warn-soft text-warn-text'
                : 'bg-danger-soft text-danger-text'}"
            >{reviewPacket.outcome}</span
          >
          <span class="text-micro text-[var(--fg-muted)]"
            >Receipt: <RefLink
              humanize
              labelHints={artifactRefHints}
              refValue={`artifact:${reviewPacket.receipt_id}`}
              showRaw
              threadId={artifact.thread_id}
            /></span
          >
          {#if String(reviewPacket.subject_ref ?? "").trim()}
            <span class="text-micro text-[var(--fg-muted)]"
              >Subject: <RefLink
                humanize
                labelHints={artifactRefHints}
                refValue={String(reviewPacket.subject_ref ?? "").trim()}
                showRaw
                threadId={artifact.thread_id}
              /></span
            >
          {/if}
        </div>
        {#if reviewPacket.notes}
          <MarkdownRenderer
            source={reviewPacket.notes}
            class="mt-2 leading-relaxed text-[var(--fg)]"
          />
        {/if}
        {#if (reviewPacket.evidence_refs ?? []).length > 0}
          <div class="mt-3">
            <p class="text-micro font-medium text-[var(--fg-muted)]">
              Evidence
            </p>
            <div class="mt-1 flex flex-wrap gap-1.5 text-micro">
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
      class="mt-4 rounded-md border border-[var(--line)] bg-[var(--bg-soft)]"
    >
      <div
        class="flex items-center justify-between border-b border-[var(--line)] px-4 py-2.5"
      >
        <h2 class="text-meta font-medium text-[var(--fg)]">Text Content</h2>
        <span class="text-micro text-[var(--fg-muted)]"
          >{artifactContentType}</span
        >
      </div>
      <pre
        class="max-h-[30rem] overflow-auto whitespace-pre-wrap break-words px-4 py-3 font-mono text-micro leading-relaxed text-[var(--fg)]">{textContent}</pre>
    </div>
  {/if}

  <details
    class="mt-4 rounded-md border border-[var(--line)] bg-[var(--bg-soft)]"
  >
    <summary
      class="cursor-pointer px-4 py-2.5 text-micro text-[var(--fg-muted)] hover:text-[var(--fg)]"
      >Raw metadata — ID: {artifact.id}</summary
    >
    <pre
      class="overflow-auto px-4 pb-3 text-micro text-[var(--fg-muted)]">{JSON.stringify(
        artifact,
        null,
        2,
      )}</pre>
  </details>

  {#if artifactContent && !textContent}
    <details
      class="mt-2 rounded-md border border-[var(--line)] bg-[var(--bg-soft)]"
    >
      <summary
        class="cursor-pointer px-4 py-2.5 text-micro text-[var(--fg-muted)] hover:text-[var(--fg)]"
        >Raw content JSON</summary
      >
      <pre
        class="overflow-auto px-4 pb-3 text-micro text-[var(--fg-muted)]">{JSON.stringify(
          artifactContent,
          null,
          2,
        )}</pre>
    </details>
  {/if}
{:else}
  <div class="mt-8 text-center text-meta text-[var(--fg-muted)]">
    Artifact not found.
  </div>
{/if}

<ConfirmModal
  open={confirmModal.open}
  title={confirmModal.action === "trash" ? "Move to trash" : "Archive artifact"}
  message={confirmModal.action === "trash"
    ? "This artifact will be moved to trash. You can restore it later."
    : "This artifact will be hidden from default views. You can unarchive it later."}
  confirmLabel={confirmModal.action === "trash" ? "Trash" : "Archive"}
  variant={confirmModal.action === "trash" ? "danger" : "warning"}
  busy={lifecycleBusy}
  onconfirm={handleConfirm}
  oncancel={() => (confirmModal = { open: false, action: "" })}
/>
