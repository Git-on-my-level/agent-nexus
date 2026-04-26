<script>
  import ArchiveButton from "$lib/components/ArchiveButton.svelte";
  import Self from "$lib/components/timeline/MessageItem.svelte";
  import ContextMenuHost from "$lib/components/ContextMenuHost.svelte";
  import CopyButton from "$lib/components/CopyButton.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import { formatTimestamp } from "$lib/formatDate";
  import { docCommentBodyHover } from "$lib/stores/docCommentBodyRailSync.js";

  const MAX_REPLY_DEPTH = 48;

  let {
    message,
    threadId,
    actorName,
    onReply,
    onArchive = null,
    onTrash = null,
    onUnarchive = null,
    lifecycleBusy = false,
    depth = 0,
    /**
     * Visual variant for the archive lifecycle button. "resolve" relabels
     * Archive→Resolve and Unarchive→Reopen (same `archived_at` lifecycle)
     * for surfaces like document text comments where the engineering verb
     * is wrong for the domain. See ArchiveButton for details.
     */
    archiveLabelKind = "archive",
    /**
     * Optional override for the rendered anchor status. Lets the parent
     * surface (e.g. the doc page) downgrade an anchored comment to "stale"
     * when its quoted text no longer occurs in the head revision, without
     * mutating the original event. Falls back to `documentComment.anchor_status`.
     */
    liveAnchorStatus = "",
    /**
     * When set (e.g. from `MessagesTab`), used for every node including nested
     * replies so `liveAnchorStatus` is derived consistently.
     */
    getLiveAnchorStatusForMessage = null,
  } = $props();

  let resolvedLiveAnchorStatus = $derived(
    typeof getLiveAnchorStatusForMessage === "function"
      ? (getLiveAnchorStatusForMessage(message) ?? liveAnchorStatus)
      : liveAnchorStatus,
  );

  let docComment = $derived(
    message?.documentComment &&
      typeof message.documentComment === "object" &&
      String(message.documentComment.selected_text ?? "").trim()
      ? message.documentComment
      : null,
  );

  let isBodyDocCommentHover = $derived(
    Boolean(docComment) &&
      Array.isArray($docCommentBodyHover) &&
      $docCommentBodyHover.includes(String(message.id)),
  );

  /**
   * Long quoted text in a narrow rail must wrap on word boundaries (and
   * fall back to anywhere for unbreakable strings like long URLs/hashes)
   * so each rail card stays scannable. Without this, a 60-char word
   * forces ~60 single-character lines on a 280px rail.
   */
  let quoteIsLong = $derived(
    Boolean(docComment) && String(docComment?.selected_text ?? "").length > 220,
  );
  let quoteExpanded = $state(false);

  let anchorStatus = $derived.by(() => {
    const live = String(resolvedLiveAnchorStatus ?? "").trim();
    if (live) return live;
    return String(docComment?.anchor_status ?? "").trim() || "current";
  });

  // Only surface a status chip when the anchor is *not* in the steady-state
  // "current" case — Google Docs–style: silence by default, speak up only
  // when something needs attention (older revision, ambiguous quote, or
  // the quote no longer exists in the head).
  let anchorStatusChip = $derived.by(() => {
    if (!docComment) return null;
    if (anchorStatus === "stale") {
      return {
        label: "Text removed",
        tone: "warn",
        title: "The quoted text is no longer present in this revision.",
      };
    }
    if (anchorStatus === "historical") {
      return {
        label: "On older revision",
        tone: "muted",
        title: "Comment is anchored to a previous revision of this document.",
      };
    }
    if (anchorStatus === "quote_only") {
      return {
        label: "Quote only",
        tone: "muted",
        title:
          "Exact position not unique in this revision — comment is anchored by quote.",
      };
    }
    return null;
  });

  let chipToneClass = $derived.by(() => {
    const tone = anchorStatusChip?.tone;
    if (tone === "warn") {
      return "bg-warn-soft text-warn-text";
    }
    return "bg-[var(--line-subtle)] text-[var(--fg-muted)]";
  });

  let isAnchoredComment = $derived(Boolean(docComment));

  // Faint accent left-border on the whole card visually links the comment
  // to the document selection it is anchored to. Replies (`depth > 0`)
  // inherit the conversation context from the parent and don't repeat
  // the accent — keeps reply trees readable.
  let articleClasses = $derived(
    [
      "rounded-md border bg-[var(--panel)] px-3 py-2",
      depth > 0 ? "bg-[var(--bg-soft)]" : "",
      message.archived_at ? "opacity-60" : "",
      isAnchoredComment && depth === 0
        ? "border-[var(--line)] border-l-2 border-l-[var(--accent)]"
        : "border-[var(--line)]",
      isBodyDocCommentHover
        ? "ring-1 ring-[var(--accent)] ring-offset-1 ring-offset-[var(--bg)]"
        : "",
    ]
      .filter(Boolean)
      .join(" "),
  );

  let quoteIsStale = $derived(anchorStatus === "stale");

  let actorLine = $derived(actorName(message.actor_id));

  let contextMenuItems = $derived(
    onTrash && !message.trashed_at
      ? [
          {
            key: "trash",
            label: "Move to trash",
            danger: true,
            disabled: Boolean(lifecycleBusy),
            onSelect: () => onTrash?.(message.id),
          },
        ]
      : [],
  );
</script>

