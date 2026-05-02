<script>
  /** Narrow column + title for create/edit resource forms. Styling only. */
  let {
    children,
    title = "",
    maxWidthClass = "max-w-2xl",
    class: clazz = "",
  } = $props();
</script>

<div class="mx-auto px-4 py-6 {maxWidthClass} {clazz}">
  {#if title}
    <h1 class="mt-2 text-subtitle font-semibold text-fg">{title}</h1>
  {/if}
  <div class={title ? "mt-4" : ""}>{@render children()}</div>
</div>
