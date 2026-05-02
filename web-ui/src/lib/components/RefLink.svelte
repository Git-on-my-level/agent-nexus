<script>
  import { browser } from "$app/environment";
  import { page } from "$app/stores";
  import AttachmentChip from "$lib/components/AttachmentChip.svelte";
  import { isTrashedAttachmentMeta } from "$lib/attachmentDisplay.js";
  import { coreClient } from "$lib/coreClient";
  import { eventRouteForRef } from "$lib/deepLinkTargets";
  import { buildPrimitiveRefRoutes, resolveRefLink } from "$lib/refLinkModel";

  let {
    refValue = "",
    threadId = "",
    boardId = "",
    humanize = false,
    showRaw = false,
    labelHints = {},
    artifactRoutesById = {},
    eventRoutesById = {},
    attachmentOverlay = null,
    attachmentPending = false,
    attachmentUploadProgress = null,
    attachmentChipSize = "inline",
  } = $props();

  let fetchedEventRoutesById = $state({});
  let mergedEventRoutesById = $derived({
    ...eventRoutesById,
    ...fetchedEventRoutesById,
  });

  let resolved = $derived(
    resolveRefLink(refValue, {
      threadId,
      boardId,
      humanize,
      labelHints,
      artifactRoutesById,
      eventRoutesById: mergedEventRoutesById,
      workspaceSlug: $page.params.workspace,
      organizationSlug: $page.params.organization,
    }),
  );

  let useAttachmentChip = $derived(
    resolved.prefix === "artifact" &&
      (!resolved.routed || resolved.routedKind === "attachment"),
  );

  let hideTrashedAttachment = $derived(
    useAttachmentChip &&
      !attachmentPending &&
      isTrashedAttachmentMeta(resolved, attachmentOverlay),
  );

  $effect(() => {
    if (!browser || resolved.prefix !== "event" || resolved.routed) return;
    const eventId = String(resolved.value ?? "").trim();
    if (!eventId || fetchedEventRoutesById[eventId]) return;

    let cancelled = false;
    void eventRouteForRef(eventId, coreClient).then((event) => {
      if (cancelled || String(event?.type ?? "") !== "message_posted") return;
      fetchedEventRoutesById = {
        ...fetchedEventRoutesById,
        ...buildPrimitiveRefRoutes({ events: [event], threadId })
          .eventRoutesById,
      };
    });
    return () => {
      cancelled = true;
    };
  });
</script>

{#if useAttachmentChip && !hideTrashedAttachment}
  <span class="inline-flex max-w-full flex-col gap-0.5 align-baseline">
    <AttachmentChip
      {resolved}
      artifactOverlay={attachmentOverlay}
      pending={attachmentPending}
      uploadProgress={attachmentUploadProgress}
      size={attachmentChipSize}
    />
    {#if showRaw && resolved.secondaryLabel}
      <span class="text-micro text-[var(--fg-muted)]"
        >{resolved.secondaryLabel}</span
      >
    {/if}
  </span>
{:else if useAttachmentChip && hideTrashedAttachment}{:else if resolved.isLink}
  <a
    class="inline-flex items-baseline gap-1 text-accent-text hover:text-accent-text"
    href={resolved.href}
    rel={resolved.isExternal ? "noreferrer noopener" : undefined}
    target={resolved.isExternal ? "_blank" : undefined}
  >
    <span>{resolved.primaryLabel}</span>
    {#if showRaw && resolved.secondaryLabel}
      <span class="text-micro text-[var(--fg-muted)]"
        >{resolved.secondaryLabel}</span
      >
    {/if}
  </a>
{:else}
  <span
    class="inline-flex items-baseline gap-1 text-micro text-[var(--fg-muted)]"
  >
    <span>{resolved.primaryLabel}</span>
    {#if showRaw && resolved.secondaryLabel}
      <span class="text-micro text-[var(--fg-muted)]"
        >{resolved.secondaryLabel}</span
      >
    {/if}
  </span>
{/if}
