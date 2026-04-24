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
    {boardId}
    {board}
    {workspaceSlug}
    {workspaceId}
    {primaryTopic}
    {actorName}
    {onclose}
    {onmovecard}
    {onsavecard}
    {onremovecard}
  />
{/if}
