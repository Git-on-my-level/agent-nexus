<script>
  import {
    actorRegistry,
    lookupActorDisplayName,
    principalRegistry,
  } from "$lib/actorSession";
  import {
    boardCardStableId,
    freshnessStatusLabel,
    freshnessStatusTone,
  } from "$lib/boardUtils";
  import {
    cardResolutionLabel,
    cardResolutionTone,
    priorityBadgeClasses,
  } from "$lib/cardDisplayUtils";
  import { formatTimestamp } from "$lib/formatDate";
  import { getPriorityLabel } from "$lib/topicFilters";
  import { boardCardInspectNav } from "$lib/topicRouteUtils";
  import { workspacePath } from "$lib/workspacePaths";

  /**
   * @typedef {object} BoardCardProps
   * @property {object} cardItem
   * @property {string} [boardId]
   * @property {string} [workspaceSlug]
   * @property {() => void} [onclick]
   * @property {import("svelte").Snippet} [footer]
   */

  /** @type {BoardCardProps} */
  let {
    cardItem,
    boardId = "",
    workspaceSlug = "",
    onclick = () => {},
    footer,
  } = $props();

  const membership = $derived(cardItem?.membership);
  const backing = $derived(cardItem?.backing);
  const derived = $derived(cardItem?.derived);
  const thread = $derived(backing?.thread);

  const cardInspectNav = $derived(boardCardInspectNav(membership, thread));
  const showThreadNav = $derived(Boolean(cardInspectNav && workspaceSlug));
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

  const topicHref = $derived.by(() => {
    if (!workspaceSlug || !cardInspectNav) return "";
    const path =
      cardInspectNav.kind === "topic"
        ? `/topics/${encodeURIComponent(cardInspectNav.segment)}`
        : `/threads/${encodeURIComponent(cardInspectNav.segment)}`;
    try {
      return workspacePath(workspaceSlug, path);
    } catch {
      return "";
    }
  });

  const dueOverdue = $derived.by(() => {
    if (!cardDueAt) return false;
    const d = new Date(cardDueAt);
    if (isNaN(d.getTime())) return false;
    return d.getTime() < Date.now();
  });

  const priorityKey = $derived(
    String(thread?.priority ?? "")
      .trim()
      .toLowerCase(),
  );

  const priorityBadge = $derived.by(() => {
    const p = priorityKey;
    if (!p) return null;
    let label;
    switch (p) {
      case "p0":
        label = "P0";
        break;
      case "p1":
        label = "P1";
        break;
      case "p2":
        label = "P2";
        break;
      case "p3":
        label = "P3";
        break;
      default:
        label = getPriorityLabel(thread?.priority);
        break;
    }
    return { label, class: priorityBadgeClasses(p) };
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
        return "bg-emerald-400";
      case "canceled":
        return "bg-gray-500";
      case "paused":
        return "bg-amber-400";
      case "stale":
        return "bg-orange-400";
      case "very-stale":
        return "bg-red-400";
      default:
        return "bg-blue-400";
    }
  }

  function threadStatusColor(status) {
    switch (status) {
      case "done":
        return "text-emerald-400";
      case "canceled":
        return "text-[var(--ui-text-muted)]";
      case "paused":
        return "text-amber-400";
      case "stale":
        return "text-orange-400";
      case "very-stale":
        return "text-red-400";
      default:
        return "text-[var(--ui-text)]";
    }
  }

  function getThreadStatus(t) {
    if (!t) return "unknown";
    if (t.status === "done") return "done";
    if (t.status === "canceled") return "canceled";
    if (t.status === "paused") return "paused";
    if (t.staleness === "stale") return "stale";
    if (t.staleness === "very-stale") return "very-stale";
    return "active";
  }

  function boardCardRowStatus(m, t) {
    const resolution = String(m?.resolution ?? "").trim();
    if (resolution === "done" || resolution === "completed") return "done";
    if (resolution === "canceled" || resolution === "cancelled")
      return "canceled";
    if (resolution === "superseded") return "paused";
    if (t) return getThreadStatus(t);
    if (String(m?.column_key ?? "").trim() === "done") return "done";
    const s = String(m?.status ?? "").trim();
    if (s === "done") return "done";
    if (s === "cancelled") return "canceled";
    return "active";
  }

  function boardCardHeaderTitle(m, t) {
    const cardTitle = String(m?.title ?? "").trim();
    if (cardTitle) return cardTitle;
    const threadTitle = String(t?.title ?? "").trim();
    if (threadTitle) return threadTitle;
    return boardCardStableId(m);
  }

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
  class="group overflow-hidden rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] transition-colors hover:border-[var(--ui-border-strong)]"
>
  <div
    aria-label={`Manage ${headerTitle}`}
    class="cursor-pointer px-2.5 py-2 transition-colors hover:bg-[var(--ui-border-subtle)]/20"
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
        {#if showThreadNav && topicHref}
          <a
            class="block truncate text-[13px] font-medium leading-snug transition-colors hover:text-indigo-300 {titleColorClass}"
            href={topicHref}
            onclick={(e) => e.stopPropagation()}
          >
            {headerTitle}
          </a>
        {:else}
          <span
            class="block truncate text-[13px] font-medium leading-snug {titleColorClass}"
          >
            {headerTitle}
          </span>
        {/if}

        <div class="mt-1 flex flex-wrap items-center gap-1">
          <span
            class="rounded-md px-1 py-0.5 text-[11px] font-medium {cardResolutionTone(
              cardResolution,
            )}"
          >
            {cardResolutionLabel(cardResolution)}
          </span>

          {#if priorityBadge}
            <span
              class="rounded-md px-1 py-0.5 text-[11px] font-medium {priorityBadge.class}"
            >
              {priorityBadge.label}
            </span>
          {/if}

          {#if assigneeVisible.length > 0}
            {#each assigneeVisible as name}
              <span
                class="max-w-[7rem] truncate rounded-md bg-[var(--ui-border)] px-1 py-0.5 text-[11px] text-[var(--ui-text-subtle)]"
                title={name}
              >
                {name}
              </span>
            {/each}
            {#if assigneeMore > 0}
              <span
                class="rounded-md bg-[var(--ui-border)] px-1 py-0.5 text-[11px] text-[var(--ui-text-muted)]"
              >
                +{assigneeMore} more
              </span>
            {/if}
          {/if}

          {#if cardDueAt}
            <span
              class="rounded-md px-1 py-0.5 text-[11px] {dueOverdue
                ? 'bg-red-500/10 text-red-400'
                : 'bg-[var(--ui-border)] text-[var(--ui-text-muted)]'}"
            >
              Due {formatTimestamp(cardDueAt) || "—"}
            </span>
          {/if}

          {#if cardFreshness}
            <span
              class="rounded-md px-1 py-0.5 text-[11px] {freshnessStatusTone(
                cardFreshness.status,
              )}"
            >
              {freshnessStatusLabel(cardFreshness.status)}
            </span>
          {/if}
        </div>

        {#if summaryText}
          <p class="mt-1 truncate text-[11px] text-[var(--ui-text-muted)]">
            {summaryText}
          </p>
        {/if}
      </div>
    </div>
  </div>
  {@render footer?.()}
</div>
