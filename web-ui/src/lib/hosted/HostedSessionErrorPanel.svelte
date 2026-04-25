<script>
  import { env } from "$env/dynamic/public";

  import Button from "$lib/components/Button.svelte";
  import {
    resolveHostedSupportUrl,
    supportLinkOpensInNewTab,
  } from "$lib/hosted/supportLink.js";

  let {
    title = "We couldn't load your account",
    message = "",
    onretry,
    retrying = false,
    onsignout,
    class: className = "",
  } = $props();

  const supportHref = resolveHostedSupportUrl(env.PUBLIC_ANX_SUPPORT_URL);
  const supportExternal = supportLinkOpensInNewTab(supportHref);
</script>

<div
  role="alert"
  class="rounded-md border border-line bg-bg-soft px-5 py-5 {className}"
>
  <h2 class="text-subtitle font-medium text-fg">{title}</h2>
  {#if message}
    <p class="mt-2 text-body text-danger-text">{message}</p>
  {/if}
  <p class="mt-3 text-meta text-fg-subtle">
    If this keeps happening, <a
      class="text-accent-text underline-offset-2 hover:underline"
      href={supportHref}
      {...supportExternal ? { target: "_blank", rel: "noreferrer" } : {}}
      >contact support</a
    >.
  </p>
  <div class="mt-4 flex flex-wrap gap-2">
    {#if onretry}
      <Button
        variant="primary"
        onclick={onretry}
        busy={retrying}
        disabled={retrying}
      >
        {retrying ? "Retrying…" : "Retry"}
      </Button>
    {/if}
    {#if onsignout}
      <Button variant="secondary" onclick={onsignout}>Sign out</Button>
    {/if}
  </div>
</div>
