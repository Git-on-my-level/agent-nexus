<script>
  import CardDetailModalInner from "$lib/components/CardDetailModalInner.svelte";

  let {
    open = false,
    cardItem = null,
    columnPeers = [],
    boardId = "",
    board = null,
    workspaceSlug = "",
    primaryTopic = null,
    actorName = (id) => id,
    onclose = () => {},
    onmovecard = async () => {},
    onsavecard = async () => {},
    onremovecard = async () => {},
  } = $props();
</script>

{#if open && cardItem}
  <CardDetailModalInner
    {cardItem}
    {columnPeers}
    {boardId}
    {board}
    {workspaceSlug}
    {primaryTopic}
    {actorName}
    {onclose}
    {onmovecard}
    {onsavecard}
    {onremovecard}
  />
{/if}
