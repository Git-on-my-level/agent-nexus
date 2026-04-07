<script>
  import { page } from "$app/stores";
  import { BOARD_STATUS_LABELS, boardCardStableId } from "$lib/boardUtils";
  import { formatTimestamp } from "$lib/formatDate";
  import { workspacePath } from "$lib/workspacePaths";
  import { topicDetailStore } from "$lib/topicDetailStore";
  import { parseRef } from "$lib/typedRefs";

  let ownedBoards = $derived($topicDetailStore.ownedBoards);
  let boardMemberships = $derived($topicDetailStore.boardMemberships);
  let workspaceSlug = $derived($page.params.workspace);

  let hasAny = $derived(ownedBoards.length > 0 || boardMemberships.length > 0);

  function statusTone(status) {
    if (status === "active") return "text-emerald-400 bg-emerald-500/10";
    if (status === "paused") return "text-amber-400 bg-amber-500/10";
    if (status === "closed")
      return "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
    return "text-[var(--ui-text-muted)] bg-[var(--ui-border)]";
  }

  function columnLabel(key) {
    if (!key) return "";
    return key.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
  }

  function pinnedDocumentHref(documentId) {
    const normalized = String(documentId ?? "").trim();
    if (!normalized) {
      return "";
    }

    return workspacePath(
      workspaceSlug,
      `/docs/${encodeURIComponent(normalized)}`,
    );
  }

  function documentIdFromCardRef(refValue) {
    const { prefix, value } = parseRef(String(refValue ?? "").trim());
    return prefix === "document" ? String(value ?? "").trim() : "";
  }
</script>

<section
  class="mt-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)]"
>
  <div
    class="flex items-center justify-between border-b border-[var(--ui-border-subtle)] px-4 py-2.5"
  >
    <div>
      <h2
        class="text-[12px] font-semibold uppercase tracking-wider text-[var(--ui-text-muted)]"
      >
        Boards
      </h2>
      <p class="mt-0.5 text-[12px] text-[var(--ui-text-subtle)]">
        Boards owned by or tracking this topic.
      </p>
    </div>
    <a
      class="text-[12px] font-medium text-indigo-400 transition-colors hover:text-indigo-300"
      href={workspacePath(workspaceSlug, "/boards")}
    >
      All boards
    </a>
  </div>

  {#if !hasAny}
    <p class="px-4 py-3 text-[13px] text-[var(--ui-text-muted)]">
      This topic isn't tracked on any boards yet.
    </p>
  {:else}
    <div class="divide-y divide-[var(--ui-border-subtle)]">
      {#if ownedBoards.length > 0}
        <div class="divide-y divide-[var(--ui-border-subtle)]">
          <div
            class="text-[11px] font-semibold uppercase tracking-wide text-[var(--ui-text-subtle)] px-4 pt-2.5 pb-1"
          >
            Owned by this topic
          </div>
          {#each ownedBoards as board}
            <a
              class="flex items-center justify-between gap-3 px-4 py-2.5 transition-colors hover:bg-[var(--ui-bg-soft)]"
              href={workspacePath(workspaceSlug, `/boards/${board.id}`)}
            >
              <div class="flex min-w-0 items-center gap-2">
                <span
                  class="truncate text-[13px] font-medium text-[var(--ui-text)]"
                >
                  {board.title || board.id}
                </span>
                {#if board.status}
                  <span
                    class="shrink-0 rounded px-1.5 py-0.5 text-[11px] font-semibold {statusTone(
                      board.status,
                    )}"
                  >
                    {BOARD_STATUS_LABELS[board.status] ?? board.status}
                  </span>
                {/if}
              </div>
              <div class="shrink-0 text-[11px] text-[var(--ui-text-subtle)]">
                {board.card_count ?? 0} cards · {formatTimestamp(
                  board.updated_at,
                ) || "—"}
              </div>
            </a>
          {/each}
        </div>
      {/if}

      {#if boardMemberships.length > 0}
        <div class="divide-y divide-[var(--ui-border-subtle)]">
          <div
            class="text-[11px] font-semibold uppercase tracking-wide text-[var(--ui-text-subtle)] px-4 pt-2.5 pb-1"
          >
            Appears as card on
          </div>
          {#each boardMemberships as membership}
            {@const boardId = membership?.board?.id ?? membership?.board_id}
            {@const boardTitle =
              membership?.board?.title ?? membership?.board_title ?? boardId}
            {@const boardStatus =
              membership?.board?.status ?? membership?.board_status}
            {@const columnKey =
              membership?.card?.column_key ?? membership?.column_key}
            {@const pinnedDocumentId = documentIdFromCardRef(
              membership?.card?.document_ref ?? membership?.document_ref ?? "",
            )}
            {@const cardMembership = membership?.card}
            {@const boardCardHref = cardMembership
              ? `${workspacePath(workspaceSlug, `/boards/${boardId}`)}?card=${encodeURIComponent(
                  boardCardStableId(cardMembership),
                )}`
              : workspacePath(workspaceSlug, `/boards/${boardId}`)}
            {#if boardId}
              <div class="px-4 py-2.5">
                <div class="flex items-center justify-between gap-3">
                  <a
                    class="flex min-w-0 items-center gap-2 transition-colors hover:text-indigo-300"
                    href={boardCardHref}
                  >
                    <span
                      class="truncate text-[13px] font-medium text-[var(--ui-text)]"
                    >
                      {boardTitle}
                    </span>
                    {#if boardStatus}
                      <span
                        class="shrink-0 rounded px-1.5 py-0.5 text-[11px] font-semibold {statusTone(
                          boardStatus,
                        )}"
                      >
                        {BOARD_STATUS_LABELS[boardStatus] ?? boardStatus}
                      </span>
                    {/if}
                    {#if columnKey}
                      <span
                        class="shrink-0 rounded bg-[var(--ui-border)] px-1.5 py-0.5 text-[11px] text-[var(--ui-text-muted)]"
                      >
                        {columnLabel(columnKey)}
                      </span>
                    {/if}
                  </a>
                  <span
                    class="shrink-0 text-[11px] text-[var(--ui-text-subtle)]"
                  >
                    Card
                  </span>
                </div>
                {#if pinnedDocumentId}
                  <div class="mt-1.5 text-[11px] text-[var(--ui-text-muted)]">
                    <a
                      class="text-indigo-400 transition-colors hover:text-indigo-300"
                      href={pinnedDocumentHref(pinnedDocumentId)}
                    >
                      Pinned doc: {pinnedDocumentId}
                    </a>
                  </div>
                {/if}
              </div>
            {/if}
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</section>
