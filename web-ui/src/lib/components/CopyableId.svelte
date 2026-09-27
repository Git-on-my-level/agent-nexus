<script>
  import CopyButton from "$lib/components/CopyButton.svelte";
  import { shortIdLabel } from "$lib/inboxMailbox.js";

  /**
   * An identifier kept out of the reading line: a short stand-in, the full
   * value on hover, and a copy button. Names lead; ids are one click away.
   */
  let { value = "", label = "Copy id", class: extraClass = "" } = $props();

  let raw = $derived(String(value ?? "").trim());
</script>

{#if raw}
  <span
    class="inline-flex min-w-0 items-center gap-0.5 align-baseline {extraClass}"
  >
    <span class="truncate font-mono text-micro text-fg-muted" title={raw}
      >{shortIdLabel(raw)}</span
    >
    <CopyButton value={raw} {label} iconOnly title={`${label}: ${raw}`} />
  </span>
{/if}
