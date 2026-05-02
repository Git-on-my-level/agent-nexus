<script>
  import {
    actorRegistry,
    lookupActorDisplayName,
    principalRegistry,
  } from "$lib/actorSession";
  import {
    boardCardHeaderTitle,
    boardCardStableId,
    freshnessStatusLabel,
    freshnessStatusTone,
  } from "$lib/boardUtils";
  import {
    cardResolutionLabel,
    cardResolutionTone,
  } from "$lib/cardDisplayUtils";
  import { formatTimestamp } from "$lib/formatDate";

  /**
   * @typedef {object} BoardCardProps
   * @property {object} cardItem
   * @property {string} [boardId]
   * @property {() => void} [onclick]
   * @property {import("svelte").Snippet} [footer]
   */

  /** @type {BoardCardProps} */
  let { cardItem, boardId = "", onclick = () => {}, footer } = $props();

  const membership = $derived(cardItem?.membership);
  const backing = $derived(cardItem?.backing);
  const derived = $derived(cardItem?.derived);
  const thread = $derived(backing?.thread);

  const cardRowId = $derived(boardCardStableId(membership));

  const rowStatus = $derived(boardCardRowStatus(membership, thread));
  const headerTitle = $derived(boardCardHeaderTitle(membership, thread));
  const cardFreshness = $derived(derived?.freshness);
  const cardResolution = $derived(String(membership?.resolution ?? "").trim());
  const summaryText = $derived(String(membership?.summary ?? "").trim());
  const cardDueAt = $derived(String(membership?.due_at ?? "").trim());
  const assigneeRefs = $derived(
    Array.isArray(membership?.assignee_refs) ? membership.assignee_refs : [],
  );

  const dueOverdue = $derived.by(() => {
    if (!cardDueAt) return false;
    const d = new Date(cardDueAt);
    if (isNaN(d.getTime())) return false;
    return d.getTime() < Date.now();
  });

  const assigneeNames = $derived.by(() => {
    const actors = $actorRegistry;
    const principals = $principalRegistry;
    return assigneeRefs.map((ref) => {
      const id = String(ref ?? "")
        .replace(/^actor:/, "")
        .trim();
      return lookupActorDisplayName(id, actors, principals);
    });
  });

  const assigneeVisible = $derived(assigneeNames.slice(0, 2));
  const assigneeMore = $derived(
    assigneeNames.length > 2 ? assigneeNames.length - 2 : 0,
  );

  const statusDotClass = $derived(threadStatusDotClass(rowStatus));
  const titleColorClass = $derived(threadStatusColor(rowStatus));

  function threadStatusDotClass(status) {
    switch (status) {
      case "done":
        return "bg-ok-text";
      case "canceled":
        return "bg-fg-muted";
      case "paused":
        return "bg-warn-text";
      default:
        return "bg-accent-solid";
    }
  }

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

  const showSummary = $derived(
    Boolean(summaryText) && summaryText !== headerTitle,
  );

  /** @param {KeyboardEvent} e */
  function handleCardKeydown(e) {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      onclick();
    }
  }
</script>

<div
  id={`card-${cardRowId}`}
  data-board-id={boardId || undefined}
  class="group overflow-hidden rounded-md border border-line bg-panel transition-colors hover:border-line-strong"
>
  <div
    aria-label={`Manage ${headerTitle}`}
    class="cursor-pointer px-2.5 py-2 transition-colors hover:bg-line-subtle"
    {onclick}
    onkeydown={handleCardKeydown}
    role="button"
    tabindex="0"
  >
    <div class="flex items-start gap-2">
      <span
        aria-hidden="true"
        class="mt-[5px] h-2 w-2 shrink-0 rounded-full {statusDotClass}"
      ></span>
      <div class="min-w-0 flex-1">
        <span
          class="block truncate text-meta font-medium leading-snug {titleColorClass}"
        >
          {headerTitle}
        </span>

        <div class="mt-1 flex flex-wrap items-center gap-1">
          <span
            class="rounded-md px-1 py-0.5 text-micro font-medium {cardResolutionTone(
              cardResolution,
            )}"
          >
            {cardResolutionLabel(cardResolution)}
          </span>

          {#if assigneeVisible.length > 0}
            {#each assigneeVisible as name (name)}
              <span
                class="max-w-[7rem] truncate rounded-md bg-line px-1 py-0.5 text-micro text-fg-muted"
                title={name}
              >
                {name}
              </span>
            {/each}
            {#if assigneeMore > 0}
              <span
                class="rounded-md bg-line px-1 py-0.5 text-micro text-fg-muted"
              >
                +{assigneeMore} more
              </span>
            {/if}
          {/if}

          {#if cardDueAt}
            <span
              class="rounded-md px-1 py-0.5 text-micro {dueOverdue
                ? 'bg-danger-soft text-danger-text'
                : 'bg-line text-fg-muted'}"
            >
              Due {formatTimestamp(cardDueAt) || "—"}
            </span>
          {/if}

          {#if cardFreshness}
            <span
              class="rounded-md px-1 py-0.5 text-micro {freshnessStatusTone(
                cardFreshness.status,
              )}"
            >
              {freshnessStatusLabel(cardFreshness.status)}
            </span>
          {/if}
        </div>

        {#if showSummary}
          <p class="mt-1 truncate text-micro text-fg-muted">
            {summaryText}
          </p>
        {/if}
      </div>
    </div>
  </div>
  {@render footer?.()}
</div>
