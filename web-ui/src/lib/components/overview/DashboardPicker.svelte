<script>
  /**
   * The Overview's one dashboard control.
   *
   * It replaces a native `<select>` plus three buttons. The select was broken
   * in a way only a reader could see: opening it fired the lazy read for the
   * rest of the reports, the read replaced the `<option>` list under the open
   * popup, and Chromium closes a popup whose options change — so the list
   * appeared with one item and vanished. The fix is not to load earlier (that
   * would charge every Overview for a list most readers never open) but to own
   * the popup: this list is ordinary DOM, so it grows in place while open.
   *
   * One control, not four. Choosing, pinning and unpinning are the same
   * decision — "which report is my dashboard" — and three buttons spelled it
   * out three times. Pinning lives on the row it applies to; the header keeps
   * the control and the link out to the document.
   */
  import { dismissOnEscape } from "$lib/actions/dismissOnEscape.js";

  let {
    /** Report choices loaded so far, newest first. */
    reports = [],
    /** The entry currently rendered below, or `null`. */
    selected = null,
    /** `document:` ref of the pinned dashboard, or `""` when none is pinned. */
    pinnedRef = "",
    /** Whether core has more choices than `reports` holds. */
    hasMore = false,
    /** A choices page is in flight. */
    loading = false,
    /** Ask for the next page of choices. Called once per open. */
    onload = null,
    /** Show this report. */
    onselect = null,
    /** Pin `ref`, or unpin with `null`. */
    onpin = null,
  } = $props();

  let open = $state(false);
  let root = $state();
  let trigger = $state();

  let pinnedSelected = $derived(
    Boolean(selected?.ref) && selected.ref === pinnedRef,
  );

  function close({ focusTrigger = false } = {}) {
    if (!open) return;
    open = false;
    if (focusTrigger) trigger?.focus();
  }

  function toggle() {
    open = !open;
    // Lazy, and only while the reader is looking: the read that broke the
    // native popup is safe here because the list it feeds is ours.
    if (open) onload?.();
  }

  function choose(entry) {
    close({ focusTrigger: true });
    if (entry.id !== selected?.id) onselect?.(entry.id);
  }

  /*
   * The menu closes on the click; the header beside it reports the write.
   * Holding the menu open until `pinning` goes false again would hang it
   * open for good against a caller that never reports the write at all.
   */
  function pin(ref) {
    close({ focusTrigger: true });
    onpin?.(ref);
  }

  $effect(() => {
    if (!open) return;
    const onPointerDown = (event) => {
      if (event.target instanceof Node && root?.contains(event.target)) return;
      close();
    };
    /*
     * Focus leaving closes it too. Without this, tabbing past the last item
     * left the menu open over the page — and an open menu owns Escape
     * through `dismissOnEscape`, so it would then swallow the Escape meant
     * for whatever the reader had actually moved into.
     */
    const onFocusOut = (event) => {
      const next = event.relatedTarget;
      if (next instanceof Node && root?.contains(next)) return;
      // A focus loss with nowhere to go (a click on the page chrome) is the
      // pointerdown handler's business, not this one's.
      if (next === null) return;
      close();
    };
    document.addEventListener("pointerdown", onPointerDown, true);
    root?.addEventListener("focusout", onFocusOut);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown, true);
      root?.removeEventListener("focusout", onFocusOut);
    };
  });
</script>

