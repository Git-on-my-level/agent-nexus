<script>
  let { title = "Raw Object", objectData = {}, open = false } = $props();
</script>

<details class="rounded-md border border-line bg-bg-soft p-3" {open}>
  <summary
    class="cursor-pointer text-micro font-semibold uppercase tracking-wide text-fg-muted"
  >
    {title}
  </summary>
  <pre
    class="mt-2 overflow-auto rounded bg-bg-soft p-3 text-micro leading-5 text-fg">{JSON.stringify(
      objectData,
      null,
      2,
    )}</pre>
</details>