<ContextMenuHost
  disabled={!onTrash || Boolean(message.trashed_at)}
  items={contextMenuItems}
>
  <article
    class={articleClasses}
    id={`message-${message.id}`}
    data-anchored-comment={isAnchoredComment ? "1" : undefined}
    data-anchor-document-id={docComment?.document_id || undefined}
    data-anchor-revision-id={docComment?.revision_id || undefined}
    onmouseenter={docComment
      ? () => {
          docCommentBodyHover.set([String(message.id)]);
        }
      : undefined}
    onmouseleave={docComment
      ? () => {
          docCommentBodyHover.set(null);
        }
      : undefined}
  >
    <div class="mb-2 flex min-w-0 w-full items-center gap-1.5">
      <div class="flex min-w-0 min-h-[1.25rem] flex-1 items-center gap-0.5">
        <span
          class="min-w-0 max-w-full truncate font-mono text-[0.65rem] leading-tight text-[var(--fg-muted)]"
          title={actorLine}
        >
          {actorLine}
        </span>
        <CopyButton
          value={actorLine}
          iconOnly
          label="Copy author id"
          size="sm"
        />
        <span
          class="shrink-0 text-[0.65rem] leading-tight text-[var(--fg-muted)]"
          >· {formatTimestamp(message.ts) || "—"}</span
        >
      </div>
      <div class="flex shrink-0 items-center gap-0.5">
        {#if !message.trashed_at && ((!message.archived_at && onArchive) || (message.archived_at && onUnarchive))}
          <ArchiveButton
            archived={Boolean(message.archived_at)}
            busy={lifecycleBusy}
            kind={archiveLabelKind}
            onarchive={() => onArchive?.(message.id)}
            onunarchive={() => onUnarchive?.(message.id)}
          />
        {/if}
        {#if !message.archived_at && !message.trashed_at}
          <button
            class="inline-flex h-7 w-7 shrink-0 cursor-pointer items-center justify-center rounded text-[var(--fg-muted)] transition-colors hover:bg-[var(--bg-soft)] hover:text-[var(--fg)]"
            onclick={() => onReply(message.id)}
            type="button"
            title="Reply"
            aria-label="Reply"
          >
            <svg
              class="h-3.5 w-3.5"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              stroke-width="2"
              aria-hidden="true"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M3 10h10a5 5 0 0 1 0 10M3 10l4-4M3 10l4 4"
              />
            </svg>
          </button>
        {/if}
      </div>
    </div>

    <div class="min-w-0">
      {#if docComment}
        <!--
          Quote-led layout: the blockquote itself communicates "this is anchored
          to selected text"; we drop the redundant "Selected in document" label
          and the dense engineering metadata line. Status is silent in the
          steady "current" case and speaks up only via a small chip when the
          anchor needs operator attention (older revision, quote-only, or
          stale).

          `whitespace-pre-wrap` preserves the line breaks the operator captured
          (e.g. selected three list items); `[overflow-wrap:anywhere]` keeps
          long unbreakable strings from blowing out the narrow rail width
          rather than wrapping. The clamp + Show more affordance keeps very
          long quotes from dominating the card.
        -->
        <blockquote
          class={[
            "mb-2 border-l-2 pl-2 pr-1 text-meta italic whitespace-pre-wrap [overflow-wrap:anywhere] break-words",
            quoteIsStale
              ? "border-warn text-[var(--fg-muted)] line-through"
              : "border-[var(--accent)] text-[var(--fg)]",
            quoteIsLong && !quoteExpanded ? "line-clamp-4" : "",
          ].join(" ")}
          title={docComment.selected_text}
        >
          {docComment.selected_text}
        </blockquote>
        {#if quoteIsLong}
          <button
            type="button"
            class="mb-2 -mt-1 cursor-pointer text-micro text-accent-text hover:underline"
            onclick={() => (quoteExpanded = !quoteExpanded)}
          >
            {quoteExpanded ? "Show less" : "Show more"}
          </button>
        {/if}
        {#if anchorStatusChip}
          <p
            class={[
              "mb-2 inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-micro",
              chipToneClass,
            ].join(" ")}
            title={anchorStatusChip.title}
          >
            {anchorStatusChip.label}
          </p>
        {/if}
      {/if}
      <div class="text-meta text-[var(--fg)] [overflow-wrap:anywhere]">
        <MarkdownRenderer
          source={message.messageText || message.summary || "Untitled message"}
          class="text-meta text-[var(--fg)]"
        />
      </div>
    </div>

    {#if message.displayRefs.length > 0}
      <div class="mt-2 flex flex-wrap gap-1.5 text-micro">
        {#each message.displayRefs as refValue (refValue)}
          <RefLink {refValue} {threadId} humanize showRaw />
        {/each}
      </div>
    {/if}

    {#if message.children.length > 0 && depth < MAX_REPLY_DEPTH}
      <div
        class="mt-2 -mx-3 space-y-1.5 border-l border-[var(--line)] pl-2 sm:pl-2.5"
      >
        {#each message.children as child (child.id)}
          <Self
            message={child}
            {threadId}
            {actorName}
            {onReply}
            {onArchive}
            {onTrash}
            {onUnarchive}
            {lifecycleBusy}
            {archiveLabelKind}
            {getLiveAnchorStatusForMessage}
            depth={depth + 1}
          />
        {/each}
      </div>
    {/if}
  </article>
</ContextMenuHost>
