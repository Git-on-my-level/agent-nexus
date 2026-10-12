<script>
  /**
   * An inbox item's heading: two lines at most, and never a wall of bold.
   *
   * `text` is the already-clamped heading from `inboxItemText`; whatever the
   * title had beyond it is rendered as body prose below, so this never hides
   * the ask. What it can still clip is a heading that wraps past two lines on
   * a narrow screen, and that is what "Show full title" reveals. `full` is the
   * untouched title, offered on hover for the same reason.
   *
   * Subjects are written by the asking agent and routinely carry a bare
   * 64-char correlation id, a ref or a URL — nothing for the browser to break
   * on — so the heading wraps anywhere rather than scrolling the page
   * sideways at phone width.
   */
  let { text = "", full = "", level = 1, class: className = "" } = $props();

  let node = $state(null);
  let expanded = $state(false);
  let clipped = $state(false);

  let headingText = $derived(String(text ?? "").trim());
  let fullText = $derived(String(full ?? "").trim());
  let hoverTitle = $derived(
    fullText && fullText !== headingText ? fullText : undefined,
  );
  let tag = $derived(
    Number.isInteger(level) && level >= 1 && level <= 6 ? `h${level}` : "h2",
  );

  function measure() {
    // Only measurable while the clamp is on; expanded keeps the last answer so
    // the control that expanded it does not vanish under the reader.
    if (!node || expanded) return;
    clipped = node.scrollHeight - node.clientHeight > 1;
  }

  $effect(() => {
    void headingText;
    void expanded;
    const host = node;
    if (!host) return;
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => measure());
    observer.observe(host);
    return () => observer.disconnect();
  });
</script>

{#if headingText}
  <div class="space-y-0.5">
    <svelte:element
      this={tag}
      bind:this={node}
      class="text-subtitle font-semibold leading-snug text-fg [overflow-wrap:anywhere] {expanded
        ? ''
        : 'line-clamp-2'} {className}"
      title={hoverTitle}
      data-inbox-heading
      data-inbox-heading-expanded={expanded ? "true" : undefined}
    >
      {headingText}
    </svelte:element>
    {#if clipped}
      <button
        class="text-micro text-fg-muted underline underline-offset-2 hover:text-fg"
        type="button"
        aria-expanded={expanded}
        data-inbox-heading-expand
        onclick={() => (expanded = !expanded)}
        >{expanded ? "Show less" : "Show full title"}</button
      >
    {/if}
  </div>
{/if}
