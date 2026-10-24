<script>
  /**
   * One loop at a glance: what it is, whether it is working, the metric it
   * climbs and how that has moved, what its recent runs did, and when it
   * last changed something. Watch's Loops view and the `live-loops` report
   * panel both draw this card, so a loop reads the same everywhere.
   *
   * Every fact here is the computed `loop` object (`source` names it); the
   * card only arranges it. A loop that only links a series (no receipts
   * yet) passes its legacy metric line through `extra`.
   */
  import { formatMetricValue, formatSpan } from "$lib/brief.js";
  import { formatAgo } from "$lib/time/format.js";
  import {
    LOOP_CONDITION_TEXT,
    cadenceText,
    goalGap,
    isIncumbent,
    withUnit,
    RUN_OUTCOME_TEXT,
  } from "$lib/loops.js";
  import LoopChart from "./LoopChart.svelte";
  import LoopRuns from "./LoopRuns.svelte";

  let {
    ref,
    title,
    href = "",
    loop = null,
    now = Date.now(),
    source = "computed:initiatives.loop",
    /** Badges beside the title (waiting on you, blocked). */
    badges = null,
    /** Draw the inline sparkline (off where a full chart follows). */
    chart = true,
    /** Content under the card body (legacy series line, note). */
    extra = null,
  } = $props();

  let condition = $derived(
    LOOP_CONDITION_TEXT[loop?.condition ?? "unknown"] ??
      LOOP_CONDITION_TEXT.unknown,
  );
  let goal = $derived(loop?.goal ?? null);
  let latest = $derived(
    goal?.latest ??
      goal?.points?.findLast((point) => isIncumbent(point))?.value ??
      null,
  );
  let gap = $derived(goalGap(goal));
  let reached = $derived(gap !== null && gap <= 0);
  let counts = $derived(
    loop
      ? ["acted", "measured", "no_op", "infra_error"]
          .filter((outcome) => loop.runs7d[outcome] > 0)
          .map(
            (outcome) => `${loop.runs7d[outcome]} ${RUN_OUTCOME_TEXT[outcome]}`,
          )
      : [],
  );
  let nextDue = $derived.by(() => {
    const at = Date.parse(loop?.nextRunDueAt || "");
    if (!Number.isFinite(at)) return "";
    const seconds = Math.round((at - now) / 1000);
    return seconds >= 60
      ? `next run in ${formatSpan(seconds)}`
      : seconds > -60
        ? "next run due now"
        : `run overdue by ${formatSpan(-seconds)}`;
  });
  let footer = $derived(
    loop
      ? [
          loop.lastEffectAt
            ? `last effect ${formatAgo(loop.lastEffectAt, now)}`
            : loop.recentRuns.length
              ? "no effect yet"
              : "",
          cadenceText(loop, formatSpan),
          nextDue,
        ].filter(Boolean)
      : [],
  );
</script>

<article
  class="loop-card loop-card--{condition.tone} flex min-w-0 flex-col gap-3 rounded-md border border-line bg-panel px-3.5 py-3 transition-colors hover:border-line-strong"
  data-loop={ref}
  data-loop-tone={condition.tone}
>
  <header class="flex min-w-0 items-start gap-3">
    <div class="min-w-0 flex-1">
      {#if href}
        <a
          {href}
          class="line-clamp-2 break-words text-body font-medium leading-snug text-fg hover:text-accent-text"
          data-loop-title>{title}</a
        >
      {:else}
        <p
          class="line-clamp-2 break-words text-body font-medium leading-snug text-fg"
          data-loop-title
        >
          {title}
        </p>
      {/if}
      {#if loop?.reason}
        <p
          class="mt-0.5 text-micro text-fg-muted"
          data-source={source || undefined}
        >
          {loop.reason}
        </p>
      {/if}
    </div>
    <div class="flex shrink-0 flex-wrap items-center justify-end gap-1.5">
      {@render badges?.()}
      {#if loop}
        <span
          class="ui-badge ui-badge--{condition.tone}"
          data-source={source || undefined}>{condition.text}</span
        >
      {/if}
    </div>
  </header>

  {#if goal}
    <div
      class="grid min-w-0 grid-cols-[minmax(0,auto)_minmax(0,1fr)] items-end gap-x-4"
      data-loop-goal
      data-source={source || undefined}
    >
      <div class="min-w-0">
        <p class="truncate text-micro text-fg-muted" title={goal.metric}>
          {goal.label || goal.metric}
          <span class="text-fg-subtle"
            >{goal.direction === "down" ? "↓" : "↑"}</span
          >
        </p>
        <p
          class="text-[1.375rem] font-semibold leading-tight tabular-nums text-fg"
        >
          {latest === null
            ? "—"
            : withUnit(formatMetricValue(latest), goal.unit)}
        </p>
        <p class="text-micro tabular-nums text-fg-subtle">
          {#if goal.best !== null && goal.best !== latest}best {withUnit(
              formatMetricValue(goal.best),
              goal.unit,
            )}{/if}{#if goal.target !== null}{goal.best !== null &&
            goal.best !== latest
              ? " · "
              : ""}{#if reached}<span class="text-ok-text">target met</span
              >{:else}target {withUnit(
                formatMetricValue(goal.target),
                goal.unit,
              )}{/if}{/if}
        </p>
      </div>
      {#if chart}
        <div class="w-full min-w-0 max-w-[22rem] self-center justify-self-end">
          {#if goal.points.length > 1}
            <LoopChart {goal} {now} label="{title}: {goal.metric}" />
          {:else}
            <p class="text-right text-micro text-fg-subtle">
              {goal.points.length
                ? "one measurement so far"
                : "no measurements yet"}
            </p>
          {/if}
        </div>
      {/if}
    </div>
  {/if}

  {#if loop}
    <div
      class="flex min-w-0 flex-wrap items-center justify-between gap-x-3 gap-y-1.5"
    >
      <LoopRuns runs={loop.recentRuns} {now} {source} />
      {#if counts.length}
        <p
          class="text-micro tabular-nums text-fg-muted"
          data-source={source || undefined}
        >
          7d: {counts.join(" · ")}
        </p>
      {:else if !loop.recentRuns.length}
        <p class="text-micro text-fg-subtle">No run receipts yet</p>
      {/if}
    </div>
    {#if footer.length}
      <p
        class="flex min-w-0 flex-wrap gap-x-1.5 text-micro text-fg-subtle"
        data-source={source || undefined}
      >
        {#each footer as part, i (i)}
          {#if i > 0}<span aria-hidden="true">·</span>{/if}<span>{part}</span>
        {/each}
      </p>
    {/if}
  {/if}

  {@render extra?.()}
</article>

<style>
  .loop-card--danger {
    border-left: 3px solid var(--danger-text);
  }
  .loop-card--warn {
    border-left: 3px solid var(--warn-text);
  }
</style>
