<script>
  import DiscussionDrawer from "$lib/components/DiscussionDrawer.svelte";
  import { boardDiscussionSurface } from "$lib/discussionSurface";

  let { board, workspaceSlug = "", workspaceId = "" } = $props();

  let surface = $derived(boardDiscussionSurface(board));
</script>

<DiscussionDrawer {...surface} {workspaceId} {workspaceSlug} />
