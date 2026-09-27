<script>
  /**
   * Whether the host bridge is checked in, as a secondary fact beside an
   * agent (never its state). Online: @mentions wake the agent now. Offline:
   * mentions wait until the bridge checks in or the agent reads them.
   */
  let { online = false, showLabel = false } = $props();

  let label = $derived(online ? "Bridge online" : "Bridge offline");
  let help = $derived(
    online
      ? "Bridge online: tagging this agent wakes it now."
      : "Bridge offline: tags wait until the host bridge checks in.",
  );
</script>

<span
  class="inline-flex shrink-0 items-center gap-1 {online
    ? 'text-ok-text'
    : 'text-fg-subtle'}"
  title={help}
  data-agent-bridge={online ? "online" : "offline"}
>
  <svg
    class="h-3 w-3"
    fill="none"
    viewBox="0 0 24 24"
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    {#if online}
      <path d="M5 12.5a10 10 0 0114 0M8.5 16a5 5 0 017 0M12 19.5h.01" />
    {:else}
      <path
        d="M3 3l18 18M8.5 16a5 5 0 017 0M12 19.5h.01M5 12.5a10 10 0 014.2-2.6M14.8 9.9a10 10 0 014.2 2.6"
      />
    {/if}
  </svg>
  {#if showLabel}
    <span>{label}</span>
  {:else}
    <span class="sr-only">{label}</span>
  {/if}
</span>
