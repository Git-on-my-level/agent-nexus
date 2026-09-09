<script>
  import WorkCard from "./WorkCard.svelte";
  import SignalBadge from "./SignalBadge.svelte";
  import ActorLabel from "$lib/components/ActorLabel.svelte";
  import {
    phaseGroups,
    label,
    PHASE_LABELS,
    isNexusOwned,
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
    requestedDecisions = {},
    boardTitles = {},
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
  let focusedKey = $state("");

  /**
   * Status is a dot plus its name. Only the two states a reader has to act on
   * — blocked and done — are loud enough to earn a badge; badging all six made
   * "Backlog" shout as loudly as "Blocked".
   */
  const DOT_CLASS = {
    in_progress: "bg-accent",
    review: "bg-accent",
    ready: "bg-fg-muted",
  };
  function dotClass(phase) {
    return DOT_CLASS[phase] ?? "bg-line-strong";
  }
  /**
   * A source can report a state Nexus has no name for. Printing the raw token
   * ("vendor_waiting") at a reader is worse than printing the source's own
   * words, so an unfamiliar phase shows `native_status` when the source sent
   * one. The status column used to carry both on two lines.
   */
  function statusText(work) {
    const phase = work?.phase || "unknown";
    if (PHASE_LABELS[phase]) return PHASE_LABELS[phase];
    return String(work?.source?.native_status ?? "").trim() || label(phase);
  }
  function badgeTone(phase) {
    if (phase === "blocked") return "warn";
    if (phase === "done") return "ok";
    return "";
  }

  function boardLabel(work) {
    const ref = String(work?.board_ref ?? "").trim();
    return boardTitles[ref] || ref.replace(/^board:/, "") || "—";
  }

  /**
   * The last time we read this task from its source. A task Nexus owns has no
   * source to read, so it says so rather than reporting "never checked" as if
   * something were wrong — and never in the internal vocabulary ("Nexus-owned").
   */
  function lastChecked(work, tick = now) {
    // `tick` is the page's 30s clock: naming it here is what makes the
    // relative labels recompute when it advances.
    void tick;
    const observed = work?.freshness?.last_observed_at;
    if (observed) {
      return {
        text: formatTimestamp(observed),
        title: formatAbsoluteDateTime(observed),
        datetime: observed,
        muted: false,
      };
    }
    if (isNexusOwned(work)) {
      return { text: "created here", title: "", datetime: "", muted: true };
    }
    return { text: "never", title: "", datetime: "", muted: true };
  }

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
      // Only act when the card container itself has focus; a focused inner
      // link (task or Requested badge) activates natively.
      if (event.target !== event.currentTarget) return;
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
    class="board-scroll flex gap-3 overflow-x-auto pb-3"
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
        <div class="mb-2 flex items-baseline justify-between gap-2 px-1 py-1">
          <h2 class="ui-label mb-0 truncate">{group.label}</h2>
          <span class="shrink-0 text-micro tabular-nums text-fg-subtle"
            >{group.items.length}</span
          >
        </div>
        <div class="flex flex-col gap-2">
          {#each group.items as work (workKey(work))}
            {@const key = workKey(work)}
            {@const decisionId = requestedDecisions[key]}
            <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
            <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
            <div
              class="outline-none {focusedKey === key
                ? 'ring-1 ring-accent'
                : ''}"
              data-work-ref={work.ref}
              draggable="true"
              tabindex="0"
              role="listitem"
              ondragstart={(event) => handleDragStart(event, work)}
              onfocus={() => (focusedKey = key)}
              onkeydown={(event) => handleCardKey(event, work)}
            >
              <WorkCard
                {work}
                href={href(work)}
                boardTitle={boardLabel(work)}
              />
              {#if requested[key]}
                <p class="mt-1 px-1">
                  {#if decisionId}
                    <a
                      class="inline-flex rounded-sm outline-none focus-visible:ring-1 focus-visible:ring-accent"
                      href={workspaceHref(
                        `/inbox?item=decision:${encodeURIComponent(decisionId)}`,
                      )}
                      title="Answer this request in Inbox"
                    >
                      <SignalBadge tone="warn">Requested</SignalBadge>
                    </a>
                  {:else}
                    <SignalBadge tone="warn">Requested</SignalBadge>
                  {/if}
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
    <!--
      One DOM for both widths. Below 640px the same rows collapse into the
      two-line row the Inbox uses (title, then `board · status · time`) — a
      four-column table that scrolls sideways on a phone is a table nobody
      reads, and a second markup for mobile is a second thing to keep true.
    -->
    <table class="work-table w-full border-collapse text-left text-meta">
      <caption class="sr-only"
        >Tracked tasks with board, status, owner and when each was last checked</caption
      >
      <thead class="text-micro uppercase tracking-wide text-fg-subtle">
        <tr>
          {#each ["Task", "Board", "Status", "Owner", "Last checked"] as heading}
            <th
              scope="col"
              class="whitespace-nowrap border-b border-line px-3 py-2 font-semibold"
              >{heading}</th
            >
          {/each}
        </tr>
      </thead>
      <tbody class="divide-y divide-line-subtle bg-panel">
        {#each records as work (workKey(work))}
          {@const checked = lastChecked(work, now)}
          {@const tone = badgeTone(work.phase)}
          <tr
            class="h-10 align-middle hover:bg-panel-hover"
            data-work-ref={work.ref}
          >
            <th scope="row" class="min-w-56 max-w-96 px-3 py-1.5 font-normal">
              <a
                class="block truncate font-medium text-fg hover:text-accent-text"
                href={href(work)}>{work.title || "Untitled task"}</a
              >
              {#if work.next_actor || work.next_action}
                <p class="truncate text-micro text-fg-muted">
                  {[work.next_actor, work.next_action]
                    .filter(Boolean)
                    .join(" — ")}
                </p>
              {/if}
              <span class="work-row-meta text-micro text-fg-muted">
                <span class="min-w-0 flex-1 truncate"
                  >{boardLabel(work)} · {statusText(work)}</span
                >
                <span class="shrink-0 tabular-nums">{checked.text}</span>
              </span>
            </th>
            <td class="max-w-40 px-3 py-1.5">
              <span class="block truncate text-fg-muted"
                >{boardLabel(work)}</span
              >
            </td>
            <td class="whitespace-nowrap px-3 py-1.5">
              {#if tone}
                <SignalBadge {tone}>{statusText(work)}</SignalBadge>
              {:else}
                <span class="inline-flex items-center gap-1.5 text-fg-muted">
                  <span
                    class="h-1.5 w-1.5 shrink-0 rounded-full {dotClass(
                      work.phase,
                    )}"
                    aria-hidden="true"
                  ></span>
                  {statusText(work)}
                </span>
              {/if}
            </td>
            <td class="max-w-40 px-3 py-1.5">
              {#if work.owner}
                <ActorLabel
                  label={work.owner}
                  seed={work.owner}
                  size="xs"
                  nameClass="text-meta text-fg truncate"
                />
              {:else}
                <span class="text-fg-subtle">—</span>
              {/if}
            </td>
            <td class="whitespace-nowrap px-3 py-1.5">
              {#if checked.datetime}
                <time
                  class="tabular-nums text-fg-muted"
                  datetime={checked.datetime}
                  title={checked.title}>{checked.text}</time
                >
              {:else}
                <span class="text-fg-subtle">{checked.text}</span>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<style>
  /* Two-line rows below 640px: same rows, no second markup. */
  .work-row-meta {
    display: none;
  }

  @media (max-width: 639px) {
    .work-table,
    .work-table tbody,
    .work-table tr,
    .work-table th[scope="row"] {
      display: block;
    }

    .work-table thead,
    .work-table td {
      display: none;
    }

    .work-table tr {
      height: auto;
    }

    .work-table th[scope="row"] {
      max-width: none;
      min-width: 0;
      padding: 8px 12px;
    }

    .work-row-meta {
      display: flex;
      align-items: center;
      gap: 8px;
      min-width: 0;
    }
  }

  /*
   * The board's horizontal scrollbar is the only cue that columns continue off
   * screen; the 6px overlay bar the rest of the app uses is invisible here.
   */
  .board-scroll {
    scrollbar-width: auto;
  }

  .board-scroll::-webkit-scrollbar {
    height: 10px;
  }

  .board-scroll::-webkit-scrollbar-thumb {
    background: var(--line-strong);
    border-radius: 999px;
  }

  .board-scroll::-webkit-scrollbar-track {
    background: var(--bg-soft);
    border-radius: 999px;
  }
</style>
