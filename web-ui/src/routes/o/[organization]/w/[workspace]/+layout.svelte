<script>
  let { data, children } = $props();
</script>

{#if data.coreSchemaCheckWarning}
  <div
    class="border-b px-4 py-2.5 text-sm leading-snug"
    style="border-color: var(--line); background: var(--warn-soft); color: var(--warn-text);"
    role="status"
  >
    <strong class="text-[var(--fg)]">Workspace core unreachable.</strong>{" "}
    Schema handshake was skipped in dev so you can open the UI. Start the
    workspace runtime (for hosted local dev, run
    <code class="rounded bg-[var(--panel)] px-1 py-0.5 font-mono text-[var(--fg-muted)]"
      >make serve</code
    >
    in <code class="rounded bg-[var(--panel)] px-1 py-0.5 font-mono text-[var(--fg-muted)]"
      >controlplane/</code
    >). API calls may fail until anx-core is up.
    <span class="mt-1 block font-mono text-[11px] text-[var(--fg-subtle)]">
      {data.coreSchemaCheckWarning}
    </span>
  </div>
{/if}

{@render children()}
