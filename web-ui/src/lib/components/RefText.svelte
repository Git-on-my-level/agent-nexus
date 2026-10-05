<script>
  /**
   * A run of report text with its markdown, refs and links made real.
   *
   * Report panels carry plain strings — a table cell, a callout, a milestone
   * label, a diagram node. A `card:` ref written in one of those used to render
   * as inert text, a bare URL stayed unclickable, and `**bold**` showed its
   * asterisks. This is now a thin wrapper over the one markdown renderer, so a
   * report cell reads the same way the same text reads in a card body or a doc.
   *
   * It never fetches. Pass the page's batch-resolve result; a ref the page did
   * not resolve renders as a "not found" chip rather than disappearing.
   */
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";

  let {
    text = "",
    resolved = new Map(),
    organizationSlug = "",
    workspaceSlug = "",
    onpreview = null,
    onpreviewclose = null,
  } = $props();
</script>

<MarkdownRenderer
  inline
  source={text}
  class="ref-text"
  {resolved}
  {organizationSlug}
  {workspaceSlug}
  {onpreview}
  {onpreviewclose}
/>
