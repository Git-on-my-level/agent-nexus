<script>
  /**
   * The shell's state when opening a workspace failed: an outage offers
   * Retry, a workspace that does not exist offers the way back to the
   * workspace list instead (Retry cannot help).
   */
  let {
    message,
    notFound = false,
    pending = false,
    onRetry,
    homeHref,
  } = $props();
</script>

<div
  class="p-6"
  role="alert"
  data-workspace-activation-error
  data-workspace-not-found={notFound ? "" : undefined}
>
  <p>{message}</p>
  {#if notFound}
    <a class="ui-btn-secondary mt-3 inline-block" href={homeHref}
      >Go to your workspaces</a
    >
  {:else}
    <button
      class="ui-btn-secondary mt-3"
      type="button"
      disabled={pending}
      onclick={() => onRetry?.()}
    >
      {pending ? "Retrying…" : "Retry"}
    </button>
  {/if}
</div>
