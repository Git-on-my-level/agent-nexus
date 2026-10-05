<script>
  /**
   * A run of report text with its refs and links made real.
   *
   * Report panels carry plain strings — a table cell, a callout, a milestone
   * label, a diagram node. A `card:` ref written in one of those used to render
   * as inert text, and a bare URL stayed unclickable. This splits the string
   * into prose, refs and URLs and renders each as itself, so the same chip
   * appears wherever a ref is written.
   *
   * It never fetches. Pass the page's batch-resolve result; a ref the page did
   * not resolve renders as a "not found" chip rather than disappearing.
   */
  import AnxRefChip from "$lib/components/AnxRefChip.svelte";
  import { safeRefDestination, tokenizeRefText } from "$lib/refResolve.js";

  let {
    text = "",
    resolved = new Map(),
    organizationSlug = "",
    workspaceSlug = "",
    onpreview = null,
    onpreviewclose = null,
  } = $props();

  let tokens = $derived(tokenizeRefText(text));
</script>

{#each tokens as token, index (index)}{#if token.type === "ref"}<AnxRefChip
      refValue={token.value}
      {resolved}
      {organizationSlug}
      {workspaceSlug}
      showKind={false}
      {onpreview}
      {onpreviewclose}
    />{:else if token.type === "url" && token.kind}<AnxRefChip
      refValue={token.value}
      {resolved}
      {organizationSlug}
      {workspaceSlug}
      showKind={false}
      {onpreview}
      {onpreviewclose}
    />{:else if token.type === "url" && safeRefDestination(token.value)}<a
      class="ref-text__link"
      href={safeRefDestination(token.value)}
      rel="noreferrer noopener"
      target="_blank">{token.value}</a
    >{:else}{token.value}{/if}{/each}

<style>
  .ref-text__link {
    color: var(--accent-text);
    overflow-wrap: anywhere;
  }
  .ref-text__link:hover {
    text-decoration: underline;
  }
</style>
