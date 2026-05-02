<script>
  /** Styling-only outer shell for workspace pages (spacing, width). No data fetching. */
  let {
    children,
    class: clazz = "",
    "data-tour": dataTour = undefined,
  } = $props();
</script>

<div class="min-w-0 max-w-full space-y-5 {clazz}" data-tour={dataTour}>
  {@render children()}
</div>
