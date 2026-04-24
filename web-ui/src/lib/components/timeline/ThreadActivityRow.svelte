<script>
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import { formatTimestamp } from "$lib/formatDate";
  import { eventTypeDotClass, toTimelineViewEvent } from "$lib/timelineUtils";

  let { rawEvent, threadId, actorName } = $props();

  let view = $derived(toTimelineViewEvent(rawEvent, { threadId }));
</script>

<div
  class="flex items-start gap-2 rounded-md border border-[var(--line-subtle)] bg-[var(--bg-soft)]/40 px-2 py-1.5 text-micro text-[var(--fg-muted)] {view.archived_at
    ? 'opacity-60'
    : ''}"
  id={`thread-activity-${view.id}`}
>
  <span
    class="mt-1 h-1.5 w-1.5 shrink-0 rounded-full {eventTypeDotClass(
      view.rawType,
    )}"
    title={view.typeLabel}
    aria-hidden="true"
  ></span>
  <div class="min-w-0 flex-1">
    <div class="line-clamp-1 [&_.prose]:!my-0 [&_.prose_*]:!my-0">
      <MarkdownRenderer
        source={String(view.summary ?? "—")}
        class="text-micro leading-snug text-[var(--fg-muted)]"
      />
    </div>
    <p class="mt-0.5 truncate text-micro text-[var(--fg-muted)] opacity-90">
      {actorName(view.actor_id)}
      · {view.typeLabel}
      · {formatTimestamp(view.ts) || "—"}
    </p>
  </div>
</div>
