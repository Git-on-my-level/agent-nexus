<script>
  /**
   * The paragraph that explains what a section does *not* mean.
   *
   * These lines are real and occasionally load-bearing — "participation does
   * not assign a task", "created here, so there is nothing to check" — but a
   * reader meets them on every visit and needs them on almost none. Printed in
   * flow they are most of what a quiet section shows. Folded away they cost
   * one line and are still one click from being read.
   */
  let {
    label = "What this means",
    class: extraClass = "",
    children,
  } = $props();
</script>

<details class="fine-print {extraClass}" data-fine-print>
  <summary>{label}</summary>
  <div class="fine-print__body">{@render children?.()}</div>
</details>

<style>
  .fine-print {
    margin-top: 6px;
  }
  .fine-print summary {
    cursor: pointer;
    color: var(--fg-subtle, var(--fg-muted));
    font-size: 11px;
    line-height: 16px;
    list-style: revert;
  }
  .fine-print summary:hover {
    color: var(--fg-muted);
  }
  .fine-print__body {
    margin-top: 4px;
    color: var(--fg-muted);
    font-size: 11px;
    line-height: 16px;
  }
</style>
