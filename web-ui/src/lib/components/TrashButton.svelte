<script>
  import Button from "$lib/components/Button.svelte";
  import Icon from "$lib/components/Icon.svelte";

  let { busy = false, size = "sm", ontrash } = $props();

  let iconSize = $derived(size === "md" ? "h-4 w-4" : "h-3.5 w-3.5");
</script>

<Button
  variant="ghost"
  size="compact"
  class="text-fg-muted hover:text-danger-text hover:bg-danger-soft"
  disabled={busy}
  onclick={ontrash}
  title="Move to trash"
  aria-label="Move to trash"
>
  <Icon name="trash" class={iconSize} />
</Button>
