<script>
  import { renderMarkdown } from "$lib/markdown.js";

  let { source = "", class: className = "", inline = false } = $props();

  let html = $derived(renderMarkdown(source, { inline }));
</script>

{#if html}
  {#if inline}
    <span class="markdown-rendered {className}">
      <!-- eslint-disable-next-line svelte/no-at-html-tags -- output is sanitized by renderMarkdown -->
      {@html html}
    </span>
  {:else}
    <article class="markdown-rendered {className}">
      <!-- eslint-disable-next-line svelte/no-at-html-tags -- output is sanitized by renderMarkdown -->
      {@html html}
    </article>
  {/if}
{/if}
