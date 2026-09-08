<script>
  import SignalBadge from "./SignalBadge.svelte";
  import { sourceLabel, workFreshness } from "$lib/pm/presentation.js";
  import { formatTimestamp } from "$lib/formatDate";
  let { work, href, now = Date.now() } = $props();
  let signal = $derived(workFreshness(work, now));
</script>

<a
  {href}
  class="block rounded-md border border-line bg-panel px-3 py-2.5 transition-colors hover:border-line-strong hover:bg-panel-hover"
  data-work-ref={work.ref}
>
  <h3 class="break-words text-meta font-medium leading-snug text-fg">
    {work.title || "Untitled work"}
  </h3>
  <p class="mt-1 truncate text-micro text-fg-muted">
    {sourceLabel(work.source)}{#if work.source?.native_status}
      · {work.source.native_status}{/if}{#if work.priority}
      · {work.priority}{/if}
  </p>
  <div class="mt-2.5 flex flex-wrap items-center gap-1.5">
    {#if !(work.source?.authority === "nexus" && !work.freshness?.last_observed_at)}
      <SignalBadge tone={signal.tone}>{signal.label}</SignalBadge>
    {/if}
    {#if work.blockers?.length}
      <SignalBadge tone="warn"
        >{work.blockers.length} blocker{work.blockers.length === 1
          ? ""
          : "s"}</SignalBadge
      >
    {/if}
  </div>
  {#if work.next_actor || work.next_action}
    <p class="mt-2.5 line-clamp-2 break-words text-micro text-fg-muted">
      {#if work.next_actor}<span class="font-medium text-fg"
          >{work.next_actor}</span
        >{#if work.next_action}
          ·
        {/if}{/if}{work.next_action || ""}
    </p>
  {/if}
  {#if work.freshness?.meaningful_progress_at}
    <p class="mt-1.5 text-micro text-fg-subtle">
      Progress {formatTimestamp(work.freshness.meaningful_progress_at)}
    </p>
  {/if}
</a>
