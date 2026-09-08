<script>
  let { tone = "neutral", children } = $props();
  const colors = {
    neutral: "bg-line text-fg-muted",
    warn: "bg-warn-soft text-warn-text",
    danger: "bg-danger-soft text-danger-text",
    ok: "bg-ok-soft text-ok-text",
  };
</script>

<span
  class="inline-flex max-w-full items-center rounded px-1.5 py-0.5 text-micro font-medium {colors[
    tone
  ] || colors.neutral}">{@render children?.()}</span
>
