<script>
  import Button from "$lib/components/Button.svelte";

  let {
    title = "",
    message = "This didn't load.",
    onretry,
    retrying = false,
    class: className = "",
  } = $props();
</script>

<div role="alert" class="rounded-md bg-danger-soft px-4 py-3 {className}">
  {#if title}
    <p class="text-subtitle font-medium text-danger-text">{title}</p>
  {/if}
  <p class="text-body text-danger-text {title ? 'mt-1' : ''}">{message}</p>
  {#if onretry}
    <Button
      variant="secondary"
      size="compact"
      onclick={onretry}
      busy={retrying}
      disabled={retrying}
      class="mt-2"
    >
      {retrying ? "Retrying…" : "Retry"}
    </Button>
  {/if}
</div>
