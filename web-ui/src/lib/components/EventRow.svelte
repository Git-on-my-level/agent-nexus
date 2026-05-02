<script>
  import {
    lookupActorDisplayName,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import { formatTimestamp } from "$lib/formatDate";

  let { row, inspectable = false } = $props();

  let actorName = $derived(
    lookupActorDisplayName(row?.actorId, $actorRegistry, $principalRegistry) ||
      row?.actorId ||
      "System",
  );
</script>

<a
  id={row?.id}
  class="block px-3 py-2.5 transition-colors hover:bg-line-subtle sm:px-4"
  href={row?.href || "#"}
>
  <div class="flex min-w-0 items-start gap-3">
    <div class="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-fg-muted"></div>
    <div class="min-w-0 flex-1">
      <div class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5">
        <span class="truncate text-meta font-medium text-fg">
          {actorName}
        </span>
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
      {#if row?.sourceLabel || (inspectable && row?.rawType)}
        <p class="mt-1 truncate text-micro text-fg-muted">
          {row.sourceLabel || row.rawType}
        </p>
      {/if}
    </div>
  </div>
</a>
