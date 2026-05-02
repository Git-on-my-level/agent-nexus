<script>
  import { onMount, tick } from "svelte";

  import {
    clearContextMenu,
    registerContextMenu,
  } from "$lib/contextMenuSingleton.js";

  /**
   * @typedef {{ key: string, label: string, onSelect: () => void, danger?: boolean, disabled?: boolean }} CtxItem
   */

  /**
   * Right-click (desktop) or long-press ~550ms (mobile) to open a compact
   * menu. Use for destructive/secondary actions to keep the default toolbar minimal.
   */
  let { disabled = false, children, items = [] } = $props();

  const LONG_PRESS_MS = 550;
  const MOVE_CANCEL_PX = 10;

  const menuInstanceId = `ctx-${Math.random().toString(36).slice(2)}`;

  let open = $state(false);
  let pos = $state({ x: 0, y: 0 });
  /** @type {ReturnType<typeof setTimeout> | null} */
  let longPressTimer = null;
  let touchStartX = 0;
  let touchStartY = 0;

  /** @param {MouseEvent} e */
  function onContextMenu(e) {
    if (disabled || !hasItems) return;
    e.preventDefault();
    void openAt(e.clientX, e.clientY);
  }

  let hasItems = $derived(
    Array.isArray(items) && items.filter((i) => i && i.label).length > 0,
  );

  /**
   * @param {number} x
   * @param {number} y
   */
  async function openAt(x, y) {
    if (!hasItems) return;
    pos = clampToViewport(x, y);
    open = true;
    await tick();
    registerContextMenu(menuInstanceId, close);
    // Defer: avoid the same right-click / touch release closing the menu.
    requestAnimationFrame(() => {
      window.addEventListener("pointerdown", onDocPointer, true);
      window.addEventListener("keydown", onKey, true);
    });
  }

  /** @param {KeyboardEvent} e */
  function onKey(e) {
    if (e.key === "Escape") close();
  }

  /** @param {PointerEvent} e */
  function onDocPointer(e) {
    if (!open) return;
    const t = e.target;
    if (t instanceof Node && menuPanelRef?.contains(t)) {
      return;
    }
    close();
  }

  let ref = $state(/** @type {HTMLDivElement | null} */ (null));
  let menuPanelRef = $state(/** @type {HTMLDivElement | null} */ (null));

  /**
   * @param {number} x
   * @param {number} y
   */
  function clampToViewport(x, y) {
    const w = window.innerWidth;
    const h = window.innerHeight;
    const mw = 220;
    const mh = 200;
    const pad = 8;
    return {
      x: Math.max(pad, Math.min(x, w - mw - pad)),
      y: Math.max(pad, Math.min(y, h - mh - pad)),
    };
  }

  function close() {
    open = false;
    clearContextMenu(menuInstanceId);
    window.removeEventListener("pointerdown", onDocPointer, true);
    window.removeEventListener("keydown", onKey, true);
  }

  function clearLongPress() {
    if (longPressTimer) {
      clearTimeout(longPressTimer);
      longPressTimer = null;
    }
  }

  /** @param {TouchEvent} e */
  function onTouchStart(e) {
    if (disabled || !hasItems) return;
    if (e.touches.length !== 1) return;
    clearLongPress();
    touchStartX = e.touches[0].clientX;
    touchStartY = e.touches[0].clientY;
    const touch = e.touches[0];
    longPressTimer = setTimeout(() => {
      e.preventDefault();
      void openAt(touch.clientX, touch.clientY);
    }, LONG_PRESS_MS);
  }

  /** @param {TouchEvent} e */
  function onTouchMove(e) {
    if (longPressTimer && e.touches[0]) {
      const x = e.touches[0].clientX;
      const y = e.touches[0].clientY;
      if (
        Math.abs(x - touchStartX) > MOVE_CANCEL_PX ||
        Math.abs(y - touchStartY) > MOVE_CANCEL_PX
      ) {
        clearLongPress();
      }
    }
  }

  function onTouchEnd() {
    clearLongPress();
  }

  onMount(() => {
    return () => {
      clearLongPress();
      if (open) {
        window.removeEventListener("pointerdown", onDocPointer, true);
        window.removeEventListener("keydown", onKey, true);
        clearContextMenu(menuInstanceId);
      }
    };
  });
</script>

<div
  bind:this={ref}
  class="block min-h-0 min-w-0"
  role="group"
  oncontextmenu={onContextMenu}
  ontouchstart={onTouchStart}
  ontouchmove={onTouchMove}
  ontouchend={onTouchEnd}
  ontouchcancel={onTouchEnd}
>
  {@render children?.()}
  {#if open && hasItems}
    <div
      bind:this={menuPanelRef}
      class="anx-ctx-menu fixed z-[200] min-w-[9rem] rounded-md border border-line bg-panel py-0.5 text-micro shadow-md"
      style:left="{pos.x}px"
      style:top="{pos.y}px"
      role="menu"
    >
      {#each items as item (item.key)}
        <button
          type="button"
          class="block w-full cursor-pointer px-2.5 py-1.5 text-left text-meta hover:bg-bg-soft disabled:cursor-not-allowed disabled:opacity-50 {item.danger
            ? 'text-danger-text hover:text-danger-text'
            : 'text-fg'}"
          role="menuitem"
          disabled={item.disabled}
          onclick={() => {
            if (item.disabled) return;
            item.onSelect();
            close();
          }}
        >
          {item.label}
        </button>
      {/each}
    </div>
  {/if}
</div>
