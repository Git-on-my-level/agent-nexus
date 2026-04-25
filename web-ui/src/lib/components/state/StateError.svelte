<script>
  import { PUBLIC_ANX_SUPPORT_URL } from "$env/static/public";

  import Button from "$lib/components/Button.svelte";
  import {
    resolveHostedSupportUrl,
    supportLinkOpensInNewTab,
  } from "$lib/hosted/supportLink.js";

  let {
    title = "",
    message = "This didn't load.",
    onretry,
    retrying = false,
    supportHint = false,
    class: className = "",
  } = $props();

  const supportHref = resolveHostedSupportUrl(PUBLIC_ANX_SUPPORT_URL);
  const supportExternal = supportLinkOpensInNewTab(supportHref);
</script>

<div role="alert" class="rounded-md bg-danger-soft px-4 py-3 {className}">
  {#if title}
    <p class="text-subtitle font-medium text-danger-text">{title}</p>
  {/if}
  <p class="text-body text-danger-text {title ? 'mt-1' : ''}">{message}</p>
  {#if supportHint}
    <p class="mt-2 text-meta text-danger-text/90">
      Need help? <a
        class="font-medium underline-offset-2 hover:underline"
        href={supportHref}
        {...supportExternal ? { target: "_blank", rel: "noreferrer" } : {}}
        >Contact Agent Nexus support</a
      >.
    </p>
  {/if}
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
