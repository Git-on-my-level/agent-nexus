<script>
  /**
   * What an answer did, and whether anyone received it.
   *
   * Handled used to end at "Answered", which is the one thing the reader
   * already knew. An answer is addressed to a task: core records the decision
   * on that task in the same transaction, and any subscription the asking
   * agent registered is an accelerator on top of it. So this names both — the
   * task outcome first, because it is the durable one, then every delivery
   * state, including the ones that are not errors.
   *
   * "Not delivered: no subscriber" is informational. An agent that asked and
   * exited is the normal case: the task was unblocked and is waiting for
   * whoever owns it next, and nothing was lost. A failed delivery carries
   * core's reason, and no retry — the contract lets only the subscriber record
   * a receipt, so a Retry button here would be a button that cannot work.
   */
  import SignalBadge from "$lib/components/pm/SignalBadge.svelte";
  import WorkSummary from "$lib/components/WorkSummary.svelte";

  let {
    /** `askDeliveryModel` result, or null while it loads or is unavailable. */
    model = null,
    /** The task's computed summary, from data the page already loaded. */
    taskSummary = null,
    taskTitle = "",
    taskHref = "",
    loading = false,
    now = Date.now(),
  } = $props();

  let task = $derived(model?.task ?? null);
  let subscriptions = $derived(model?.subscriptions ?? []);
</script>

{#if loading && !model}
  <p class="text-micro text-fg-subtle" data-inbox-delivery-loading>
    Reading delivery state…
  </p>
{:else if model}
  <div class="space-y-2" data-inbox-delivery>
    {#if task}
      <div class="space-y-1">
        <p class="ui-label">Task</p>
        <!-- A context request clears the blocker too, so `task.label` reads
             "Returned for context" rather than "Unblocked": the ask was not
             answered, and the header above already says so. -->
        {#if taskSummary}
          <!-- The same renderer the Tasks table and the board use, so Handled
               cannot describe the task differently from the task list. -->
          <WorkSummary
            summary={taskSummary}
            density="row"
            title={taskTitle || task.ref}
            {now}
          />
        {/if}
        <p class="text-meta text-fg [overflow-wrap:anywhere]">
          {#if taskHref}
            <a class="text-fg hover:underline" href={taskHref}
              >{taskTitle || task.ref}</a
            >
            <span class="text-fg-subtle">{" · "}</span>
          {/if}<span data-inbox-task-outcome>{task.label}</span>
        </p>
      </div>
    {/if}
    <div class="space-y-1">
      <p class="ui-label">Delivery</p>
      {#if model.notDelivered}
        <p class="text-meta text-fg-muted" data-inbox-delivery-none>
          {model.notDelivered}
        </p>
      {:else if !subscriptions.length}
        <p class="text-meta text-fg-muted">No subscriptions were registered.</p>
      {:else}
        <ul class="space-y-1">
          {#each subscriptions as entry (entry.id || entry.label)}
            <li
              class="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-0.5 text-meta"
              data-inbox-delivery-row={entry.id || entry.label}
            >
              <span class="shrink-0 text-micro text-fg-subtle"
                >{entry.kindLabel}</span
              >
              <span class="min-w-0 flex-1 truncate text-fg">{entry.label}</span>
              <SignalBadge tone={entry.tone} class="shrink-0"
                >{entry.stateLabel}</SignalBadge
              >
              {#if entry.ageLabel}
                <span
                  class="shrink-0 tabular-nums text-micro text-fg-subtle"
                  data-inbox-delivery-age>{entry.ageLabel}</span
                >
              {/if}
              {#if entry.attempts > 1}
                <span class="shrink-0 text-micro text-fg-subtle"
                  >{entry.attempts} attempts</span
                >
              {/if}
              {#if entry.reason}
                <span
                  class="w-full text-micro {entry.failed
                    ? 'text-danger-text'
                    : 'text-fg-muted'} [overflow-wrap:anywhere]"
                  data-inbox-delivery-reason>{entry.reason}</span
                >
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  </div>
{/if}
