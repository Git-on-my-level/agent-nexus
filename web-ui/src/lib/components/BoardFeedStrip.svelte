<script>
  import { boardBackingThreadId } from "$lib/boardUtils";
  import DiscussionDrawer from "$lib/components/DiscussionDrawer.svelte";

  let { board, workspaceSlug = "", workspaceId = "" } = $props();

  let threadId = $derived(boardBackingThreadId(board));

  const DISCUSSION_EMPTY =
    "Board-wide updates and discussion live here, separate from individual card threads. Post a short note the whole board should see — triage callouts, column policy, or sprint boundaries.";
</script>

<DiscussionDrawer
  {threadId}
  {workspaceId}
  {workspaceSlug}
  label="Board announcements"
  storageKey="board-feed:{threadId}"
  emptyMessage={DISCUSSION_EMPTY}
  expandFillsParent
  narrowEdgeToEdge
/>