<div class="picker" bind:this={root}>
  <button
    class="picker-trigger"
    type="button"
    bind:this={trigger}
    aria-haspopup="menu"
    aria-expanded={open}
    data-overview-report-picker
    onclick={toggle}
  >
    <span class="picker-value"
      >{selected?.title || selected?.id || "Choose a report"}</span
    >
    {#if pinnedSelected}<span class="picker-pinned" data-overview-report-pinned
        >Pinned</span
      >{/if}
    <span class="picker-caret" aria-hidden="true">▾</span>
  </button>
  {#if open}
    <div
      class="picker-menu"
      role="menu"
      aria-label="Report"
      use:dismissOnEscape={{ onDismiss: () => close({ focusTrigger: true }) }}
    >
      <ul class="picker-list" role="none">
        {#each reports as entry (entry.id)}
          <li role="none">
            <button
              type="button"
              role="menuitemradio"
              aria-checked={entry.id === selected?.id}
              class="picker-item"
              class:picker-item--current={entry.id === selected?.id}
              data-overview-report-choice={entry.id}
              onclick={() => choose(entry)}
            >
              <span class="picker-check" aria-hidden="true"
                >{entry.id === selected?.id ? "✓" : ""}</span
              >
              <span class="picker-name">{entry.title || entry.id}</span>
              {#if entry.ref && entry.ref === pinnedRef}
                <span class="picker-pinned">Pinned</span>
              {/if}
            </button>
          </li>
        {/each}
        {#if loading}
          <li class="picker-note" role="none">Loading reports…</li>
        {:else if hasMore}
          <li role="none">
            <button
              type="button"
              role="menuitem"
              class="picker-item picker-item--more"
              onclick={() => onload?.()}>Load more reports</button
            >
          </li>
        {/if}
      </ul>
      {#if selected}
        <div class="picker-actions" role="none">
          {#if pinnedSelected}
            <button
              type="button"
              role="menuitem"
              class="picker-item"
              onclick={() => pin(null)}>Use newest report</button
            >
          {:else}
            <button
              type="button"
              role="menuitem"
              class="picker-item"
              onclick={() => pin(selected.ref)}>Pin as dashboard</button
            >
          {/if}
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .picker {
    position: relative;
    display: inline-flex;
    min-width: 0;
  }
  .picker-trigger {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    max-width: 18rem;
    min-width: 0;
    padding: 2px 8px;
    border: 1px solid var(--line);
    border-radius: 999px;
    background: transparent;
    color: var(--fg);
    font-size: 12px;
    line-height: 1.6;
    cursor: pointer;
  }
  .picker-trigger:hover {
    border-color: var(--line-strong);
    background: var(--bg-soft);
  }
  .picker-trigger:focus-visible {
    outline: 2px solid var(--accent-solid);
    outline-offset: 1px;
  }
  .picker-value {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .picker-caret {
    flex: none;
    color: var(--fg-muted);
    font-size: 10px;
  }
  .picker-pinned {
    flex: none;
    padding: 0 5px;
    border: 1px solid var(--line);
    border-radius: 999px;
    color: var(--fg-muted);
    font-size: 10px;
    line-height: 1.5;
  }
  .picker-menu {
    position: absolute;
    right: 0;
    top: calc(100% + 4px);
    z-index: 50;
    min-width: 14rem;
    max-width: min(22rem, calc(100vw - 2rem));
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--panel);
    box-shadow: var(--shadow-menu);
  }
  .picker-list {
    /* Long lists scroll rather than running off the page. */
    max-height: 17rem;
    overflow-y: auto;
    margin: 0;
    padding: 4px;
    list-style: none;
  }
  .picker-item {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    padding: 5px 8px;
    border: 0;
    border-radius: 4px;
    background: none;
    color: var(--fg);
    font-size: 12px;
    text-align: left;
    cursor: pointer;
  }
  .picker-item:hover:not(:disabled) {
    background: var(--bg-soft);
  }
  .picker-item:disabled {
    color: var(--fg-muted);
    cursor: default;
  }
  .picker-item:focus-visible {
    outline: 2px solid var(--accent-solid);
    outline-offset: -2px;
  }
  .picker-item--current {
    color: var(--accent-text);
  }
  .picker-item--more,
  .picker-note {
    color: var(--fg-muted);
    font-size: 11px;
  }
  .picker-note {
    display: block;
    padding: 5px 8px;
  }
  .picker-check {
    flex: none;
    width: 0.75rem;
    color: var(--accent-text);
    font-size: 10px;
  }
  .picker-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .picker-actions {
    border-top: 1px solid var(--line-subtle);
    padding: 4px;
  }
</style>
