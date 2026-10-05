<script>
  /**
   * One ANX ref, as a chip: a status dot, a title, and a kind label where it
   * adds something.
   *
   * The chip never fetches. It renders from a page-level batch resolve, which
   * is what keeps a table of twenty rows to one request. A ref the resolve
   * could not place renders dashed and says "not found" — it is never dropped,
   * because a plan pointing at something missing is exactly what a reader
   * needs to see.
   *
   * Hover and focus both open the preview, so the card is reachable from the
   * keyboard rather than being a pointer-only affordance.
   */
  import { refChipModel } from "$lib/refResolve.js";

  let {
    /** The raw ref or work URL. */
    refValue = "",
    /** Page-level batch resolve result, from `indexResolvedRefs`. */
    resolved = new Map(),
    organizationSlug = "",
    workspaceSlug = "",
    /** Show the kind label ("Task", "Doc"); off inside a column that already says it. */
    showKind = true,
    /** Called with the model and the anchor element when the preview should open. */
    onpreview = null,
    /**
     * Called when the preview should close. The preview defers the close, so
     * the reader can travel from the chip to the card's Open / Copy ref
     * controls without it disappearing on the way.
     */
    onpreviewclose = null,
    class: extraClass = "",
  } = $props();

  let model = $derived(
    refChipModel(refValue, resolved, { organizationSlug, workspaceSlug }),
  );

  let host = $state();

  function open() {
    if (host) onpreview?.(model, host);
  }
  function close() {
    onpreviewclose?.();
  }
</script>

{#snippet body()}
  <span
    class="anx-ref-chip__dot"
    data-tone={model.resolvable ? model.statusTone : "missing"}
    aria-hidden="true"
  ></span>
  <span class="anx-ref-chip__title">{model.title}</span>
  {#if !model.resolvable}
    <span class="anx-ref-chip__kind">not found</span>
  {:else}
    <!--
      An external record's status belongs on the chip: a plan step pointing at
      a GitHub pull request is answered by "merged" or "open", and that word is
      the reason the step is where it is. An ANX ref's status is already in the
      dot's tone and in the column it sits in.
    -->
    {#if model.isExternal && model.statusLabel}
      <span class="anx-ref-chip__status" data-tone={model.statusTone}
        >{model.statusLabel}</span
      >
    {/if}
    {#if showKind && model.kindLabel}
      <span class="anx-ref-chip__kind">{model.kindLabel}</span>
    {/if}
  {/if}
{/snippet}

<!--
  A resolvable ref with somewhere to go is a real link. One without — a board,
  or a ref that resolves to nothing — is a `note`: it carries information but
  there is nothing to open, and it still takes focus so the keyboard reaches
  its preview. The two are written out rather than switched with
  `<svelte:element>` so the anchor is statically an anchor; a dynamic element
  carrying hover handlers cannot be proved interactive and trips the a11y rule.
-->
{#if model.resolvable && model.href}
  <a
    bind:this={host}
    class="anx-ref-chip {extraClass}"
    href={model.href}
    rel={model.isExternal ? "noreferrer noopener" : undefined}
    target={model.isExternal ? "_blank" : undefined}
    data-anx-ref={model.raw}
    data-status={model.status || undefined}
    title={model.raw}
    aria-label={`${model.kindLabel || "Ref"}: ${model.title}`}
    onmouseenter={open}
    onfocusin={open}
    onmouseleave={close}
    onfocusout={close}>{@render body()}</a
  >
{:else}
  <!-- The chip carries a preview the keyboard has to be able to open, so it
       takes focus even though there is nothing to activate. -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <span
    bind:this={host}
    class="anx-ref-chip {extraClass}"
    class:anx-ref-chip--missing={!model.resolvable}
    role="note"
    tabindex="0"
    data-anx-ref={model.raw}
    data-status={model.status || undefined}
    title={model.raw}
    aria-label={model.resolvable
      ? `${model.kindLabel || "Ref"}: ${model.title}`
      : `Not found: ${model.raw}`}
    onmouseenter={open}
    onfocusin={open}
    onmouseleave={close}
    onfocusout={close}>{@render body()}</span
  >
{/if}

<style>
  .anx-ref-chip {
    display: inline-flex;
    align-items: baseline;
    gap: 5px;
    max-width: 100%;
    min-width: 0;
    padding: 1px 6px;
    border: 1px solid var(--line);
    border-radius: 4px;
    background: var(--bg);
    color: var(--accent-text);
    font-size: 11px;
    line-height: 1.5;
    vertical-align: baseline;
  }
  .anx-ref-chip:hover {
    border-color: var(--line-strong);
  }
  .anx-ref-chip:focus-visible {
    outline: 2px solid var(--accent-solid);
    outline-offset: 1px;
  }
  /* Dashed and muted: visibly present, visibly not a destination. */
  .anx-ref-chip--missing {
    border-style: dashed;
    color: var(--fg-muted);
  }
  .anx-ref-chip__dot {
    flex: none;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    /* Baseline-aligned text next to a dot needs the dot nudged to the text's
       optical centre, not its baseline. */
    transform: translateY(-1px);
    background: var(--fg-muted);
  }
  .anx-ref-chip__dot[data-tone="ok"] {
    background: var(--ok-text, var(--accent-solid));
  }
  .anx-ref-chip__dot[data-tone="danger"] {
    background: var(--danger-text, var(--warn-text));
  }
  .anx-ref-chip__dot[data-tone="missing"] {
    background: transparent;
    border: 1px dashed var(--fg-muted);
  }
  .anx-ref-chip__title {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .anx-ref-chip__kind {
    flex: none;
    color: var(--fg-muted);
    font-size: 10px;
    text-transform: lowercase;
  }
  .anx-ref-chip__status {
    flex: none;
    color: var(--fg-muted);
    font-size: 10px;
    text-transform: lowercase;
  }
  .anx-ref-chip__status[data-tone="ok"] {
    color: var(--ok-text, var(--accent-text));
  }
  .anx-ref-chip__status[data-tone="danger"] {
    color: var(--danger-text, var(--warn-text));
  }
</style>
