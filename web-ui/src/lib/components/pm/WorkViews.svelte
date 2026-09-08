<script>
  import WorkCard from "./WorkCard.svelte";
  import SignalBadge from "./SignalBadge.svelte";
  import {
    phaseGroups,
    label,
    sourceLabel,
    workFreshness,
    workKey,
    taskDetailPath,
  } from "$lib/pm/presentation.js";
  import { formatTimestamp, formatAbsoluteDateTime } from "$lib/formatDate";
  let {
    records = [],
    view = "table",
    workspaceHref,
    now = Date.now(),
    requested = {},
    onMove,
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
  const href = (work) => workspaceHref(taskDetailPath(work));
  const phaseTone = (phase) =>
    phase === "blocked" ? "warn" : phase === "done" ? "ok" : "neutral";
  let focusedKey = $state("");

  function orderedPhases() {
    return groups.map((group) => group.key);
  }

  function handleDragStart(event, work) {
    event.dataTransfer.setData("text/plain", workKey(work));
    event.dataTransfer.effectAllowed = "move";
  }

  function handleDrop(event, phase) {
    event.preventDefault();
    const key = event.dataTransfer.getData("text/plain");
    const work = records.find((item) => workKey(item) === key);
    if (work && onMove) onMove(work, phase);
  }

  function handleCardKey(event, work) {
    if (event.key === "Enter") {
      const link = event.currentTarget.querySelector("a[href]");
      if (link) {
        event.preventDefault();
        link.click();
      }
      return;
    }
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    event.preventDefault();
    const keys = orderedPhases();
    const current = work.phase || "unknown";
    const index = keys.indexOf(current);
    const nextIndex =
      event.key === "ArrowRight"
        ? Math.min(keys.length - 1, Math.max(0, index) + 1)
        : Math.max(0, (index < 0 ? 0 : index) - 1);
    const nextPhase = keys[nextIndex];
    if (nextPhase && nextPhase !== current && onMove) onMove(work, nextPhase);
  }
</script>

{#if view === "board"}
  <!-- Scroll regions must be keyboard-focusable for horizontal navigation. -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div
    class="flex gap-3 overflow-x-auto pb-3"
    role="region"
    aria-label="Task board grouped by phase"
    tabindex="0"
  >
    {#each groups as group (group.key)}
      <section
        class="w-72 shrink-0 rounded-md bg-bg-soft p-2"
        aria-label={group.label}
        ondragover={(event) => event.preventDefault()}
        ondrop={(event) => handleDrop(event, group.key)}
      >
        <div class="mb-2 flex items-baseline justify-between px-1 py-1">
          <h2
            class="text-micro font-semibold uppercase tracking-wide text-fg-muted"
          >
            {group.label}
          </h2>
          <span class="text-micro text-fg-subtle">{group.items.length}</span>
        </div>
        <div class="space-y-2">
          {#each group.items as work (workKey(work))}
            <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
            <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
            <div
              class="outline-none {focusedKey === workKey(work)
                ? 'ring-1 ring-accent'
                : ''}"
              draggable="true"
              tabindex="0"
              role="listitem"
              ondragstart={(event) => handleDragStart(event, work)}
              onfocus={() => (focusedKey = workKey(work))}
              onkeydown={(event) => handleCardKey(event, work)}
            >
              <WorkCard {work} href={href(work)} {now} />
              {#if requested[workKey(work)]}
                <p class="mt-1 px-1">
                  <SignalBadge tone="warn">Requested</SignalBadge>
                </p>
              {/if}
            </div>
          {:else}
            <p class="px-2 py-6 text-center text-micro text-fg-subtle">
              Nothing here
            </p>
          {/each}
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
    aria-label="Task table"
    tabindex="0"
  >
    <table class="w-full border-collapse text-left text-meta">
      <caption class="sr-only"
        >Tracked tasks with source status, next step and evidence freshness</caption
      >
      <thead
        class="text-micro font-medium uppercase tracking-wide text-fg-subtle"
      >
        <tr>
          {#each ["Task", "Status", "Next", "Evidence"] as heading}
            <th
              scope="col"
              class="whitespace-nowrap border-b border-line px-3 py-2 font-medium"
              >{heading}</th
            >
          {/each}
        </tr>
      </thead>
      <tbody class="divide-y divide-line-subtle bg-panel">
        {#each records as work (workKey(work))}
          {@const signal = workFreshness(work, now)}
          <tr class="align-top hover:bg-panel-hover" data-work-ref={work.ref}>
            <th scope="row" class="min-w-56 max-w-96 px-3 py-2.5 font-normal">
              <a
                class="break-words font-medium text-fg hover:text-accent-text"
                href={href(work)}>{work.title || "Untitled task"}</a
              >
              <p class="mt-0.5 truncate text-micro text-fg-muted">
                {sourceLabel(work.source)}{#if work.source?.native_id}
                  <span class="font-mono">{work.source.native_id}</span
                  >{/if}{#if work.project_ref}
                  · {work.project_ref}{/if}{#if work.priority}
                  · {work.priority}{/if}
              </p>
            </th>
            <td class="min-w-36 whitespace-nowrap px-3 py-2.5">
              <SignalBadge tone={phaseTone(work.phase)}
                >{label(work.phase)}</SignalBadge
              >
              {#if work.source?.native_status}
                <p class="mt-1 max-w-40 truncate text-micro text-fg-muted">
                  {work.source.native_status}
                </p>
              {/if}
              {#if work.blockers?.length}
                <p class="mt-1 text-micro text-warn-text">
                  {work.blockers.length} blocker{work.blockers.length === 1
                    ? ""
                    : "s"}
                </p>
              {/if}
            </td>
            <td class="min-w-48 max-w-80 px-3 py-2.5">
              {#if work.next_actor || work.next_action}
                <p class="break-words text-meta">
                  {#if work.next_actor}<span class="font-medium text-fg"
                      >{work.next_actor}</span
                    >{/if}
                </p>
                <p class="break-words text-micro text-fg-muted">
                  {work.next_action || ""}
                </p>
              {:else}
                <span class="text-micro text-fg-subtle">—</span>
              {/if}
            </td>
            <td class="min-w-44 whitespace-nowrap px-3 py-2.5">
              {#if work.source?.authority === "nexus" && !work.freshness?.last_observed_at}
                <span class="text-micro text-fg-subtle">Nexus-owned</span>
              {:else}
                <SignalBadge tone={signal.tone}>{signal.label}</SignalBadge>
                <p class="mt-1 text-micro text-fg-muted">
                  {#if work.freshness?.last_observed_at}
                    <time
                      datetime={work.freshness.last_observed_at}
                      title={formatAbsoluteDateTime(
                        work.freshness.last_observed_at,
                      )}
                      >seen {formatTimestamp(
                        work.freshness.last_observed_at,
                      )}</time
                    >
                  {:else}never observed{/if}{#if work.freshness?.meaningful_progress_at}
                    · <time
                      datetime={work.freshness.meaningful_progress_at}
                      title={formatAbsoluteDateTime(
                        work.freshness.meaningful_progress_at,
                      )}
                      >progress {formatTimestamp(
                        work.freshness.meaningful_progress_at,
                      )}</time
                    >{/if}
                </p>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}
