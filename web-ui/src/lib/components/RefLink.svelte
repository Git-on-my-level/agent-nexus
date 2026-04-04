<script>
  import { page } from "$app/stores";
  import { resolveRefLink } from "$lib/refLinkModel";

  let {
    refValue = "",
    threadId = "",
    boardId = "",
    snapshotIsThread = false,
    humanize = false,
    showRaw = false,
    labelHints = {},
  } = $props();

  let resolved = $derived(
    resolveRefLink(refValue, {
      threadId,
      boardId,
      snapshotIsThread,
      humanize,
      labelHints,
      workspaceSlug: $page.params.workspace,
    }),
  );
</script>

{#if resolved.isLink}
  <a
    class="inline-flex items-baseline gap-1 text-indigo-400 hover:text-indigo-300"
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
  <span
    class="inline-flex items-baseline gap-1 text-[11px] text-[var(--ui-text-muted)]"
  >
    <span>{resolved.primaryLabel}</span>
    {#if showRaw && resolved.secondaryLabel}
      <span class="text-[11px] text-gray-400">{resolved.secondaryLabel}</span>
    {/if}
  </span>
{/if}
