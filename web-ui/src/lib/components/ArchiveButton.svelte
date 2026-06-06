<script>
  import Button from "$lib/components/Button.svelte";
  import Icon from "$lib/components/Icon.svelte";

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
    class={isResolveKind
      ? "!h-7 !min-w-0 !border-0 !bg-transparent !px-1.5 hover:!bg-bg-soft"
      : ""}
    disabled={busy}
    onclick={onunarchive}
    title={isResolveKind ? "Reopen" : "Unarchive"}
    aria-label={isResolveKind ? "Reopen" : "Unarchive"}
  >
    {#if isResolveKind}
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
          d="M9 15L3 9m0 0l6-6M3 9h12a6 6 0 0 1 0 12h-3"
        />
      </svg>
    {:else}
      Unarchive
    {/if}
  </Button>
{:else if isResolveKind}
  <Button
    variant="ghost"
    size="compact"
    class="!h-7 !min-w-0 !px-1.5"
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
    <Icon name="archive" class={iconSize} />
  </Button>
{/if}
