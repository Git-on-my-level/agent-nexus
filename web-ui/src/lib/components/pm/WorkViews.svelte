<script>
  import WorkCard from "./WorkCard.svelte";
  import SignalBadge from "./SignalBadge.svelte";
  import ActorLabel from "$lib/components/ActorLabel.svelte";
  import {
    phaseGroups,
    label,
    sourceLabel,
    workFreshness,
    workKey,
  } from "$lib/pm/presentation.js";
  import { formatTimestamp, formatAbsoluteDateTime } from "$lib/formatDate";
  let {
    records = [],
    view = "table",
    workspaceHref,
    now = Date.now(),
  } = $props();
  let groups = $derived(
    phaseGroups(records).filter(
      (group) =>
        group.items.length ||
        ["backlog", "in_progress", "blocked", "review", "done"].includes(
          group.key,
        ),
    ),
  );
  const href = (work) =>
    workspaceHref(`/work/${encodeURIComponent(workKey(work))}`);
</script>

{#if view === "board"}
  <!-- Scroll regions must be keyboard-focusable for horizontal navigation. -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div
    class="flex gap-3 overflow-x-auto pb-3"
    role="region"
    aria-label="Work board grouped by phase"
    tabindex="0"
  >
    {#each groups as group (group.key)}
      <section
        class="w-72 shrink-0 rounded-md bg-bg-soft p-2"
        aria-label={group.label}
      >
        <div class="mb-2 flex items-center justify-between px-1 py-1.5">
          <h2 class="text-meta font-semibold text-fg">{group.label}</h2>
          <span class="text-micro text-fg-muted">{group.items.length}</span>
        </div>
        <div class="space-y-2">
          {#each group.items as work (workKey(work))}<WorkCard
              {work}
              href={href(work)}
              {now}
            />{:else}<p class="px-2 py-6 text-center text-micro text-fg-muted">
              No work in this phase
            </p>{/each}
        </div>
      </section>
    {/each}
  </div>
{:else}
  <!-- Scroll regions must be keyboard-focusable for horizontal navigation. -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div
    class="overflow-x-auto rounded-md border border-line"
    role="region"
    aria-label="Work table"
    tabindex="0"
  >
    <table class="w-full border-collapse text-left text-meta">
      <caption class="sr-only"
        >Tracked commitments with source authority, next actor, and evidence
        freshness</caption
      >
      <thead class="bg-bg-soft text-micro text-fg-muted"
        ><tr>
          {#each ["Commitment", "Source / status", "Phase", "Next actor / action", "Evidence", "Meaningful progress"] as heading}<th
              scope="col"
              class="whitespace-nowrap border-b border-line px-3 py-2.5 font-medium"
              >{heading}</th
            >{/each}
        </tr></thead
      >
      <tbody class="divide-y divide-line bg-panel">
        {#each records as work (workKey(work))}
          {@const signal = workFreshness(work, now)}
          <tr class="align-top hover:bg-panel-hover" data-work-ref={work.ref}>
            <th scope="row" class="min-w-52 max-w-80 px-3 py-3 font-normal"
              ><a
                class="break-words font-semibold text-fg hover:text-accent-text"
                href={href(work)}>{work.title || "Untitled commitment"}</a
              >
              <div class="mt-1 text-micro text-fg-muted">
                {work.project_ref || "No project"}{#if work.priority}
                  · {work.priority}{/if}
              </div>
              {#if work.blockers?.length}<p
                  class="mt-1 text-micro text-warn-text"
                >
                  {work.blockers.length} blocker{work.blockers.length === 1
                    ? ""
                    : "s"}
                </p>{/if}</th
            >
            <td class="min-w-36 px-3 py-3"
              ><div class="text-fg">{sourceLabel(work.source)}</div>
              <div class="mt-1 break-words text-micro text-fg-muted">
                {work.source?.native_status || "Status not observed"}
              </div></td
            >
            <td class="whitespace-nowrap px-3 py-3"
              ><SignalBadge tone={work.phase === "blocked" ? "warn" : "neutral"}
                >{label(work.phase)}</SignalBadge
              ></td
            >
            <td class="min-w-48 max-w-72 px-3 py-3"
              >{#if work.next_actor}<ActorLabel
                  label={work.next_actor}
                  size="xs"
                />{:else}<span class="text-fg-muted">Not assigned</span>{/if}
              <p class="mt-1 break-words text-micro text-fg-muted">
                {work.next_action || "Next action not established"}
              </p></td
            >
            <td class="min-w-40 px-3 py-3"
              ><SignalBadge tone={signal.tone}>{signal.label}</SignalBadge>
              <p class="mt-1 text-micro text-fg-muted">
                Observed {formatTimestamp(work.freshness?.last_observed_at) ||
                  "never"}
              </p></td
            >
            <td class="min-w-40 px-3 py-3 text-micro text-fg-muted"
              >{#if work.freshness?.meaningful_progress_at}<time
                  datetime={work.freshness.meaningful_progress_at}
                  title={formatAbsoluteDateTime(
                    work.freshness.meaningful_progress_at,
                  )}
                  >{formatTimestamp(
                    work.freshness.meaningful_progress_at,
                  )}</time
                >{:else}Not established{/if}</td
            >
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}
