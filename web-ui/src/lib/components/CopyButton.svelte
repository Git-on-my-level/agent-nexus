<script>
  let { value = "", label = "Copy", size = "sm" } = $props();

  let copied = $state(false);
  let timer;

  async function onCopy() {
    try {
      await navigator.clipboard.writeText(String(value ?? ""));
      copied = true;
      clearTimeout(timer);
      timer = setTimeout(() => (copied = false), 1400);
    } catch {
      // clipboard unavailable; swallow
    }
  }

  let padding = $derived(size === "md" ? "px-2 py-1" : "px-1.5 py-0.5");
</script>

<button
  type="button"
  class="cursor-pointer inline-flex items-center gap-1 rounded {padding} text-micro font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line)] hover:text-[var(--fg)]"
  onclick={onCopy}
  title={copied ? "Copied" : label}
  aria-label={label}
>
  {#if copied}
    <svg
      class="h-3 w-3"
      fill="none"
      viewBox="0 0 24 24"
      stroke="currentColor"
      stroke-width="2.5"
      aria-hidden="true"
    >
      <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
    </svg>
    Copied
  {:else}
    <svg
      class="h-3 w-3"
      fill="none"
      viewBox="0 0 24 24"
      stroke="currentColor"
      stroke-width="2"
      aria-hidden="true"
    >
      <path
        stroke-linecap="round"
        stroke-linejoin="round"
        d="M8 5H6a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2v-2M8 5a2 2 0 002 2h4a2 2 0 002-2M8 5a2 2 0 012-2h4a2 2 0 012 2m0 0h2a2 2 0 012 2v3"
      />
    </svg>
    Copy
  {/if}
</button>
