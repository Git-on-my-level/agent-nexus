<script>
  let {
    title,
    lastUpdated = "2026-04-25",
    children,
  } = $props();
</script>

<article class="mx-auto max-w-2xl space-y-4 text-fg">
  <header class="space-y-1 border-b border-line pb-4">
    <h1 class="text-xl font-semibold text-fg">{title}</h1>
    <p class="text-micro text-fg-subtle">Last updated: {lastUpdated}</p>
  </header>
  <div
    class="space-y-4 text-sm leading-relaxed [&_h2]:mt-6 [&_h2]:text-base [&_h2]:font-semibold [&_h2]:text-fg [&_p]:text-fg [&_ul]:list-disc [&_ul]:pl-5 [&_li]:text-fg [&_a]:text-accent-text [&_a]:underline"
  >
    {@render children?.()}
  </div>
</article>
