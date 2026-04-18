<script>
  import { page } from "$app/stores";
  import { resolveRefLink } from "$lib/refLinkModel";

  let {
    refValue = "",
    threadId = "",
    boardId = "",
    humanize = false,
    showRaw = false,
    labelHints = {},
  } = $props();

  let resolved = $derived(
    resolveRefLink(refValue, {
      threadId,
      boardId,
      humanize,
      labelHints,
      workspaceSlug: $page.params.workspace,
    }),
  );
</script>

{#if resolved.isLink}
  <a
    class="inline-flex items-baseline gap-1 text-accent-text hover:text-accent-text"
    href={resolved.href}
    rel={resolved.isExternal ? "noreferrer noopener" : undefined}
    target={resolved.isExternal ? "_blank" : undefined}
  >
    <span>{resolved.primaryLabel}</span>
    {#if showRaw && resolved.secondaryLabel}
      <span class="text-[11px] text-fg-subtle">{resolved.secondaryLabel}</span>
    {/if}
  </a>
{:else}
  <span
    class="inline-flex items-baseline gap-1 text-[11px] text-[var(--fg-muted)]"
  >
    <span>{resolved.primaryLabel}</span>
    {#if showRaw && resolved.secondaryLabel}
      <span class="text-[11px] text-fg-subtle">{resolved.secondaryLabel}</span>
    {/if}
  </span>
{/if}
