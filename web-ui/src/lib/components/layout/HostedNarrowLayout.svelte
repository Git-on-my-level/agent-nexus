<script>
  let { children, maxWidthClass = "max-w-md", class: clazz = "" } = $props();
</script>

<div class="mx-auto px-4 py-8 {maxWidthClass} {clazz}">
  {@render children()}
</div>
