<script>
  /** @type {{ status?: string | null, label?: string | null }} */
  let { status = null, label = null } = $props();

  const tone = $derived.by(() => {
    const s = String(status ?? "")
      .trim()
      .toLowerCase();
    if (s === "ready" || s === "active") return "text-ok-text bg-ok-soft";
    if (s === "provisioning" || s === "pending")
      return "text-warn-text bg-warn-soft";
    if (
      s === "failed" ||
      s === "error" ||
      s === "degraded" ||
      s === "suspended"
    )
      return "text-danger-text bg-danger-soft";
    return "text-fg-subtle bg-panel-hover";
  });

  const display = $derived(label ?? status ?? "unknown");
  const displayText = $derived(String(display ?? ""));
</script>

<!--
  min-w-0 + max-w-full + truncate: the label is arbitrary (call sites pass
  telemetry strings, not just the short status words), so the pill has to be
  able to shrink. With the old shrink-0 a long label kept its full intrinsic
  width and pushed its row siblings out / overflowed the container. It only
  shrinks when the row actually runs out of room, so short statuses ("ready",
  "provisioning", …) still render in full. The full text stays in the DOM for
  assistive tech and is surfaced on hover via title.
-->
<span
  data-testid="status-pill"
  title={displayText}
  class="inline-block min-w-0 max-w-full truncate align-middle rounded px-1.5 py-0.5 text-micro {tone}"
  >{display}</span
>
