<script>
  /** One decision's "select for skipping" checkbox, in the list or the stack. */
  let { decision, checked = false, ontoggle, labelled = false } = $props();
</script>

<label
  class={labelled
    ? "flex min-h-8 items-center gap-2 px-1 text-micro text-fg-muted"
    : "flex shrink-0 cursor-pointer items-center pl-3"}
  title={labelled ? undefined : "Select for skipping (X)"}
>
  <input
    type="checkbox"
    {checked}
    onchange={() => ontoggle(decision.inboxItemId)}
    aria-label={labelled ? undefined : `Select ${decision.title}`}
    data-decide-pick={decision.inboxItemId}
  />
  {#if labelled}Select{/if}
</label>
