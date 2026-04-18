<script>
  import Button from "$lib/components/Button.svelte";

  let {
    title = "Nothing here yet",
    helper = "",
    actionLabel = "",
    actionHref = "",
    onclick = undefined,
    class: className = "",
  } = $props();
</script>

<div class="rounded-md border border-line bg-bg-soft px-6 py-8 text-center {className}">
  <h2 class="text-subtitle text-fg">{title}</h2>
  {#if helper}
    <p class="mx-auto mt-1.5 max-w-md text-meta text-fg-subtle">{helper}</p>
  {/if}
  {#if actionLabel && (actionHref || onclick)}
    <div class="mt-4">
      <Button
        variant="primary"
        href={actionHref || undefined}
        onclick={onclick}
      >
        {actionLabel}
      </Button>
    </div>
  {/if}
</div>
