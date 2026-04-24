<script>
  import CopyButton from "$lib/components/CopyButton.svelte";

  /**
   * @typedef {{ label: string, value: string, mono?: boolean, copyLabel?: string }} IntegrityRow
   */
  let {
    rows = [],
    rawJson = "",
    rawJsonCopyLabel = "Copy JSON",
    class: className = "",
  } = $props();

  function rowCopyLabel(row) {
    return row.copyLabel ?? `Copy ${row.label}`;
  }

  function isPresent(value) {
    return value != null && String(value).trim() !== "";
  }
</script>

<details
  class="rounded-md border border-[var(--line)] bg-[var(--bg-soft)] {className}"
>
  <summary
    class="cursor-pointer px-4 py-2.5 text-micro text-[var(--fg-muted)] hover:text-[var(--fg)]"
    >IDs & integrity</summary
  >
  <div class="space-y-3 border-t border-[var(--line-subtle)] px-4 pb-3 pt-3">
    {#each rows as row}
      {#if isPresent(row.value)}
        <div>
          <p
            class="text-micro uppercase tracking-[0.12em] text-[var(--fg-muted)]"
          >
            {row.label}
          </p>
          <div
            class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[var(--fg-muted)]"
          >
            <span
              class="min-w-0 shrink break-all {row.mono !== false
                ? 'font-mono text-micro'
                : 'text-micro'}"
            >
              {row.value}
            </span>
            <CopyButton value={String(row.value)} label={rowCopyLabel(row)} />
          </div>
        </div>
      {/if}
    {/each}

    {#if isPresent(rawJson)}
      <div>
        <p
          class="text-micro uppercase tracking-[0.12em] text-[var(--fg-muted)]"
        >
          Raw JSON
        </p>
        <div
          class="mt-1 flex items-start gap-2 text-micro text-[var(--fg-muted)]"
        >
          <CopyButton value={String(rawJson)} label={rawJsonCopyLabel} />
          <pre class="min-w-0 flex-1 overflow-auto">{rawJson}</pre>
        </div>
      </div>
    {/if}
  </div>
</details>
