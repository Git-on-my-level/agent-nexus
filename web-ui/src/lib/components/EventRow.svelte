<script>
  import {
    lookupActorDisplayName,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import ActorLabel from "$lib/components/ActorLabel.svelte";
  import { formatTimestamp } from "$lib/formatDate";

  let { row } = $props();

  let actorLine = $derived(
    lookupActorDisplayName(row?.actorId, $actorRegistry, $principalRegistry) ||
      row?.actorId ||
      "System",
  );
</script>

<a
  id={row?.id}
  class="group/row relative block px-3 py-2.5 transition-colors hover:bg-panel-hover sm:px-4"
  href={row?.href || "#"}
>
  <div class="min-w-0">
    <div class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5">
      <ActorLabel
        label={actorLine}
        seed={String(row?.actorId ?? actorLine)}
        size="xs"
        truncate={false}
        nameClass="truncate text-meta font-medium text-fg group-hover/row:text-accent-text transition-colors"
      />
      <span class="text-micro text-fg-muted">·</span>
      <span class="text-micro font-medium text-fg-muted">
        {formatTimestamp(row?.ts) || "—"}
      </span>
      <span class="text-micro text-fg-muted">·</span>
      <span class="text-micro font-medium text-fg">{row?.label}</span>
    </div>
    {#if row?.detail}
      <p class="mt-1 whitespace-pre-line text-meta leading-snug text-fg">
        {row.detail}
      </p>
    {/if}
    {#if row?.sourceLabel}
      <p class="mt-1 truncate text-micro text-fg-muted">
        {row.sourceLabel}
      </p>
    {/if}
  </div>
</a>
