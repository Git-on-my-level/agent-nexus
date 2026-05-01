<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import ArchiveButton from "$lib/components/ArchiveButton.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import TrashButton from "$lib/components/TrashButton.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import AttachmentPreview from "$lib/components/AttachmentPreview.svelte";
  import { coreClient } from "$lib/coreClient";
  import { kindLabel, kindDescription, kindColor } from "$lib/artifactKinds";
  import { formatTimestamp } from "$lib/formatDate";
  import { workspacePath } from "$lib/workspacePaths";
  import ProvenanceBadge from "$lib/components/ProvenanceBadge.svelte";
  import WorkspaceResourceTopRow from "$lib/components/WorkspaceResourceTopRow.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import { topicDetailPathFromRef } from "$lib/topicRouteUtils";
  import { parseRef } from "$lib/typedRefs";
  import {
    lookupActorDisplayName,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";

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
  let artifactTopicRef = $derived.by(() => {
    const candidates = [
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
  let textContent = $derived(
    artifactContentType.startsWith("text/") &&
      typeof artifactContent === "string"
      ? artifactContent
      : "",
  );
  let structuredContent = $derived(
    artifactContent &&
      typeof artifactContent === "object" &&
      !Array.isArray(artifactContent)
      ? artifactContent
      : null,
  );
  let cardArtifactContent = $derived(
    artifact?.kind === "card" && structuredContent ? structuredContent : null,
  );
  let hasTextContent = $derived(
    typeof textContent === "string" && textContent.length > 0,
  );
  let artifactRefHints = $derived(buildArtifactRefHints());

  let attachmentFileName = $derived(
    String(artifact?.original_filename ?? artifact?.summary ?? "").trim(),
  );

  function workspaceHref(pathname = "/") {
    return workspacePath(organizationSlug, workspaceSlug, pathname);
  }

  let artifactHeaderTitle = $derived(
    String(artifact?.summary ?? "").trim() ||
      `${kindLabel(artifact?.kind ?? "artifact")} artifact`,
  );

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

  let cardArtifactCardRef = $derived.by(() => {
    if (!artifact || String(artifact.kind ?? "").trim() !== "card") return "";
    const cardId =
      String(artifact.card_id ?? "").trim() ||
      firstTypedRefValue(artifact.refs, "card");
    return cardId ? `card:${cardId}` : "";
  });

  let cardArtifactRevisionRef = $derived.by(() => {
    if (!artifact || String(artifact.kind ?? "").trim() !== "card") return "";
    const revId =
      String(artifact.revision_id ?? "").trim() ||
      firstTypedRefValue(artifact.refs, "card_revision");
    return revId ? `card_revision:${revId}` : "";
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
    if (artifact.kind === "card") {
      const cardId = String(artifact.card_id ?? "").trim();
      if (cardId) hints[`card:${cardId}`] = "Card";
      const revId = String(artifact.revision_id ?? "").trim();
      if (revId) hints[`card_revision:${revId}`] = "Card revision";
    }
    if (artifact.thread_id)
      hints[`thread:${artifact.thread_id}`] = "Thread (timeline)";
    return hints;
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
    try {
      const contentResponse = await coreClient.getArtifactContent(targetId);
      artifactContent = contentResponse.content ?? null;
      artifactContentType = contentResponse.contentType ?? "";
    } catch (e) {
      artifactContent = null;
      artifactContentType = "";
      contentLoadError = `Content unavailable: ${e instanceof Error ? e.message : String(e)}`;
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

{#if loading}
  <nav
    class="mb-2 flex min-w-0 items-center gap-1.5 text-micro text-[var(--fg-muted)]"
    aria-label="Breadcrumb"
  >
    <a
      class="shrink-0 transition-colors hover:text-[var(--fg)]"
      href={workspaceHref("/artifacts")}>Artifacts</a
    >
    <span class="shrink-0 text-[var(--fg-subtle)]">/</span>
    <span class="min-w-0 truncate text-[var(--fg-muted)]">{artifactId}</span>
  </nav>
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
  <nav
    class="mb-2 flex min-w-0 items-center gap-1.5 text-micro text-[var(--fg-muted)]"
    aria-label="Breadcrumb"
  >
    <a
      class="shrink-0 transition-colors hover:text-[var(--fg)]"
      href={workspaceHref("/artifacts")}>Artifacts</a
    >
    <span class="shrink-0 text-[var(--fg-subtle)]">/</span>
    <span class="min-w-0 truncate text-[var(--fg-muted)]">{artifactId}</span>
  </nav>
  <div class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text">
    {loadError}
  </div>
{:else if artifact}
  {#if artifact?.trashed_at}
    <div
      class="trash-banner mb-4 flex flex-wrap items-center justify-between gap-3 rounded-md border border-danger/30 bg-danger-soft px-3 py-2 text-meta text-danger-text"
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
      class="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-md border border-warn/30 bg-warn-soft px-3 py-2 text-meta text-warn-text"
    >
      <p class="min-w-0 flex-1">
        This artifact was archived on {formatTimestamp(artifact.archived_at) ||
          "—"}{#if artifact.archived_by}{" by "}{actorName(
            artifact.archived_by,
          )}{/if}.
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

  {#snippet artifactDesktop()}
    <h1 class="min-w-0 text-subtitle font-semibold text-[var(--fg)]">
      {artifactHeaderTitle}
    </h1>
    <p class="mt-0.5 text-meta text-[var(--fg-muted)]">
      {kindDescription(artifact.kind)}
    </p>
  {/snippet}

  <WorkspaceResourceTopRow
    breadcrumbAriaLabel="Breadcrumb and artifact kind"
    desktopAriaLabel="Artifact details"
    desktop={artifactDesktop}
  >
    {#snippet breadcrumb()}
      <a
        class="shrink-0 transition-colors hover:text-[var(--fg)]"
        href={workspaceHref("/artifacts")}>Artifacts</a
      >
      <span class="shrink-0 text-[var(--fg-subtle)]">/</span>
      <div class="flex min-h-0 min-w-0 flex-1 items-center gap-1.5">
        <span
          class="min-w-0 shrink truncate text-[var(--fg-muted)]"
          aria-current="page">{artifact?.summary || artifactId}</span
        >
        <span
          class="shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium leading-none sm:py-0.5 sm:text-micro {kindColor(
            artifact.kind,
          )}">{kindLabel(artifact.kind)}</span
        >
      </div>
    {/snippet}
    {#snippet actions()}
      {#if !artifact.trashed_at}
        {#if !artifact.archived_at}
          <ArchiveButton
            busy={lifecycleBusy}
            size="md"
            onarchive={() => (confirmModal = { open: true, action: "archive" })}
          />
        {/if}
        <TrashButton
          busy={lifecycleBusy}
          size="md"
          ontrash={() => (confirmModal = { open: true, action: "trash" })}
        />
      {/if}
    {/snippet}
  </WorkspaceResourceTopRow>

  <section
    class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] p-4"
  >
    <div
      class="mt-2 flex flex-wrap items-center gap-2 text-micro max-md:mt-1.5"
    >
      <span
        class="max-md:hidden rounded px-1.5 py-0.5 font-medium {kindColor(
          artifact.kind,
        )}">{kindLabel(artifact.kind)}</span
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
    {#if cardArtifactCardRef || cardArtifactRevisionRef}
      <div
        class="mt-1.5 flex flex-wrap items-center gap-2 text-micro text-[var(--fg-muted)]"
      >
        {#if cardArtifactCardRef}
          <RefLink
            humanize
            labelHints={artifactRefHints}
            refValue={cardArtifactCardRef}
            showRaw
            threadId={artifact.thread_id}
          />
        {/if}
        {#if cardArtifactRevisionRef}
          <RefLink
            humanize
            labelHints={artifactRefHints}
            refValue={cardArtifactRevisionRef}
            showRaw
            threadId={artifact.thread_id}
          />
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

  {#if !contentLoadError && artifact.kind !== "doc" && artifact.kind !== "card" && artifact.kind !== "attachment" && !hasTextContent}
    <div
      class="mt-3 rounded-md bg-warn-soft px-3 py-2 text-micro text-warn-text"
    >
      No structured view available for this artifact.
    </div>
  {/if}

  {#if !contentLoadError && artifact.kind === "card" && !cardArtifactContent && !hasTextContent}
    <div
      class="mt-3 rounded-md bg-warn-soft px-3 py-2 text-micro text-warn-text"
    >
      Card revision content unavailable for this artifact.
    </div>
  {/if}

  {#if cardArtifactContent}
    <div
      class="mt-4 rounded-md border border-[var(--line)] bg-[var(--bg-soft)]"
    >
      <div class="border-b border-[var(--line)] px-4 py-2.5">
        <h2 class="text-meta font-medium text-[var(--fg)]">
          Card revision content
        </h2>
      </div>
      <div class="px-4 py-3 text-meta">
        <div class="flex flex-wrap gap-2 text-micro text-[var(--fg-muted)]">
          {#if cardArtifactCardRef}
            <RefLink
              humanize
              labelHints={artifactRefHints}
              refValue={cardArtifactCardRef}
              showRaw
              threadId={artifact.thread_id}
            />
          {/if}
          {#if cardArtifactRevisionRef}
            <RefLink
              humanize
              labelHints={artifactRefHints}
              refValue={cardArtifactRevisionRef}
              showRaw
              threadId={artifact.thread_id}
            />
          {/if}
        </div>
        {#if cardArtifactContent.title}
          <p class="mt-3 text-base font-medium text-[var(--fg)]">
            {cardArtifactContent.title}
          </p>
        {/if}
        {#if cardArtifactContent.summary}
          <div class="mt-2 leading-relaxed text-[var(--fg)]">
            <MarkdownRenderer source={cardArtifactContent.summary} />
          </div>
        {/if}
        {#if Array.isArray(cardArtifactContent.definition_of_done) && cardArtifactContent.definition_of_done.length > 0}
          <div class="mt-3">
            <p class="text-micro font-medium text-[var(--fg-muted)]">
              Definition of done
            </p>
            <ul class="mt-1 space-y-0.5 text-[var(--fg-muted)]">
              {#each cardArtifactContent.definition_of_done as item}
                <li class="flex items-start gap-2">
                  <span
                    class="mt-1.5 h-1 w-1 shrink-0 rounded-full bg-[var(--fg-muted)]"
                  ></span>
                  <span>{item}</span>
                </li>
              {/each}
            </ul>
          </div>
        {/if}
      </div>
    </div>
  {/if}

  {#if !contentLoadError && artifact.kind === "attachment" && artifactContent != null}
    <div
      class="mt-4 rounded-md border border-[var(--line)] bg-[var(--bg-soft)]"
    >
      <div
        class="flex items-center justify-between border-b border-[var(--line)] px-4 py-2.5"
      >
        <h2 class="text-meta font-medium text-[var(--fg)]">Attachment</h2>
        <span class="text-micro text-[var(--fg-muted)]"
          >{artifactContentType}</span
        >
      </div>
      <div class="px-4 py-3">
        <AttachmentPreview
          content={artifactContent}
          contentType={artifactContentType}
          fileName={attachmentFileName}
        />
      </div>
    </div>
  {/if}

  {#if hasTextContent && artifact.kind !== "attachment"}
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

  {#if artifactContent && !textContent && artifact.kind !== "attachment"}
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
