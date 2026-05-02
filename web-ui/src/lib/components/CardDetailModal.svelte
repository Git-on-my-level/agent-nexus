<script>
  import CardDetailModalInner from "$lib/components/CardDetailModalInner.svelte";

  let {
    open = false,
    cardItem = null,
    boardId = "",
    board = null,
    workspaceSlug = "",
    workspaceId = "",
    primaryTopic = null,
    columnPeerStableIds = [],
    actorName = (id) => id,
    requestedDetailTab = "",
    onDetailTabChange = undefined,
    onclose = () => {},
    onmovecard = async () => {},
    onsavecard = async () => {},
    onrevisecard = async () => {},
    onremovecard = async () => {},
  } = $props();
</script>

{#if open && cardItem}
  <CardDetailModalInner
    {cardItem}
    {boardId}
    {board}
    {workspaceSlug}
    {workspaceId}
    {primaryTopic}
    {columnPeerStableIds}
    {actorName}
    {requestedDetailTab}
    {onDetailTabChange}
    {onclose}
    {onmovecard}
    {onsavecard}
    {onrevisecard}
    {onremovecard}
  />
{/if}
