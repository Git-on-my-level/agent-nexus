<script>
  import { browser } from "$app/environment";
  import { page } from "$app/stores";
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
  const rootClass =
    "compact-ref-link inline-flex min-w-0 max-w-full items-baseline gap-1 rounded border border-[var(--line)] bg-[var(--bg)] px-1.5 py-0.5 text-micro leading-tight";
</script>

{#if resolved.isLink}
  <a
    class="{rootClass} text-accent-text hover:border-[var(--line-strong)] hover:text-accent-text"
    href={resolved.href}
    rel={resolved.isExternal ? "noreferrer noopener" : undefined}
    target={resolved.isExternal ? "_blank" : undefined}
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
  </a>
{:else}
  <span class="{rootClass} text-[var(--fg-muted)]" title={resolved.raw}>
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
  </span>
{/if}
