<script>
  export let title = "Raw Object";
  export let objectData = {};
  export let open = false;
</script>

<details class="rounded-md border border-slate-200 bg-slate-50 p-3" {open}>
  <summary
    class="cursor-pointer text-xs font-semibold uppercase tracking-wide text-slate-600"
  >
    {title}
  </summary>
  <pre
    class="mt-2 overflow-auto rounded bg-white p-3 text-[11px] leading-5 text-slate-800">{JSON.stringify(
      objectData,
      null,
      2,
    )}</pre>
</details>
