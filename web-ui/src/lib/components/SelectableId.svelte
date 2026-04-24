<script>
  import { selectNodeText } from "$lib/dom/selectNodeText.js";

  let { id = "", className = "" } = $props();
</script>

<!-- Full ID in the DOM; visually truncated with ellipsis when needed. Double-click to select all, then copy. -->
<span
  class="block min-w-0 max-w-full cursor-text truncate font-mono {className}"
  title="Double-click to select full ID, then copy"
  role="button"
  tabindex="0"
  ondblclick={(e) => {
    e.preventDefault();
    selectNodeText(e.currentTarget);
  }}
  onkeydown={(e) => {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      selectNodeText(e.currentTarget);
    }
  }}>{id}</span
>
