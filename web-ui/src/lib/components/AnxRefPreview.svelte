<script>
  /**
   * The preview card behind a ref chip: kind, board, priority, status, a
   * progress bar, the next step, the owner, when it last moved, and Open /
   * Copy ref.
   *
   * One of these is mounted per page, not per chip. Chips ask it to open
   * against their own element, which is what keeps a table of chips to a single
   * floating layer. It registers through `contextMenuSingleton` so an open
   * preview and an open context menu cannot overlap.
   *
   * The surface is opaque on purpose: `scripts/check-floating-layers.mjs`
   * fails a fixed translucent layer, because whatever scrolls under it stays
   * readable through it.
   */
  import { onMount } from "svelte";

  import {
    clearContextMenu,
    registerContextMenu,
  } from "$lib/contextMenuSingleton.js";
  import { formatMovedAgo } from "$lib/refResolve.js";

  const ID = "anx-ref-preview";

  let model = $state(null);
  let anchor = $state(null);
  let position = $state({ top: 0, left: 0 });
  let card = $state();

  export function open(nextModel, nextAnchor) {
    model = nextModel;
    anchor = nextAnchor;
    registerContextMenu(ID, close);
  }

  export function close() {
    model = null;
    anchor = null;
    clearContextMenu(ID);
  }

  /**
   * Place the card under its chip, then pull it back inside the viewport. A
   * chip near the right edge or low on a phone screen would otherwise open a
   * card the reader cannot read.
   */
  function place() {
    if (!anchor || !card) return;
    const chip = anchor.getBoundingClientRect();
    const box = card.getBoundingClientRect();
    const margin = 8;
    const left = Math.max(
      margin,
      Math.min(chip.left, window.innerWidth - box.width - margin),
    );
    const below = chip.bottom + 6;
    const top =
      below + box.height + margin > window.innerHeight
        ? Math.max(margin, chip.top - box.height - 6)
        : below;
    position = { top, left };
  }

  $effect(() => {
    if (model && anchor && card) place();
  });

  onMount(() => {
    const onScrollOrResize = () => (model ? close() : undefined);
    window.addEventListener("scroll", onScrollOrResize, true);
    window.addEventListener("resize", onScrollOrResize);
    return () => {
      window.removeEventListener("scroll", onScrollOrResize, true);
      window.removeEventListener("resize", onScrollOrResize);
      clearContextMenu(ID);
    };
  });

  let copied = $state(false);
  async function copyRef() {
    if (!model) return;
    try {
      await navigator.clipboard?.writeText(model.raw);
      copied = true;
      setTimeout(() => (copied = false), 1200);
    } catch {
      // A denied clipboard is not worth an error state; the ref is on screen.
    }
  }

  let movedLabel = $derived(
    model?.lastMovedAt ? formatMovedAgo(model.lastMovedAt) : "",
  );
</script>

{#if model}
  <!--
    `role="tooltip"` rather than a dialog: it does not take focus, and it
    follows the chip the reader is already on.
  -->
  <div
    bind:this={card}
    class="anx-ref-preview"
    style:top="{position.top}px"
    style:left="{position.left}px"
    role="tooltip"
    aria-live="polite"
  >
    <p class="anx-ref-preview__title">{model.title}</p>
    <p class="anx-ref-preview__meta">
      {#if model.kindLabel}<span>{model.kindLabel}</span>{/if}
      {#if model.board}<span>{model.board}</span>{/if}
      {#if model.priority}<span>{model.priority}</span>{/if}
      {#if model.statusLabel}<span data-tone={model.statusTone}
          >{model.statusLabel}</span
        >{/if}
    </p>

    {#if !model.resolvable}
      <p class="anx-ref-preview__missing">
        This ref does not resolve to anything in the workspace.
      </p>
    {/if}

    {#if model.progress}
      <div class="anx-ref-preview__progress">
        <progress
          value={model.progress.done}
          max={model.progress.total}
          aria-label="{model.title} checklist"
        ></progress>
        <span>{model.progress.done}/{model.progress.total}</span>
      </div>
    {/if}

    {#if model.nextStep}
      <p class="anx-ref-preview__next">Next: {model.nextStep}</p>
    {/if}

    <p class="anx-ref-preview__meta">
      {#if model.owner}<span>{model.owner}</span>{/if}
      {#if movedLabel}<span>moved {movedLabel}</span>{/if}
    </p>

    <div class="anx-ref-preview__actions">
      {#if model.resolvable && model.href}
        <a
          class="ui-btn-secondary"
          href={model.href}
          rel={model.isExternal ? "noreferrer noopener" : undefined}
          target={model.isExternal ? "_blank" : undefined}>Open</a
        >
      {/if}
      <button class="ui-btn-secondary" type="button" onclick={copyRef}
        >{copied ? "Copied" : "Copy ref"}</button
      >
    </div>
  </div>
{/if}

<style>
  .anx-ref-preview {
    position: fixed;
    z-index: 70;
    width: min(22rem, calc(100vw - 16px));
    padding: 10px 12px;
    border: 1px solid var(--line-strong);
    border-radius: 6px;
    /* Opaque: see the component comment. */
    background: var(--panel);
    box-shadow: 0 8px 24px rgb(0 0 0 / 0.28);
    color: var(--fg);
    font-size: 12px;
  }
  .anx-ref-preview__title {
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .anx-ref-preview__meta {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 10px;
    margin-top: 4px;
    color: var(--fg-muted);
    font-size: 11px;
  }
  .anx-ref-preview__meta span[data-tone="ok"] {
    color: var(--ok-text, var(--accent-text));
  }
  .anx-ref-preview__meta span[data-tone="danger"] {
    color: var(--danger-text, var(--warn-text));
  }
  .anx-ref-preview__missing {
    margin-top: 6px;
    color: var(--warn-text);
    font-size: 11px;
  }
  .anx-ref-preview__progress {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 8px;
    color: var(--fg-muted);
    font-size: 11px;
  }
  .anx-ref-preview__progress progress {
    flex: 1;
    height: 5px;
    border: 0;
    border-radius: 3px;
    background: var(--bg-soft);
    accent-color: var(--accent-solid);
  }
  .anx-ref-preview__next {
    margin-top: 6px;
    overflow-wrap: anywhere;
  }
  .anx-ref-preview__actions {
    display: flex;
    gap: 6px;
    margin-top: 10px;
  }
</style>
