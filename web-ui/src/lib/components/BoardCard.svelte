<script>
  import {
    actorRegistry,
    lookupActorDisplayName,
    principalRegistry,
  } from "$lib/actorSession";
  import ActorAvatar from "$lib/components/ActorAvatar.svelte";
  import ContextMenuHost from "$lib/components/ContextMenuHost.svelte";
  import Icon from "$lib/components/Icon.svelte";
  import { copyText } from "$lib/clipboard.js";
  import {
    boardCardHeaderTitle,
    boardCardStableId,
    boardCardTimelineMessageCount,
  } from "$lib/boardUtils";
  import {
    cardResolutionLabel,
    cardResolutionTone,
    isOverdue,
  } from "$lib/cardDisplayUtils";
  import { formatTimestamp } from "$lib/formatDate";

  /**
   * @typedef {object} BoardCardProps
   * @property {object} cardItem
   * @property {string} [boardId]
   * @property {string} [cardHref]
   * @property {() => void} [onclick]
   * @property {(e: PointerEvent) => void} [ondragstart]
   * @property {(e: KeyboardEvent) => void} [onboardkeydown]
   * @property {boolean} [dragging]
   * @property {boolean} [dropBefore]
   * @property {boolean} [dropAfter]
   * @property {import("svelte").Snippet} [footer]
   */

  /** @type {BoardCardProps} */
  let {
    cardItem,
    boardId = "",
    cardHref = "",
    onclick = () => {},
    ondragstart = undefined,
    onboardkeydown = undefined,
    dragging = false,
    dropBefore = false,
    dropAfter = false,
    footer,
  } = $props();

  const membership = $derived(cardItem?.membership);
  const backing = $derived(cardItem?.backing);
  const derived = $derived(cardItem?.derived);
  const thread = $derived(backing?.thread);

  const cardRowId = $derived(boardCardStableId(membership));

  const rowStatus = $derived(boardCardRowStatus(membership, thread));
  const headerTitle = $derived(boardCardHeaderTitle(membership, thread));
  const timelineMessageCount = $derived(
    boardCardTimelineMessageCount(cardItem),
  );
  const cardResolution = $derived(String(membership?.resolution ?? "").trim());
  const summaryText = $derived(String(membership?.summary ?? "").trim());
  const cardDueAt = $derived(String(membership?.due_at ?? "").trim());
  const cardUpdatedRelative = $derived(
    formatTimestamp(String(membership?.updated_at ?? "").trim()),
  );
  const assigneeRefs = $derived(
    Array.isArray(membership?.assignee_refs) ? membership.assignee_refs : [],
  );

  const dueOverdue = $derived(cardDueAt ? isOverdue(cardDueAt) : false);

  const contextMenuItems = $derived(
    cardHref
      ? [
          {
            key: "copy-link",
            label: "Copy link",
            onSelect: () => void copyText(cardHref),
          },
        ]
      : [],
  );

  const assigneeNames = $derived.by(() => {
    const actors = $actorRegistry;
    const principals = $principalRegistry;
    return assigneeRefs.map((ref) => {
      const id = String(ref ?? "")
        .replace(/^actor:/, "")
        .trim();
      return {
        id,
        name: lookupActorDisplayName(id, actors, principals),
      };
    });
  });

  const showResolutionChip = $derived(
    cardResolution === "done" ||
      cardResolution === "completed" ||
      cardResolution === "superseded",
  );

  const showSummary = $derived(
    Boolean(summaryText) && summaryText !== headerTitle,
  );

  function threadStatusColor(status) {
    switch (status) {
      case "done":
        return "text-fg";
      case "canceled":
        return "text-fg-muted";
      case "paused":
        return "text-warn-text";
      default:
        return "text-fg";
    }
  }

  function getThreadStatus(t) {
    if (!t) return "unknown";
    const life = String(t.state ?? "").trim();
    if (life === "archived") return "paused";
    if (life === "trashed") return "canceled";
    return "active";
  }

  function boardCardRowStatus(m, t) {
    const resolution = String(m?.resolution ?? "").trim();
    if (resolution === "done" || resolution === "completed") return "done";
    if (resolution === "superseded") return "paused";
    if (t) return getThreadStatus(t);
    if (String(m?.column_key ?? "").trim() === "done") return "done";
    return "active";
  }

  const titleColorClass = $derived(threadStatusColor(rowStatus));

  /** @param {PointerEvent} e */
  function handleCardPointerDown(e) {
    if (e.button !== 0 || !ondragstart) return;
    ondragstart(e);
  }

  /** @param {KeyboardEvent} e */
  function handleCardKeydown(e) {
    if (onboardkeydown) {
      onboardkeydown(e);
      if (e.defaultPrevented) return;
    }
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      onclick();
    }
  }
