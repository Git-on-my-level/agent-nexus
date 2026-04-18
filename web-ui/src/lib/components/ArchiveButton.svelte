<script>
  import Button from "$lib/components/Button.svelte";

  let {
    archived = false,
    busy = false,
    size = "sm",
    onarchive,
    onunarchive,
  } = $props();

  let iconSize = $derived(size === "md" ? "h-4 w-4" : "h-3.5 w-3.5");
</script>

{#if archived}
  <Button
    variant="secondary"
    size="compact"
    disabled={busy}
    onclick={onunarchive}
  >
    Unarchive
  </Button>
{:else}
  <Button
    variant="ghost"
    size="compact"
    disabled={busy}
    onclick={onarchive}
    title="Archive"
    aria-label="Archive"
  >
    <svg
      class={iconSize}
      fill="currentColor"
      viewBox="0 0 24 24"
      aria-hidden="true"
    >
      <path
        d="M20.25 7.5l-.625 10.632a2.25 2.25 0 01-2.247 2.118H6.622a2.25 2.25 0 01-2.247-2.118L3.75 7.5m8.25 3v6.75m0 0l-3-3m3 3l3-3M3.375 7.5h17.25c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125H3.375c-.621 0-1.125.504-1.125 1.125v-1.5c0 .621.504 1.125 1.125 1.125z"
      />
    </svg>
  </Button>
{/if}
