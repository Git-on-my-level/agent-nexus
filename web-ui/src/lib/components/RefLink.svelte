<script>
  import { resolveRefLink } from "$lib/refLinkModel";

  let {
    refValue = "",
    threadId = "",
    snapshotIsThread = false,
    humanize = false,
    showRaw = false,
    labelHints = {},
  } = $props();

  let resolved = $derived(
    resolveRefLink(refValue, {
      threadId,
      snapshotIsThread,
      humanize,
      labelHints,
    }),
  );
</script>

{#if resolved.isLink}
  <a
    class="inline-flex items-baseline gap-1 text-indigo-600 hover:text-indigo-800"
    href={resolved.href}
    rel={resolved.isExternal ? "noreferrer noopener" : undefined}
    target={resolved.isExternal ? "_blank" : undefined}
  >
    <span>{resolved.primaryLabel}</span>
    {#if showRaw && resolved.secondaryLabel}
      <span class="text-[11px] text-gray-400">{resolved.secondaryLabel}</span>
    {/if}
  </a>
{:else}
  <span class="inline-flex items-baseline gap-1 text-xs text-gray-500">
    <span>{resolved.primaryLabel}</span>
    {#if showRaw && resolved.secondaryLabel}
      <span class="text-[11px] text-gray-400">{resolved.secondaryLabel}</span>
    {/if}
  </span>
{/if}
