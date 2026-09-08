<script>
  let { tone = "neutral", children } = $props();
  const colors = {
    neutral: "bg-bg-soft text-fg-muted ring-1 ring-inset ring-line",
    warn: "bg-warn-soft text-warn-text",
    danger: "bg-danger-soft text-danger-text",
    ok: "bg-ok-soft text-ok-text",
  };
</script>

<span
  class="inline-flex max-w-full items-center whitespace-nowrap rounded px-1.5 py-0.5 text-micro font-medium leading-none {colors[
    tone
  ] || colors.neutral}"
  style="padding-top:3px;padding-bottom:3px">{@render children?.()}</span
>
