<script>
  import Button from "$lib/components/Button.svelte";

  let {
    message = "Something went wrong.",
    onretry,
    retrying = false,
    class: className = "",
  } = $props();
</script>

<div role="alert" class="rounded-md bg-danger-soft px-4 py-3 {className}">
  <p class="text-body text-danger-text">{message}</p>
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
