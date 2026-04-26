<script>
  import Button from "$lib/components/Button.svelte";

  let {
    archived = false,
    busy = false,
    size = "sm",
    onarchive,
    onunarchive,
    /**
     * Visual variant. Default "archive" keeps the inbox/box copy + icon.
     * "resolve" relabels the same lifecycle action for surfaces (like
     * document text comments) where "Resolve" / "Reopen" is the natural
     * vocabulary, without changing the underlying `archived_at` semantics.
     */
    kind = "archive",
  } = $props();

  let iconSize = $derived(size === "md" ? "h-4 w-4" : "h-3.5 w-3.5");
  let isResolveKind = $derived(kind === "resolve");
</script>

{#if archived}
  <Button
    variant="secondary"
    size="compact"
    disabled={busy}
    onclick={onunarchive}
  >
    {isResolveKind ? "Reopen" : "Unarchive"}
  </Button>
{:else if isResolveKind}
  <Button
    variant="ghost"
    size="compact"
    disabled={busy}
    onclick={onarchive}
    title="Resolve"
    aria-label="Resolve"
  >
    <svg
      class={iconSize}
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      viewBox="0 0 24 24"
      aria-hidden="true"
    >
      <path
        stroke-linecap="round"
        stroke-linejoin="round"
        d="M9 12.75 11.25 15 15 9.75M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z"
      />
    </svg>
    Resolve
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
        d="M3.375 3C2.339 3 1.5 3.84 1.5 4.875v.75c0 1.036.84 1.875 1.875 1.875h17.25c1.035 0 1.875-.84 1.875-1.875v-.75C22.5 3.839 21.66 3 20.625 3H3.375Z"
      />
      <path
        fill-rule="evenodd"
        d="m3.087 9 .54 9.176A3 3 0 0 0 6.62 21h10.757a3 3 0 0 0 2.995-2.824L20.913 9H3.087Zm6.163 3.75A.75.75 0 0 1 10 12h4a.75.75 0 0 1 0 1.5h-4a.75.75 0 0 1-.75-.75Z"
        clip-rule="evenodd"
      />
    </svg>
  </Button>
{/if}
