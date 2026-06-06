<script>
  import Button from "$lib/components/Button.svelte";
  import { copyText } from "$lib/clipboard.js";

  let {
    value = "",
    label = "Copy",
    size = "sm",
    /** Icon + checkmark only — no "Copy"/"Copied" text (compact toolbars). */
    iconOnly = false,
    icon = "copy",
  } = $props();

  let copied = $state(false);
  let timer;

  async function onCopy() {
    if (await copyText(value)) {
      copied = true;
      clearTimeout(timer);
      timer = setTimeout(() => (copied = false), 1400);
    }
  }

  let btnSize = $derived(size === "md" ? "default" : "compact");
  let iconBtnClass = $derived(
    iconOnly ? "!h-6 !min-h-0 !w-6 !min-w-0 !px-0" : "",
  );
</script>

<Button
  class="shrink-0 {iconBtnClass}"
  variant="ghost"
  size={btnSize}
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
    {#if !iconOnly}
      Copied
    {/if}
  {:else if icon === "link"}
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
        d="M13.19 8.688a4.5 4.5 0 011.242 7.244l-4.5 4.5a4.5 4.5 0 01-6.364-6.364l1.757-1.757m13.35-.709l1.414-1.414a4.5 4.5 0 00-6.364-6.364l-4.5 4.5a4.5 4.5 0 001.242 7.244"
      />
    </svg>
    {#if !iconOnly}
      Copy
    {/if}
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
        d="M8 5H6a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2v-2M8 5a2 2 0 012-2h4a2 2 0 012 2M8 5a2 2 0 002 2h4a2 2 0 002-2M8 5a2 2 0 012-2h4a2 2 0 012 2m0 0h2a2 2 0 012 2v3"
      />
    </svg>
    {#if !iconOnly}
      Copy
    {/if}
  {/if}
</Button>
