<script>
  import { browser } from "$app/environment";
  import { page } from "$app/stores";

  import ActorAvatar from "$lib/components/ActorAvatar.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import ContextMenuHost from "$lib/components/ContextMenuHost.svelte";
  import CopyButton from "$lib/components/CopyButton.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import MessageActions from "$lib/components/timeline/MessageActions.svelte";
  import { scrollAndHighlightTarget } from "$lib/deepLinkTargets";
  import { formatTimestamp } from "$lib/formatDate";
  import { resolveRefLink } from "$lib/refLinkModel.js";
  import {
    docCommentBodyFocus,
    docCommentBodyHover,
  } from "$lib/stores/docCommentBodyRailSync.js";

  let {
    message,
    threadId,
    actorName,
    onReply,
    onArchive = null,
    onTrash = null,
    onUnarchive = null,
    lifecycleBusy = false,
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
  let replyTo = $derived(message?.replyTo ?? null);

  function scrollToReplyParent() {
    if (!browser || !replyTo?.id) return;
    const target = document.getElementById(`message-${replyTo.id}`);
    scrollAndHighlightTarget(target);
  }

  function truncateReplyText(text, max = 120) {
    const t = String(text ?? "").trim();
    if (!t) return "message";
    return t.length > max ? `${t.slice(0, max)}…` : t;
  }

  const RECEIPT_DELIVERY_STAGE_INDEX = {
    requested: 0,
    claimed: 1,
    completed: 2,
    failed: 2,
  };

  function receiptValue(value) {
    return String(value ?? "").trim();
  }

  function receiptStageState(done, failed = false) {
    if (failed) return "failed";
    return done ? "done" : "pending";
  }

  function receiptStatusLabel(receipt) {
    const delivery = String(receipt?.delivery_status ?? "").trim();
    const notification = String(
      receipt?.notification_status ?? receipt?.status ?? "",
    ).trim();
    if (delivery === "failed") return "Failed";
    if (notification === "dismissed") return "Dismissed";
    if (notification === "read") return "Seen";
    if (delivery === "completed") return "Processed";
    if (delivery === "claimed") return "Bridge triggered";
    return "Queued";
  }

  function receiptDeliveryProgress(receipt) {
    const delivery = String(receipt?.delivery_status ?? "").trim();
    if (Object.hasOwn(RECEIPT_DELIVERY_STAGE_INDEX, delivery)) {
      return RECEIPT_DELIVERY_STAGE_INDEX[delivery];
    }
    return receiptValue(receipt?.created_at) ? 0 : -1;
  }

  function receiptLifecycleStages(receipt) {
    const delivery = String(receipt?.delivery_status ?? "").trim();
    const notification = String(
      receipt?.notification_status ?? receipt?.status ?? "",
    ).trim();
    const progress = receiptDeliveryProgress(receipt);
    const readDone =
      notification === "read" || receiptValue(receipt?.read_at) !== "";
    const dismissed =
      notification === "dismissed" ||
      receiptValue(receipt?.dismissed_at) !== "";

    return [
      {
        key: "requested",
        label: "Wake requested",
        status: receiptStageState(progress >= 0),
        timestamp: receiptValue(receipt?.created_at),
      },
      {
        key: "claimed",
        label: "Bridge claimed",
        status: receiptStageState(progress >= 1),
        timestamp: receiptValue(receipt?.claimed_at),
      },
      {
        key: "processed",
        label: delivery === "failed" ? "Processing failed" : "Processed",
        status: receiptStageState(progress >= 2, delivery === "failed"),
        timestamp: receiptValue(
          delivery === "failed" ? receipt?.failed_at : receipt?.completed_at,
        ),
        detail:
          delivery === "failed" ? receiptValue(receipt?.failure_reason) : "",
      },
      {
        key: "seen",
        label: dismissed ? "Dismissed" : "Seen",
        status: receiptStageState(readDone || dismissed),
        timestamp: receiptValue(
          dismissed ? receipt?.dismissed_at : receipt?.read_at,
        ),
      },
    ];
  }

  function receiptIconToneClass(receipt) {
    const delivery = String(receipt?.delivery_status ?? "").trim();
    if (delivery === "failed") return "text-danger-text";
    return "text-ok-text";
  }

  function receiptStageMarkerClass(stage) {
    if (stage.status === "failed") return "text-danger-text";
    if (stage.status === "done") return "text-ok-text";
    return "text-fg-subtle opacity-45";
  }

  function receiptStageDotClass(stage) {
    if (stage.status === "failed") return "bg-danger";
    if (stage.status === "done") return "bg-ok";
    return "bg-line-strong";
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
          iconToneClass: receiptIconToneClass(receipt),
          stages: receiptLifecycleStages(receipt),
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
      "group/msg relative rounded-md border bg-panel px-3 py-1.5 transition-colors",
      message.archived_at ? "opacity-60" : "",
      isAnchoredComment
        ? "border-line border-l-2 border-l-accent"
        : "border-line hover:border-line-strong",
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
        <!--
          Floating action toolbar: hover-revealed (or focus-revealed for
          keyboard) so the resting card stays clean. Anchored top-right and
          drawn over the panel background so it reads over message content,
          including grouped rows that have no header to host actions.
        -->
        <div
          class="msg-toolbar absolute right-1.5 top-1.5 z-10 flex items-center gap-0.5 rounded-md border border-line bg-panel/95 px-0.5 opacity-0 shadow-sm transition-opacity duration-150 focus-within:opacity-100 group-hover/msg:opacity-100"
        >
          <CopyButton
            value={messageHref}
            iconOnly
            icon="link"
            label="Copy message link"
            size="sm"
          />
          {@render visibleActions()}
        </div>

        {#if replyTo}
          <button
            type="button"
            class="mb-1 flex w-full min-w-0 cursor-pointer items-center gap-1.5 text-left text-micro text-fg-subtle transition-colors hover:text-fg"
            onclick={scrollToReplyParent}
            title={truncateReplyText(replyTo.text, 500)}
            aria-label={`Jump to message from ${actorName(replyTo.authorActorId)}: ${truncateReplyText(replyTo.text, 80)}`}
          >
            <svg
              class="h-3 w-3 shrink-0 text-fg-subtle transition-colors group-hover/msg:text-accent"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              viewBox="0 0 24 24"
              aria-hidden="true"
            >
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                d="M9 14 4 9l5-5M4 9h11a5 5 0 0 1 5 5v0a5 5 0 0 1-5 5h-3"
              />
            </svg>
            <span class="shrink-0 font-medium text-fg-muted">
              {actorName(replyTo.authorActorId)}
            </span>
            <span class="min-w-0 flex-1 truncate text-fg-subtle">
              {truncateReplyText(replyTo.text)}
            </span>
          </button>
        {/if}

        <div class="mb-1.5 flex min-w-0 w-full items-start gap-2 pr-16">
          <ActorAvatar
            label={actorLine}
            seed={String(message.actor_id ?? actorLine)}
            size="sm"
          />
          <div class="flex min-w-0 min-h-[1.25rem] flex-1 items-center gap-1">
            <span
              class="min-w-0 truncate text-meta font-semibold leading-tight text-fg"
              title={actorLine}
            >
              {actorDisplayLine}
            </span>
            <span class="shrink-0 text-micro leading-tight text-fg-muted"
              >{formatTimestamp(message.ts) || "—"}</span
            >
            {#if notificationReceipts.length > 0}
              <span class="ml-1 flex shrink-0 items-center gap-1 text-micro">
                {#each notificationReceipts as row (row.key)}
                  <button
                    type="button"
                    class="group/receipt relative inline-flex cursor-default items-center gap-1 rounded border-0 bg-transparent px-0.5 py-0 outline-none focus-visible:ring-1 focus-visible:ring-accent focus-visible:ring-offset-1 focus-visible:ring-offset-panel"
                    aria-label={`@${row.handle || "agent"} ${row.label}`}
                  >
                    <span
                      class={[
                        "flex shrink-0 items-center",
                        row.iconToneClass,
                      ].join(" ")}
                      aria-hidden="true"
                    >
                      {#each row.stages as stage, index (stage.key)}
                        <svg
                          class={[
                            "h-3 w-3 shrink-0",
                            index > 0 ? "-ml-1" : "",
                            receiptStageMarkerClass(stage),
                          ].join(" ")}
                          fill="none"
                          viewBox="0 0 16 16"
                          stroke="currentColor"
                          stroke-width="2"
                        >
                          {#if stage.status === "failed"}
                            <path
                              stroke-linecap="round"
                              d="M5 5l6 6M11 5l-6 6"
                            />
                          {:else}
                            <path
                              stroke-linecap="round"
                              stroke-linejoin="round"
                              d="M3.5 8.5 6.5 11.5 12.5 4.5"
                            />
                          {/if}
                        </svg>
                      {/each}
                    </span>
                    <span class="max-w-20 truncate text-fg-muted">
                      {row.label}
                    </span>
                    <span
                      class="pointer-events-none absolute left-0 top-full z-40 mt-2 w-64 rounded-md border border-line bg-panel p-3 text-left text-micro text-fg opacity-0 shadow-menu transition group-hover/receipt:opacity-100 group-focus/receipt:opacity-100"
                    >
                      <span class="mb-2 block font-medium text-fg">
                        @{row.handle || "agent"} lifecycle
                      </span>
                      <span class="block space-y-2">
                        {#each row.stages as stage (stage.key)}
                          <span class="grid grid-cols-[0.75rem_1fr] gap-2">
                            <span
                              class={[
                                "mt-1 h-2 w-2 rounded-full",
                                receiptStageDotClass(stage),
                              ].join(" ")}
                            ></span>
                            <span class="min-w-0">
                              <span class="block font-medium text-fg">
                                {stage.label}
                              </span>
                              <span class="block text-fg-muted">
                                {#if stage.timestamp}
                                  {formatTimestamp(stage.timestamp)}
                                {:else if stage.status === "pending"}
                                  Pending
                                {:else if stage.status === "failed"}
                                  Failed
                                {:else}
                                  Complete
                                {/if}
                              </span>
                              {#if stage.detail}
                                <span class="block text-danger-text">
                                  {stage.detail}
                                </span>
                              {/if}
                            </span>
                          </span>
                        {/each}
                      </span>
                    </span>
                  </button>
                {/each}
              </span>
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
      </article>
    </ContextMenuHost>
  {/snippet}
</MessageActions>

<style>
  /* Touch devices have no hover state: keep the action toolbar (reply,
     archive, trash, copy link) visible so it stays reachable, since there is
     no overflow-menu fallback for these primary row actions. */
  @media (hover: none) {
    .msg-toolbar {
      opacity: 1;
    }
  }
</style>
