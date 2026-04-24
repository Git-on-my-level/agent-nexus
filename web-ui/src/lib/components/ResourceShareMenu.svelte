<script>
  import { browser } from "$app/environment";
  import { onMount } from "svelte";

  import Button from "$lib/components/Button.svelte";

  /**
   * @typedef {Record<string, unknown> | null | undefined} JsonRecord
   */

  let {
    /** Resource id shown in the page URL / API (topic id, document id, board id, card id, …). */
    resourceId = "",
    /** Serialized with JSON.stringify for "Copy raw JSON". */
    rawRecord = /** @type {JsonRecord} */ (null),
    /** Head revision content hash (document detail only). */
    contentHash = "",
    /** When set, used instead of `window.location.href` for Share. */
    shareUrl = "",
  } = $props();

  let menuOpen = $state(false);
  let shareCopied = $state(false);
  /**
   * Most-recent menu item that flashed "Copied". Per polish §N2 we surface a
   * brief visible label change inside the menu — `aria-live` text alone is
   * easy to miss.
   */
  let menuItemCopied = $state(/** @type {""|"id"|"hash"|"json"} */ (""));
  let liveStatus = $state("");
  let shareTimer;
  let menuItemTimer;
  let statusTimer;
  let rootEl;

  function flashStatus(msg) {
    liveStatus = msg;
    clearTimeout(statusTimer);
    statusTimer = setTimeout(() => {
      liveStatus = "";
    }, 1600);
  }

  function flashMenuItem(/** @type {"id"|"hash"|"json"} */ which) {
    menuItemCopied = which;
    clearTimeout(menuItemTimer);
    menuItemTimer = setTimeout(() => {
      menuItemCopied = "";
    }, 1400);
  }

  function effectiveShareUrl() {
    const fromProp = String(shareUrl ?? "").trim();
    if (fromProp) return fromProp;
    if (browser) return window.location.href;
    return "";
  }

  async function copyShareLink() {
    const url = effectiveShareUrl();
    if (!url) return;
    try {
      await navigator.clipboard.writeText(url);
      shareCopied = true;
      flashStatus("Link copied");
      clearTimeout(shareTimer);
      shareTimer = setTimeout(() => {
        shareCopied = false;
      }, 1400);
    } catch {
      flashStatus("Could not copy link");
    }
  }

  async function copyId() {
    const id = String(resourceId ?? "").trim();
    if (!id) return;
    try {
      await navigator.clipboard.writeText(id);
      flashStatus("ID copied");
      flashMenuItem("id");
    } catch {
      flashStatus("Could not copy ID");
    }
  }

  async function copyHash() {
    const h = String(contentHash ?? "").trim();
    if (!h) return;
    try {
      await navigator.clipboard.writeText(h);
      flashStatus("Content hash copied");
      flashMenuItem("hash");
    } catch {
      flashStatus("Could not copy hash");
    }
  }

  async function copyJson() {
    if (rawRecord == null || typeof rawRecord !== "object") return;
    try {
      await navigator.clipboard.writeText(JSON.stringify(rawRecord, null, 2));
      flashStatus("JSON copied");
      flashMenuItem("json");
    } catch {
      flashStatus("Could not copy JSON");
    }
  }

  function toggleMenu() {
    menuOpen = !menuOpen;
  }

  onMount(() => {
    function onDocPointerDown(/** @type {PointerEvent} */ e) {
      if (!menuOpen || !rootEl) return;
      if (e.target instanceof Node && !rootEl.contains(e.target)) {
        menuOpen = false;
      }
    }
    document.addEventListener("pointerdown", onDocPointerDown, true);
    return () =>
      document.removeEventListener("pointerdown", onDocPointerDown, true);
  });

  let idCopyable = $derived(Boolean(String(resourceId ?? "").trim()));
  let hashCopyable = $derived(Boolean(String(contentHash ?? "").trim()));
  let jsonCopyable = $derived(
    rawRecord != null && typeof rawRecord === "object",
  );
  /**
   * Per polish §P2 the kebab uses hide-when-absent semantics for every menu
   * item — if none would render, the kebab itself is suppressed so we never
   * show a button that opens an empty menu.
   */
  let hasMenuItems = $derived(idCopyable || hashCopyable || jsonCopyable);
</script>

<div
  bind:this={rootEl}
  class="relative inline-flex shrink-0 items-center gap-1"
>
  <span class="sr-only" aria-live="polite">{liveStatus}</span>
  <Button
    variant="secondary"
    size="compact"
    onclick={() => void copyShareLink()}
    aria-label={shareCopied ? "Link copied" : "Copy link to this page"}
  >
    {shareCopied ? "Copied" : "Share"}
  </Button>
  {#if hasMenuItems}
    <div class="relative">
      <button
        type="button"
        class="inline-flex h-7 w-7 cursor-pointer items-center justify-center rounded-md border border-line bg-transparent text-fg-muted transition-colors hover:bg-panel-hover hover:text-fg disabled:cursor-not-allowed disabled:opacity-50"
        aria-label="More copy options"
        aria-expanded={menuOpen}
        aria-haspopup="menu"
        onclick={toggleMenu}
      >
        <svg
          class="h-4 w-4"
          fill="currentColor"
          viewBox="0 0 24 24"
          aria-hidden="true"
        >
          <circle cx="12" cy="5" r="1.75" />
          <circle cx="12" cy="12" r="1.75" />
          <circle cx="12" cy="19" r="1.75" />
        </svg>
      </button>
      {#if menuOpen}
        <div
          class="absolute right-0 z-50 mt-1 min-w-[11rem] rounded-md border border-[var(--line)] bg-[var(--panel)] py-1 shadow-lg"
          role="menu"
        >
          {#if idCopyable}
            <button
              type="button"
              role="menuitem"
              class="block w-full px-3 py-2 text-left text-micro text-[var(--fg)] hover:bg-[var(--line-subtle)]"
              onclick={() => void copyId()}
            >
              {menuItemCopied === "id" ? "Copied" : "Copy ID"}
            </button>
          {/if}
          {#if hashCopyable}
            <button
              type="button"
              role="menuitem"
              class="block w-full px-3 py-2 text-left text-micro text-[var(--fg)] hover:bg-[var(--line-subtle)]"
              onclick={() => void copyHash()}
            >
              {menuItemCopied === "hash" ? "Copied" : "Copy content hash"}
            </button>
          {/if}
          {#if jsonCopyable}
            <button
              type="button"
              role="menuitem"
              class="block w-full px-3 py-2 text-left text-micro text-[var(--fg)] hover:bg-[var(--line-subtle)]"
              onclick={() => void copyJson()}
            >
              {menuItemCopied === "json" ? "Copied" : "Copy raw JSON"}
            </button>
          {/if}
        </div>
      {/if}
    </div>
  {/if}
</div>
