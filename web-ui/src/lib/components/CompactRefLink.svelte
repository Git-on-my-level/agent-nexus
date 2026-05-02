<script>
  import { browser } from "$app/environment";
  import { page } from "$app/stores";
  import AttachmentChip from "$lib/components/AttachmentChip.svelte";
  import RefChip from "$lib/components/RefChip.svelte";
  import { isTrashedAttachmentMeta } from "$lib/attachmentDisplay.js";
  import { coreClient } from "$lib/coreClient";
  import { eventRouteForRef } from "$lib/deepLinkTargets";
  import { buildPrimitiveRefRoutes, resolveRefLink } from "$lib/refLinkModel";

  let {
    refValue = "",
    threadId = "",
    boardId = "",
    humanize = true,
    showRaw = true,
    labelHints = {},
    artifactRoutesById = {},
    eventRoutesById = {},
    attachmentOverlay = null,
    attachmentPending = false,
    attachmentUploadProgress = null,
    /** @type {'inline' | 'compact' | 'block' | 'tight'} */
    attachmentChipSize = "inline",
    /** Ref sits in a grouped row with an adjacent control (strip inner chrome). */
    composerRowEmbed = false,
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

  function compactId(value) {
    const text = String(value ?? "").trim();
    if (text.length <= 12) return text;
    return text.slice(0, 10);
  }

  function compactLabel(link) {
    const routedKind = String(link?.routedKind ?? "").trim();
    if (link?.routed && routedKind === "attachment") {
      const primary = String(link?.primaryLabel ?? "").trim();
      if (primary) {
        const max = 44;
        return primary.length > max ? `${primary.slice(0, max - 1)}…` : primary;
      }
    }

    const prefix = String(link?.prefix ?? "").trim();
    const value = compactId(link?.value);
    if (!prefix || !value) return String(link?.primaryLabel ?? link?.raw ?? "");

    const nounByPrefix = {
      artifact: "Artifact",
      board: "Board",
      card: "Card",
      document: "Doc",
      document_revision: "Doc rev",
      thread: "Thread",
      topic: "Topic",
    };
    const noun = nounByPrefix[prefix];
    return noun ? `${noun} ${value}` : `${prefix}:${value}`;
  }

  let mobileLabel = $derived(compactLabel(resolved));
  let mobileRaw = $derived(
    resolved.prefix && resolved.value
      ? `${resolved.prefix}:${compactId(resolved.value)}`
      : resolved.raw,
  );
</script>

{#if useAttachmentChip && !hideTrashedAttachment}
  <AttachmentChip
    {resolved}
    artifactOverlay={attachmentOverlay}
    pending={attachmentPending}
    uploadProgress={attachmentUploadProgress}
    size={attachmentChipSize}
    groupedInRow={composerRowEmbed}
  />
{:else if useAttachmentChip && hideTrashedAttachment}{:else if resolved.isLink}
  <RefChip
    embedded={composerRowEmbed}
    href={resolved.href}
    external={resolved.isExternal}
    title={resolved.raw}
    navigable={true}
    accentText={true}
  >
    <span class="compact-ref-link__full min-w-0 truncate"
      >{resolved.primaryLabel}</span
    >
    <span class="compact-ref-link__mobile min-w-0 truncate">{mobileLabel}</span>
    {#if showRaw && resolved.secondaryLabel}
      <span
        class="compact-ref-link__raw min-w-0 truncate font-mono text-[var(--fg-muted)]"
        >{resolved.secondaryLabel}</span
      >
      <span
        class="compact-ref-link__mobile-raw min-w-0 truncate font-mono text-[var(--fg-muted)]"
        >{mobileRaw}</span
      >
    {/if}
  </RefChip>
{:else}
  <RefChip
    embedded={composerRowEmbed}
    href=""
    navigable={false}
    accentText={false}
    title={resolved.raw}
  >
    <span class="compact-ref-link__full min-w-0 truncate"
      >{resolved.primaryLabel}</span
    >
    <span class="compact-ref-link__mobile min-w-0 truncate">{mobileLabel}</span>
    {#if showRaw && resolved.secondaryLabel}
      <span
        class="compact-ref-link__raw min-w-0 truncate font-mono text-[var(--fg-muted)]"
        >{resolved.secondaryLabel}</span
      >
      <span
        class="compact-ref-link__mobile-raw min-w-0 truncate font-mono text-[var(--fg-muted)]"
        >{mobileRaw}</span
      >
    {/if}
  </RefChip>
{/if}
