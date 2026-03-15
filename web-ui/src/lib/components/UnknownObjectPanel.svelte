<script>
  let { title = "Raw Object", objectData = {}, open = false } = $props();
</script>

<details
  class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] p-3"
  {open}
>
  <summary
    class="cursor-pointer text-xs font-semibold uppercase tracking-wide text-[var(--ui-text-muted)]"
  >
    {title}
  </summary>
  <pre
    class="mt-2 overflow-auto rounded bg-[var(--ui-panel-muted)] p-3 text-[11px] leading-5 text-[var(--ui-text)]">{JSON.stringify(
      objectData,
      null,
      2,
    )}</pre>
</details>
