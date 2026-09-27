<script>
  import CopyButton from "$lib/components/CopyButton.svelte";
  import { shortIdLabel } from "$lib/inboxMailbox.js";

  /**
   * A requester or responder by name. When only an identifier is known, show
   * a short readable stand-in and keep the full id one click away, instead of
   * a monospace UUID in the sentence.
   */
  let { name = "", id = "", class: extraClass = "" } = $props();

  let label = $derived(String(name ?? "").trim());
  let rawId = $derived(
    String(id ?? "")
      .trim()
      .replace(/^actor:/, ""),
  );
</script>

{#if label}
  <span class="font-medium text-fg {extraClass}">{label}</span>
{:else if rawId}
  <span class="inline-flex items-center gap-0.5 align-baseline {extraClass}">
    <span class="text-fg" title={rawId}>{shortIdLabel(rawId)}</span>
    <CopyButton value={rawId} label="Copy id" iconOnly title="Copy full id" />
  </span>
{:else}
  <span class="text-fg-muted {extraClass}">someone</span>
{/if}
