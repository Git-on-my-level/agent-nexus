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

  import AgeBadge from "$lib/components/AgeBadge.svelte";
  import {
    clearContextMenu,
    registerContextMenu,
  } from "$lib/contextMenuSingleton.js";

  const ID = "anx-ref-preview";

  let model = $state(null);
  let anchor = $state(null);
  let position = $state({ top: 0, left: 0 });
  let card = $state();

  /**
   * Closing is deferred by a beat so the preview survives the gap between
   * leaving the chip and arriving on the card — and the gap between the chip
   * losing focus and a control inside the card gaining it. Without that, Open
   * and Copy ref could be seen but never reached: the preview vanished on the
   * way to them.
   */
  let closeTimer = null;

  function cancelClose() {
    if (closeTimer) {
      clearTimeout(closeTimer);
      closeTimer = null;
    }
  }

  /**
   * Escape closes the dialog and puts focus back on the chip — which fires the
   * chip's own focus handler and would reopen what the reader just dismissed.
   * This window lets that one focus event pass without reopening.
   */
  let dismissedAt = 0;
  const REOPEN_GUARD_MS = 400;

  export function open(nextModel, nextAnchor) {
    if (Date.now() - dismissedAt < REOPEN_GUARD_MS) return;
    cancelClose();
    model = nextModel;
    anchor = nextAnchor;
    registerContextMenu(ID, close);
  }

  /** Controls inside the card, in the order a reader tabs through them. */
  function focusables() {
    if (!card) return [];
    return [...card.querySelectorAll("a[href], button:not([disabled])")];
  }

  /** Leave it open long enough to travel to it. */
  export function requestClose(delay = 180) {
    cancelClose();
    closeTimer = setTimeout(() => {
      closeTimer = null;
      close();
    }, delay);
  }

  export function close({ dismissed = false } = {}) {
    cancelClose();
    const returnFocus =
      dismissed || card?.contains(document.activeElement) ? anchor : null;
    if (dismissed) dismissedAt = Date.now();
    model = null;
    anchor = null;
    clearContextMenu(ID);
    // Dismissing puts the reader back on the chip they came from, rather than
    // dropping focus to the top of the document.
    returnFocus?.focus?.();
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
    const onKeydown = (event) => {
      if (!model) return;
      if (event.key === "Escape") {
        event.stopPropagation();
        close({ dismissed: true });
        return;
      }
      // The card is appended at the end of the document, so it is nowhere near
      // the chip in tab order and its Open / Copy ref controls would never be
      // reached. Tab from the chip steps into the card instead.
      if (
        event.key === "Tab" &&
        !event.shiftKey &&
        document.activeElement === anchor
      ) {
        const [first] = focusables();
        if (first) {
          event.preventDefault();
          event.stopPropagation();
          first.focus();
        }
        return;
      }
      // Shift+Tab off the front of the card goes back to the chip it belongs
      // to, which is where the reader came from.
      if (
        event.key === "Tab" &&
        event.shiftKey &&
        document.activeElement === focusables()[0]
      ) {
        event.preventDefault();
        event.stopPropagation();
        close({ dismissed: true });
      }
    };
    window.addEventListener("scroll", onScrollOrResize, true);
    window.addEventListener("resize", onScrollOrResize);
    document.addEventListener("keydown", onKeydown, true);
    return () => {
      document.removeEventListener("keydown", onKeydown, true);
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
</script>

{#if model}
  <!--
    A dialog, not a tooltip: it holds Open and Copy ref, so it is interactive
    content a reader can move into. `aria-modal` stays off — it does not trap
    focus or block the page; Escape closes it and returns focus to the chip.
  -->
  <div
    bind:this={card}
    class="anx-ref-preview"
    style:top="{position.top}px"
    style:left="{position.left}px"
    role="dialog"
    aria-label={`${model.kindLabel || "Ref"}: ${model.title}`}
    tabindex="-1"
    onmouseenter={cancelClose}
    onfocusin={cancelClose}
    onmouseleave={() => requestClose()}
    onfocusout={() => requestClose()}
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
      {#if model.lastMovedAt}<AgeBadge
          at={model.lastMovedAt}
          verb="moved"
        />{/if}
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
