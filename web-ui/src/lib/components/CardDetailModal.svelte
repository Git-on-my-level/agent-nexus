<script>
  import { writable } from "svelte/store";

  import CardDetailModalInner from "$lib/components/CardDetailModalInner.svelte";

  let {
    open = false,
    cardItem = null,
    columnPeers = [],
    boardId = "",
    board = null,
    workspaceSlug = "",
    actorName = (id) => id,
    onclose = () => {},
    onmovecard = async () => {},
    onsavecard = async () => {},
    onremovecard = async () => {},
  } = $props();

  /**
   * Owned here so it survives `CardDetailModalInner` remounts inside `{#if open && cardItem}`.
   * @type {import("svelte/store").Writable<"overview" | "messages" | "timeline">}
   */
  const cdmDetailPane = writable("overview");
</script>

{#if open && cardItem}
  <CardDetailModalInner
    {cdmDetailPane}
    {cardItem}
    {columnPeers}
    {boardId}
    {board}
    {workspaceSlug}
    {actorName}
    {onclose}
    {onmovecard}
    {onsavecard}
    {onremovecard}
  />
{/if}
