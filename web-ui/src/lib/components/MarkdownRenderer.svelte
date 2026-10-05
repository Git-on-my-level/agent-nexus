<script>
  /**
   * The one markdown surface. Card bodies, docs, reports, ask text, tile
   * descriptions and previews all render through here, so GFM support, HTML
   * handling and sanitization are decided once in `$lib/markdown.js` rather
   * than per page.
   *
   * Ref chips: `renderMarkdown` emits a `<span data-md-ref>` placeholder for
   * each ANX ref and work URL it finds in prose, and this mounts the real
   * `AnxRefChip` into it so the chip carries the same hover preview it does
   * everywhere else. Pass `resolved` (a page-level batch resolve) to get
   * titles and status on those chips; without one they render from the ref
   * itself, which is still the right text and still links.
   */
  import { mount, unmount } from "svelte";

  import AnxRefChip from "$lib/components/AnxRefChip.svelte";
  import { renderMarkdown } from "$lib/markdown.js";

  let {
    source = "",
    class: className = "",
    inline = false,
    /** Page-level batch resolve result, from `indexResolvedRefs`. */
    resolved = new Map(),
    organizationSlug = "",
    workspaceSlug = "",
    onpreview = null,
    onpreviewclose = null,
    /** Off for a surface with no refs worth chipping (an editor preview). */
    refChips = true,
  } = $props();

  let html = $derived(renderMarkdown(source, { inline, refChips }));
  let root = $state();

  $effect(() => {
    // Re-read the rendered markup and the resolve result: a chip's title
    // arrives with the batch resolve, after the body is already on screen.
    void html;
    void resolved;
    const host = root;
    if (!host || !refChips) return;

    const chips = [];
    for (const node of host.querySelectorAll("[data-md-ref]")) {
      const refValue = node.getAttribute("data-md-ref") || "";
      if (!refValue) continue;
      // The placeholder's own text is the no-JS fallback; the chip replaces it.
      node.textContent = "";
      node.classList.add("md-ref--mounted");
      chips.push(
        mount(AnxRefChip, {
          target: node,
          props: {
            refValue,
            resolved,
            organizationSlug,
            workspaceSlug,
            showKind: false,
            onpreview,
            onpreviewclose,
          },
        }),
      );
    }

    return () => {
      for (const chip of chips) unmount(chip);
    };
  });
</script>

{#if html}
  {#if inline}
    <span bind:this={root} class="markdown-rendered {className}">
      <!-- eslint-disable-next-line svelte/no-at-html-tags -- output is sanitized by renderMarkdown -->
      {@html html}
    </span>
  {:else}
    <article bind:this={root} class="markdown-rendered {className}">
      <!-- eslint-disable-next-line svelte/no-at-html-tags -- output is sanitized by renderMarkdown -->
      {@html html}
    </article>
  {/if}
{/if}
