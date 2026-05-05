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
</script>

<span class="shrink-0 rounded px-1.5 py-0.5 text-micro {tone}">{display}</span>
