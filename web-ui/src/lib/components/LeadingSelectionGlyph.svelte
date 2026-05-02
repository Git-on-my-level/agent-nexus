<script>
  /** Compact selected vs idle indicator for list select mode (not a checkbox). */
  let { selected = false } = $props();
</script>

{#if selected}
  <span
    class="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-accent-solid text-white shadow-sm"
    aria-hidden="true"
  >
    <svg
      class="h-3 w-3"
      fill="none"
      viewBox="0 0 24 24"
      stroke="currentColor"
      stroke-width="3"
    >
      <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
    </svg>
  </span>
{:else}
  <span
    class="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full border-2 border-line bg-bg-soft"
    aria-hidden="true"
  ></span>
{/if}
