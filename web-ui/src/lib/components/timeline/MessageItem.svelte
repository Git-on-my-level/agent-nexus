<script>
  import { browser } from "$app/environment";
  import { page } from "$app/stores";

  import Self from "$lib/components/timeline/MessageItem.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import ContextMenuHost from "$lib/components/ContextMenuHost.svelte";
  import CopyButton from "$lib/components/CopyButton.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import MessageActions from "$lib/components/timeline/MessageActions.svelte";
  import { formatTimestamp } from "$lib/formatDate";
  import { resolveRefLink } from "$lib/refLinkModel.js";
  import {
    docCommentBodyFocus,
    docCommentBodyHover,
  } from "$lib/stores/docCommentBodyRailSync.js";

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
    artifactRoutesById = {},
    eventRoutesById = {},
    /**
     * When set (e.g. from `MessagesTab`), used for every node including nested
     * replies so `liveAnchorStatus` is derived consistently.
     */
    getLiveAnchorStatusForMessage = null,
    organizationSlug = "",
    workspaceSlug = "",
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
  let isBodyDocCommentFocus = $derived(
    Boolean(docComment) &&
      Array.isArray($docCommentBodyFocus) &&
      $docCommentBodyFocus.includes(String(message.id)),
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
    return "bg-line-subtle text-fg-muted";
  });

  let isAnchoredComment = $derived(Boolean(docComment));

  function receiptStatusLabel(receipt) {
    const delivery = String(receipt?.delivery_status ?? "").trim();
    const notification = String(
      receipt?.notification_status ?? receipt?.status ?? "",
    ).trim();
    if (delivery === "failed") return "Failed";
    if (delivery === "completed") return "Processed";
    if (delivery === "claimed") return "Bridge triggered";
    if (notification === "dismissed") return "Dismissed";
    return "Queued";
  }

  function receiptToneClass(receipt) {
    const delivery = String(receipt?.delivery_status ?? "").trim();
    if (delivery === "failed") return "bg-danger-soft text-danger-text";
    if (delivery === "claimed" || delivery === "completed") {
      return "bg-ok-soft text-ok-text";
    }
    return "bg-line-subtle text-fg-muted";
  }

  function receiptTitle(receipt) {
    const parts = [];
    const delivery = String(receipt?.delivery_status ?? "").trim();
    const notification = String(
      receipt?.notification_status ?? receipt?.status ?? "",
    ).trim();
    if (delivery) parts.push(`delivery: ${delivery}`);
    if (notification) parts.push(`notification: ${notification}`);
    if (receipt?.claimed_at) parts.push(`claimed: ${receipt.claimed_at}`);
    if (receipt?.completed_at) parts.push(`completed: ${receipt.completed_at}`);
    if (receipt?.failed_at) parts.push(`failed: ${receipt.failed_at}`);
    if (receipt?.failure_reason) parts.push(String(receipt.failure_reason));
    return parts.join(" | ");
  }

  let notificationReceipts = $derived.by(() => {
    const receipts = Array.isArray(message?.notificationReceipts)
      ? message.notificationReceipts
      : [];
    return receipts
      .map((receipt) => {
        const handle = String(receipt?.target_handle ?? "").trim();
        const actorId = String(receipt?.target_actor_id ?? "").trim();
        const wakeupId = String(receipt?.wakeup_id ?? "").trim();
        return {
          receipt,
          key: wakeupId || `${actorId}:${handle}`,
          handle,
          label: receiptStatusLabel(receipt),
          toneClass: receiptToneClass(receipt),
          title: receiptTitle(receipt),
        };
      })
      .filter((row) => row.handle || row.key);
  });

  // Faint accent left-border on the whole card visually links the comment
  // to the document selection it is anchored to. Replies (`depth > 0`)
  // inherit the conversation context from the parent and don't repeat
  // the accent — keeps reply trees readable.
  let articleClasses = $derived(
    [
      "rounded-md border bg-panel px-3 py-1.5",
      depth > 0 ? "bg-bg-soft" : "",
      message.archived_at ? "opacity-60" : "",
      isAnchoredComment && depth === 0
        ? "border-line border-l-2 border-l-accent"
        : "border-line",
      isBodyDocCommentHover || isBodyDocCommentFocus
        ? "ring-1 ring-accent ring-offset-1 ring-offset-bg"
        : "",
    ]
      .filter(Boolean)
      .join(" "),
  );

  let quoteIsStale = $derived(anchorStatus === "stale");

  let actorLine = $derived(actorName(message.actor_id));
  let actorDisplayLine = $derived.by(() => {
    const name = actorLine;
    if (name.length <= 24 || name.includes(" ")) return name;
    const dotIdx = name.indexOf(".");
    if (dotIdx > 0 && dotIdx < name.length - 1) {
      const prefix = name.slice(0, dotIdx);
      const suffix = name.slice(dotIdx + 1, dotIdx + 9);
      return `${prefix}.${suffix}…`;
    }
    return `${name.slice(0, 20)}…`;
  });

  let messageHref = $derived.by(() => {
    const org = String(
      organizationSlug || $page.params.organization || "",
    ).trim();
    const ws = String(workspaceSlug || $page.params.workspace || "").trim();
    const id = String(message?.id ?? "").trim();
    if (!org || !ws || !id) return "";
    const href = resolveRefLink(`event:${id}`, {
      eventRoutesById,
      threadId,
      workspaceSlug: ws,
      organizationSlug: org,
    }).href;
    if (!href || !browser) return href;
    return new URL(href, window.location.origin).toString();
  });
