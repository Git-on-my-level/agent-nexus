<script>
  /**
   * Trash icon button — triggers the move-to-trash action.
   *
   * size: 'sm' → p-1 / h-3.5 w-3.5  (list rows)
   * size: 'md' → p-1.5 / h-4 w-4    (detail headers)
   */
  let { busy = false, size = "sm", ontrash } = $props();

  let iconSize = $derived(size === "md" ? "h-4 w-4" : "h-3.5 w-3.5");
  let padding = $derived(size === "md" ? "p-1.5" : "p-1");
</script>

<button
  class="cursor-pointer rounded-md {padding} text-[var(--fg-muted)] transition-colors hover:bg-[var(--line)] hover:text-danger-text disabled:cursor-not-allowed disabled:opacity-50"
  disabled={busy}
  onclick={ontrash}
  title="Move to trash"
  aria-label="Move to trash"
  type="button"
>
  <svg
    class={iconSize}
    fill="none"
    viewBox="0 0 24 24"
    stroke="currentColor"
    stroke-width="2"
    aria-hidden="true"
  >
    <path
      stroke-linecap="round"
      stroke-linejoin="round"
      d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0"
    />
  </svg>
</button>
