<script>
  /**
   * Archive / Unarchive toggle button.
   *
   * When `archived` is false: renders an icon button that triggers the archive action.
   * When `archived` is true: renders a text "Unarchive" button.
   *
   * size: 'sm' → p-1 / h-3.5 w-3.5  (list rows)
   * size: 'md' → p-1.5 / h-4 w-4    (detail headers)
   */
  let {
    archived = false,
    busy = false,
    size = "sm",
    onarchive,
    onunarchive,
  } = $props();

  let iconSize = $derived(size === "md" ? "h-4 w-4" : "h-3.5 w-3.5");
  let padding = $derived(size === "md" ? "p-1.5" : "p-1");
</script>

{#if archived}
  <button
    class="cursor-pointer rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-2 py-1 text-[11px] font-medium text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border-subtle)] disabled:cursor-not-allowed disabled:opacity-50"
    disabled={busy}
    onclick={onunarchive}
    type="button"
  >
    Unarchive
  </button>
{:else}
  <button
    class="cursor-pointer rounded-md {padding} text-[var(--ui-text-muted)] transition-colors hover:bg-[var(--ui-border)] hover:text-[var(--ui-accent)] disabled:cursor-not-allowed disabled:opacity-50"
    disabled={busy}
    onclick={onarchive}
    title="Archive"
    type="button"
  >
    <svg
      class={iconSize}
      fill="currentColor"
      viewBox="0 0 24 24"
      aria-hidden="true"
    >
      <path
        d="M20.25 7.5l-.625 10.632a2.25 2.25 0 01-2.247 2.118H6.622a2.25 2.25 0 01-2.247-2.118L3.75 7.5m8.25 3v6.75m0 0l-3-3m3 3l3-3M3.375 7.5h17.25c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125H3.375c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125z"
      />
    </svg>
  </button>
{/if}
