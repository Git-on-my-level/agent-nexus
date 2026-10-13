<script>
  /**
   * The destination's own shape, painted before its data arrives: the page
   * name in the header position it will keep, and row placeholders in the
   * bordered container the list will fill.
   *
   * The title is real text, not a grey bar — the first paint should answer
   * "where am I?", which is the one question a blank screen with a spinner
   * cannot. Nothing here reads anything; it adds no requests.
   */
  import SkeletonList from "../state/SkeletonList.svelte";
  import WorkspacePageHeader from "./WorkspacePageHeader.svelte";
  import WorkspacePageShell from "./WorkspacePageShell.svelte";

  let { title = "", rows = 6 } = $props();
</script>

<WorkspacePageShell>
  <WorkspacePageHeader {title} />
  <SkeletonList {rows} />
</WorkspacePageShell>
