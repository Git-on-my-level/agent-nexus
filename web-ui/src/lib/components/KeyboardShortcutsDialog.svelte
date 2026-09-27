<script>
  /**
   * The `?` help overlay: a keyboard surface opened and closed with `?` (and
   * Esc). Focus moves into it on open and back to where the reader was on
   * close. The page owns the key handling; this renders the list.
   */
  let {
    open = $bindable(false),
    shortcuts = [],
    title = "Keyboard shortcuts",
  } = $props();

  let dialog = $state(null);
  let opener = null;
  let wasOpen = false;
  $effect(() => {
    if (open) {
      if (!wasOpen) opener = document.activeElement;
      wasOpen = true;
      dialog?.focus();
    } else if (wasOpen) {
      wasOpen = false;
      opener?.focus?.();
      opener = null;
    }
  });
</script>

{#if open}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <div
    bind:this={dialog}
    class="fixed inset-0 z-50 flex items-start justify-center bg-black/60 px-4 pt-[12vh] outline-none"
    role="dialog"
    aria-modal="true"
    aria-label={title}
    tabindex="-1"
    data-shortcut-help
    onclick={(event) => {
      if (event.target === event.currentTarget) open = false;
    }}
  >
    <div class="w-full max-w-sm rounded-md border border-line bg-panel p-4">
      <h2
        class="text-micro font-semibold uppercase tracking-wide text-fg-muted"
      >
        {title}
      </h2>
      <dl class="mt-3 space-y-1.5 text-meta">
        {#each shortcuts as [action, keys] (action)}
          <div class="flex items-baseline justify-between gap-6">
            <dt class="text-fg-muted">{action}</dt>
            <dd class="flex shrink-0 items-center gap-1">
              {#each keys as key, index (index)}
                {#if key === "–" || key === "…"}
                  <span class="text-micro text-fg-subtle">{key}</span>
                {:else}
                  <kbd
                    class="rounded border border-line bg-accent-soft px-1.5 py-0.5 font-mono text-micro text-accent-text"
                    >{key}</kbd
                  >
                {/if}
              {/each}
            </dd>
          </div>
        {/each}
      </dl>
    </div>
  </div>
{/if}