</script>

<MessageActions
  {message}
  {onReply}
  {onArchive}
  {onTrash}
  {onUnarchive}
  {lifecycleBusy}
  {archiveLabelKind}
>
  {#snippet children(contextMenuItems, visibleActions)}
    <ContextMenuHost
      disabled={contextMenuItems.length === 0}
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
        <div class="mb-1.5 flex min-w-0 w-full items-center gap-1.5">
          <div class="flex min-w-0 min-h-[1.25rem] flex-1 items-center gap-0.5">
            <span
              class="min-w-0 max-w-full truncate font-mono text-[0.65rem] leading-tight text-fg-muted"
              title={actorLine}
            >
              {actorDisplayLine}
            </span>
            <CopyButton
              value={messageHref}
              iconOnly
              icon="link"
              label="Copy message link"
              size="sm"
            />
            <span class="shrink-0 text-[0.65rem] leading-tight text-fg-muted"
              >· {formatTimestamp(message.ts) || "—"}</span
            >
          </div>
          <div class="flex shrink-0 items-center gap-0.5">
            {@render visibleActions()}
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
                  ? "border-warn text-fg-muted line-through"
                  : "border-accent text-fg",
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
          <div
            class="card-content-block text-meta text-fg [overflow-wrap:anywhere]"
          >
            <MarkdownRenderer
              source={message.messageText ||
                message.summary ||
                "Untitled message"}
              class="markdown-rendered--bubble text-meta text-fg"
            />
          </div>
        </div>

        {#if message.displayRefs.length > 0}
          <div class="mt-2 flex min-w-0 flex-wrap gap-1.5 text-micro">
            {#each message.displayRefs as refValue (refValue)}
              <RefLink
                variant="compact"
                {refValue}
                {threadId}
                humanize
                showRaw
                {artifactRoutesById}
                {eventRoutesById}
              />
            {/each}
          </div>
        {/if}

        {#if notificationReceipts.length > 0}
          <div class="mt-2 flex min-w-0 flex-wrap gap-1.5 text-micro">
            {#each notificationReceipts as row (row.key)}
              <span
                class={[
                  "inline-flex min-w-0 max-w-full items-center gap-1 rounded px-1.5 py-0.5",
                  row.toneClass,
                ].join(" ")}
                title={row.title}
              >
                <span class="truncate">@{row.handle || "agent"}</span>
                <span class="shrink-0">{row.label}</span>
              </span>
            {/each}
          </div>
        {/if}

        {#if message.children.length > 0 && depth < MAX_REPLY_DEPTH}
          <div
            class="mt-2 -mx-3 space-y-1.5 border-l border-line pl-2 sm:pl-2.5"
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
                {artifactRoutesById}
                {eventRoutesById}
                {getLiveAnchorStatusForMessage}
                {organizationSlug}
                {workspaceSlug}
                depth={depth + 1}
              />
            {/each}
          </div>
        {/if}
      </article>
    </ContextMenuHost>
  {/snippet}
</MessageActions>
