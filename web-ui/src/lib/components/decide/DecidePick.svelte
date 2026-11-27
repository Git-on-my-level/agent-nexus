<script>
  /**
   * One decision's "select for skipping" checkbox: a row's leading cell in
   * the list, or a gutter beside the card in the phone stack.
   */
  let { decision, checked = false, ontoggle, gutter = false } = $props();
</script>

<label
  class={gutter
    ? "flex min-h-11 w-9 shrink-0 cursor-pointer items-start justify-start pt-4"
    : "flex shrink-0 cursor-pointer items-center pl-3"}
  title="Select for skipping (X)"
>
  <input
    type="checkbox"
    class={gutter ? "size-5" : undefined}
    {checked}
    onchange={() => ontoggle(decision.inboxItemId)}
    aria-label={`Select ${decision.title}`}
    data-decide-pick={decision.inboxItemId}
  />
</label>