</script>

<div
  id={`card-${cardRowId}`}
  data-board-card-slot
  data-card-id={cardRowId}
  data-board-id={boardId || undefined}
  class="relative {dropBefore ? 'board-card-drop-before' : ''} {dropAfter
    ? 'board-card-drop-after'
    : ''}"
>
  {#if dropBefore}
    <div
      class="pointer-events-none absolute -top-1 left-0 right-0 z-10 h-0.5 rounded-full bg-accent"
      aria-hidden="true"
    ></div>
  {/if}
  {#if dropAfter}
    <div
      class="pointer-events-none absolute -bottom-1 left-0 right-0 z-10 h-0.5 rounded-full bg-accent"
      aria-hidden="true"
    ></div>
  {/if}
  <ContextMenuHost items={contextMenuItems} disabled={!cardHref}>
    <div
      class="group overflow-hidden rounded-md border border-line bg-panel transition-all hover:border-line-strong hover:shadow-sm {dragging
        ? 'opacity-40'
        : ''}"
    >
      <div
        aria-label={`Manage ${headerTitle}`}
        class="flex cursor-pointer select-none"
        {onclick}
        onkeydown={handleCardKeydown}
        onpointerdown={handleCardPointerDown}
        role="button"
        tabindex="0"
      >
        <div
          class="min-w-0 flex-1 px-2.5 py-2 transition-colors hover:bg-line-subtle/50"
        >
          <div class="flex items-start gap-2">
            <div class="min-w-0 flex-1">
              <span
                class="block truncate text-meta font-medium leading-snug {titleColorClass}"
              >
                {headerTitle}
              </span>

              {#if showSummary}
                <p class="mt-0.5 line-clamp-2 text-micro text-fg-muted">
                  {summaryText}
                </p>
              {/if}

              <div class="mt-1.5 flex flex-wrap items-center gap-1.5">
                {#if showResolutionChip}
                  <span
                    class="rounded px-1 py-0.5 text-micro font-medium {cardResolutionTone(
                      cardResolution,
                    )}"
                  >
                    {cardResolutionLabel(cardResolution)}
                  </span>
                {/if}

                {#if cardDueAt}
                  <span
                    class="inline-flex items-center gap-0.5 rounded px-1 py-0.5 text-micro {dueOverdue
                      ? 'bg-danger-soft text-danger-text'
                      : 'bg-line text-fg-muted'}"
                    title={`Due ${formatTimestamp(cardDueAt)}`}
                  >
                    <Icon name="calendar" class="h-3 w-3" />
                    {formatTimestamp(cardDueAt) || "—"}
                  </span>
                {/if}

                {#if timelineMessageCount > 0}
                  <span
                    class="inline-flex items-center gap-0.5 text-micro text-fg-muted"
                    title={`${timelineMessageCount} comment${timelineMessageCount === 1 ? "" : "s"}`}
                  >
                    <Icon name="comment" class="h-3 w-3" />
                    <span class="tabular-nums font-medium"
                      >{timelineMessageCount > 99
                        ? "99+"
                        : timelineMessageCount}</span
                    >
                  </span>
                {/if}

                {#if cardUpdatedRelative}
                  <span
                    class="text-micro text-fg-subtle tabular-nums"
                    title="Last updated"
                  >
                    {cardUpdatedRelative}
                  </span>
                {/if}
              </div>
            </div>

            {#if assigneeNames.length > 0}
              <div class="flex shrink-0 -space-x-1.5 pt-0.5">
                {#each assigneeNames.slice(0, 3) as assignee (assignee.id)}
                  <ActorAvatar
                    label={assignee.name}
                    seed={assignee.id}
                    size="xs"
                    class="ring-1 ring-panel"
                  />
                {/each}
                {#if assigneeNames.length > 3}
                  <span
                    class="inline-flex h-5 w-5 items-center justify-center rounded-full bg-line text-[10px] font-medium text-fg-muted ring-1 ring-panel"
                  >
                    +{assigneeNames.length - 3}
                  </span>
                {/if}
              </div>
            {/if}
          </div>
        </div>
      </div>
    </div>
  </ContextMenuHost>
  {@render footer?.()}
</div>

<style>
  .board-card-drop-before {
    padding-top: 0.25rem;
  }

  .board-card-drop-after {
    padding-bottom: 0.25rem;
  }
</style>
