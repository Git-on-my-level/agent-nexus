<script>
  import { threadDetailStore } from "$lib/threadDetailStore";
  import { actorRegistry, lookupActorDisplayName } from "$lib/actorSession";
  import { formatTimestamp } from "$lib/formatDate";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import RefLink from "$lib/components/RefLink.svelte";
  import { toTimelineView, eventTypeDotClass } from "$lib/timelineUtils";

  let { threadId } = $props();

  let timeline = $derived($threadDetailStore.timeline);
  let timelineLoading = $derived($threadDetailStore.timelineLoading);
  let timelineError = $derived($threadDetailStore.timelineError);

  let actorName = $derived((id) => lookupActorDisplayName(id, $actorRegistry));

  let timelineView = $derived(toTimelineView(timeline, { threadId }));
  let hasTimelineEvents = $derived(timelineView.length > 0);
</script>

<div class="mt-4">
  <div class="mb-3 flex items-center justify-between gap-3">
    <h2
      class="text-[12px] font-semibold uppercase tracking-wider text-[var(--ui-text-muted)]"
    >
      Timeline
    </h2>
    <div class="min-h-[1rem] text-right" aria-live="polite">
      {#if timelineLoading && hasTimelineEvents}
        <p class="text-[11px] text-[var(--ui-text-muted)]">Syncing…</p>
      {/if}
    </div>
  </div>
  {#if timelineError && !hasTimelineEvents}
    <p class="rounded bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
      {timelineError}
    </p>
  {:else if timelineLoading && !hasTimelineEvents}
    <p class="text-[13px] text-[var(--ui-text-muted)]">Loading timeline...</p>
  {:else if !hasTimelineEvents}
    <p class="text-[13px] text-[var(--ui-text-muted)]">No events yet.</p>
  {:else}
    {#if timelineError}
      <p class="mb-2 rounded bg-red-500/10 px-3 py-2 text-[13px] text-red-400">
        {timelineError}
      </p>
    {/if}
    <div class="space-y-1">
      {#each timelineView as event (event.id)}
        <div
          class="group rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-4 py-2.5"
          id={`event-${event.id}`}
        >
          <div class="flex items-start justify-between gap-3">
            <div class="flex min-w-0 flex-1 items-start gap-2.5">
              <span
                class="mt-1.5 h-2 w-2 shrink-0 rounded-full {eventTypeDotClass(
                  event.rawType,
                )}"
                title={event.typeLabel}
              ></span>
              <div class="min-w-0 flex-1">
                <MarkdownRenderer
                  source={event.summary}
                  class="text-[13px] text-[var(--ui-text)]"
                />
                <p class="mt-0.5 text-[12px] text-[var(--ui-text-muted)]">
                  {actorName(event.actor_id)} · {event.typeLabel} · {formatTimestamp(
                    event.ts,
                  ) || "—"}
                </p>
              </div>
            </div>
          </div>

          {#if event.changedFields.length > 0}
            <div class="mt-1.5 flex flex-wrap gap-1 text-[12px]">
              {#each event.changedFields as field}
                <span
                  class="rounded bg-[var(--ui-border)] px-1.5 py-0.5 text-[var(--ui-text-muted)]"
                  >{field}</span
                >
              {/each}
            </div>
          {/if}

          {#if event.refs.length > 0}
            <div class="mt-1.5 flex flex-wrap gap-1.5 text-[12px]">
              {#each event.refs as refValue}<RefLink
                  {refValue}
                  {threadId}
                />{/each}
            </div>
          {/if}

          {#if !event.isKnownType}
            <details class="mt-1.5">
              <summary
                class="cursor-pointer text-[12px] text-[var(--ui-text-muted)]"
                >Details</summary
              >
              <pre
                class="mt-1 overflow-auto rounded bg-[var(--ui-bg-soft)] p-2 text-[11px] text-[var(--ui-text-muted)]">{JSON.stringify(
                  event.payload ?? {},
                  null,
                  2,
                )}</pre>
            </details>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>
