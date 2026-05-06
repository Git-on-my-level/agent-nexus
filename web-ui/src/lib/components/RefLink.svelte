<script>
  import { browser } from "$app/environment";
  import { page } from "$app/stores";
  import AttachmentChip from "$lib/components/AttachmentChip.svelte";
  import RefChip from "$lib/components/RefChip.svelte";
  import { isTrashedAttachmentMeta } from "$lib/attachmentDisplay.js";
  import { coreClient } from "$lib/coreClient";
  import { isInternalUuid } from "$lib/resourceIdentity.js";
  import { eventRouteForRef } from "$lib/deepLinkTargets";
  import { buildPrimitiveRefRoutes, resolveRefLink } from "$lib/refLinkModel";

  let {
    refValue = "",
    threadId = "",
    boardId = "",
    /** Ignored when `variant === 'compact'`; compact defaults to true. */
    humanize = undefined,
    /** Ignored when `variant === 'compact'`; compact defaults to true. */
    showRaw = undefined,
    labelHints = {},
    artifactRoutesById = {},
    eventRoutesById = {},
    attachmentOverlay = null,
    attachmentPending = false,
    attachmentUploadProgress = null,
    /** @type {'inline' | 'compact' | 'block' | 'tight'} */
    attachmentChipSize = "inline",
    /** `default`: plain link / attachment chip. `compact`: RefChip + mobile labels. */
    variant = "default",
    /** Ref sits in a grouped row with an adjacent control (strip inner chrome). Compact only. */
    composerRowEmbed = false,
  } = $props();

  let effectiveHumanize = $derived(
    humanize !== undefined ? humanize : variant === "compact",
  );
  let effectiveShowRaw = $derived(
    showRaw !== undefined ? showRaw : variant === "compact",
  );

  let fetchedEventRoutesById = $state({});
  let mergedEventRoutesById = $derived({
    ...eventRoutesById,
    ...fetchedEventRoutesById,
  });

  let resolved = $derived(
    resolveRefLink(refValue, {
      threadId,
      boardId,
      humanize: effectiveHumanize,
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

  // Compact a ref value for mobile display; handles are short enough to show
  // as-is, but legacy UUID values still get truncated to 10 chars.
  function compactId(value) {
    const text = String(value ?? "").trim();
    if (!text || isInternalUuid(text)) return "";
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
    resolved.prefix && resolved.value && compactId(resolved.value)
      ? `${resolved.prefix}:${compactId(resolved.value)}`
      : resolved.raw,
  );
</script>

{#if variant === "compact"}
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
      <span class="compact-ref-link__mobile min-w-0 truncate"
        >{mobileLabel}</span
      >
      {#if effectiveShowRaw && resolved.secondaryLabel}
        <span
          class="compact-ref-link__raw min-w-0 truncate font-mono text-fg-muted"
          >{resolved.secondaryLabel}</span
        >
        <span
          class="compact-ref-link__mobile-raw min-w-0 truncate font-mono text-fg-muted"
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
      <span class="compact-ref-link__mobile min-w-0 truncate"
        >{mobileLabel}</span
      >
      {#if effectiveShowRaw && resolved.secondaryLabel}
        <span
          class="compact-ref-link__raw min-w-0 truncate font-mono text-fg-muted"
          >{resolved.secondaryLabel}</span
        >
        <span
          class="compact-ref-link__mobile-raw min-w-0 truncate font-mono text-fg-muted"
          >{mobileRaw}</span
        >
      {/if}
    </RefChip>
  {/if}
{:else if useAttachmentChip && !hideTrashedAttachment}
  <span class="inline-flex max-w-full flex-col gap-0.5 align-baseline">
    <AttachmentChip
      {resolved}
      artifactOverlay={attachmentOverlay}
      pending={attachmentPending}
      uploadProgress={attachmentUploadProgress}
      size={attachmentChipSize}
    />
    {#if effectiveShowRaw && resolved.secondaryLabel}
      <span class="text-micro text-fg-muted">{resolved.secondaryLabel}</span>
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
    {#if effectiveShowRaw && resolved.secondaryLabel}
      <span class="text-micro text-fg-muted">{resolved.secondaryLabel}</span>
    {/if}
  </a>
{:else}
  <span class="inline-flex items-baseline gap-1 text-micro text-fg-muted">
    <span>{resolved.primaryLabel}</span>
    {#if effectiveShowRaw && resolved.secondaryLabel}
      <span class="text-micro text-fg-muted">{resolved.secondaryLabel}</span>
    {/if}
  </span>
{/if}
