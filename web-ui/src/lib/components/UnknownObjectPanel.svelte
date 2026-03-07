<script>
  export let title = "Raw Object";
  export let objectData = {};
  export let open = false;
</script>

<details class="rounded-md border border-gray-200 bg-gray-50 p-3" {open}>
  <summary
    class="cursor-pointer text-xs font-semibold uppercase tracking-wide text-gray-600"
  >
    {title}
  </summary>
  <pre
    class="mt-2 overflow-auto rounded bg-gray-100 p-3 text-[11px] leading-5 text-gray-800">{JSON.stringify(
      objectData,
      null,
      2,
    )}</pre>
</details>
