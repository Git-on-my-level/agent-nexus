<script>
  import ActorLabel from "$lib/components/ActorLabel.svelte";
  import SignalBadge from "./SignalBadge.svelte";
  import { sourceLabel, workFreshness } from "$lib/pm/presentation.js";
  import { formatTimestamp } from "$lib/formatDate";
  let { work, href, now = Date.now() } = $props();
  let signal = $derived(workFreshness(work, now));
</script>

<a
  {href}
  class="block rounded-md border border-line bg-panel p-3 transition-colors hover:border-line-strong hover:bg-panel-hover"
  data-work-ref={work.ref}
>
  <div
    class="mb-2 flex flex-wrap items-center gap-1.5 text-micro text-fg-muted"
  >
    <span>{sourceLabel(work.source)}</span><span aria-hidden="true">·</span
    ><span class="truncate" title={work.source?.native_id || work.ref}
      >{work.source?.native_id || work.handle || work.ref}</span
    >
    {#if work.priority}<span class="ml-auto">{work.priority}</span>{/if}
  </div>
  <h3 class="break-words text-meta font-semibold text-fg">
    {work.title || "Untitled commitment"}
  </h3>
  {#if work.source?.native_status}<p class="mt-1 text-micro text-fg-muted">
      Source status: {work.source.native_status}
    </p>{/if}
  <div class="mt-3">
    <SignalBadge tone={signal.tone}>{signal.label}</SignalBadge>
  </div>
  <div class="mt-3 border-t border-line-subtle pt-2 text-micro">
    <p class="text-fg-muted">Next actor</p>
    {#if work.next_actor}<ActorLabel
        label={work.next_actor}
        size="xs"
        nameClass="text-micro font-medium text-fg"
      />{:else}<p class="text-fg">Not assigned</p>{/if}
    <p class="mt-1 line-clamp-2 break-words text-fg">
      {work.next_action || "Next action not established"}
    </p>
    {#if work.blockers?.length}<p class="mt-2 text-warn-text">
        {work.blockers.length} blocker{work.blockers.length === 1 ? "" : "s"}
      </p>{/if}
    <p class="mt-2 text-fg-muted">
      Progress: {formatTimestamp(work.freshness?.meaningful_progress_at) ||
        "not established"}
    </p>
  </div>
</a>
