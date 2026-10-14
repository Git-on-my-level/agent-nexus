<script>
  let message = $state("");
</script>

<svelte:window onlocal-save-error={(event) => (message = event.detail)} />
{#if message}
  <div
    class="fixed bottom-4 left-4 right-4 z-50 rounded-md border border-danger bg-panel p-3 text-meta text-fg"
    role="alert"
    data-local-save-warning
  >
    {message}
    <button
      type="button"
      class="ml-3 ui-btn-secondary"
      onclick={() => (message = "")}>Dismiss</button
    >
  </div>
{/if}
