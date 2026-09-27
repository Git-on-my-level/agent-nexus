<script>
  import {
    UNDO_WINDOW_MS,
    dismissInboxResponseToast,
    inboxResponseToast,
    retryInboxResponse,
  } from "$lib/inboxResponseQueue.js";
  import { formatShortcut } from "$lib/keyboardHints.js";

  /**
   * The one undo toast. A queued response waits here for a few seconds
   * before it is committed; Undo (or ⌘Z) hands it back to the page.
   */
  let { onUndo } = $props();

  let toast = $derived($inboxResponseToast);
</script>

{#if toast}
  <div
    class="pointer-events-none fixed inset-x-0 bottom-[calc(4.75rem+env(safe-area-inset-bottom,0px))] z-40 flex justify-center px-4 lg:bottom-6"
  >
    <div
      class="pointer-events-auto relative flex max-w-full items-center gap-3 overflow-hidden rounded-md border border-line-strong bg-panel px-3 py-2 text-meta text-fg shadow-[var(--shadow-menu)]"
      role={toast.state === "failed" ? "alert" : "status"}
      aria-live={toast.state === "failed" ? "assertive" : "polite"}
      data-inbox-toast={toast.state}
    >
      {#if toast.state === "failed"}
        <span class="min-w-0 [overflow-wrap:anywhere]"
          ><span class="text-danger-text">Not sent.</span>
          {toast.error || "The response could not be recorded."}</span
        >
        <button
          class="shrink-0 text-micro font-medium text-accent-text hover:underline"
          type="button"
          onclick={() => void retryInboxResponse()}>Retry</button
        >
        <button
          class="shrink-0 text-micro text-fg-muted hover:text-fg"
          type="button"
          onclick={dismissInboxResponseToast}>Dismiss</button
        >
      {:else}
        <span class="min-w-0 truncate"
          >{toast.state === "sent" ? `${toast.message}.` : toast.message}</span
        >
        {#if toast.state === "pending"}
          <button
            class="shrink-0 text-micro font-medium text-accent-text hover:underline"
            type="button"
            title={`Undo (${formatShortcut("Z")})`}
            onclick={() => onUndo?.()}>Undo</button
          >
          <kbd
            class="hidden shrink-0 rounded border border-line px-1 font-mono text-micro text-fg-subtle sm:inline"
            >{formatShortcut("Z")}</kbd
          >
          {#key toast.id}
            <span
              class="undo-clock absolute inset-x-0 bottom-0 h-px origin-left bg-accent"
              style={`animation-duration: ${Math.max(0, toast.deadline - Date.now()) || UNDO_WINDOW_MS}ms`}
              aria-hidden="true"
            ></span>
          {/key}
        {:else if toast.state === "sending"}
          <span class="shrink-0 text-micro text-fg-subtle">Sending…</span>
        {/if}
      {/if}
    </div>
  </div>
{/if}

<style>
  .undo-clock {
    animation-name: undo-clock;
    animation-timing-function: linear;
    animation-fill-mode: forwards;
  }
  @keyframes undo-clock {
    from {
      transform: scaleX(1);
    }
    to {
      transform: scaleX(0);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .undo-clock {
      animation: none;
    }
  }
</style>
