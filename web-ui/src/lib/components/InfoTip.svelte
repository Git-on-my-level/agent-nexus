<script>
  /**
   * The caveat behind a label, as a tooltip rather than a fold.
   *
   * These sentences — "participation does not assign a task", "created here,
   * so there is nothing to read back from" — are real and occasionally
   * load-bearing, but a reader meets them on every visit and needs them on
   * almost none. They used to be folded away behind a `<details>`, which
   * still spent a visible line on `▶ What evidence means` under every quiet
   * section and made the page read like a list of things the UI wanted to
   * explain about itself.
   *
   * This spends a glyph instead. It is the shared `use:tooltip` trigger, so it
   * opens on hover, on keyboard focus and on a touch hold, and the whole page
   * still costs one floating layer.
   *
   * Keep `text` to one or two short sentences. Anything longer belongs in the
   * surface itself, or in the docs.
   */
  import { tooltip } from "$lib/actions/tooltip.js";

  let {
    /** The sentence. Plain text: this is a tooltip, not a popover. */
    text = "",
    /**
     * What the sentence is about, for a reader who cannot see where the glyph
     * sits — "What evidence means". Read out as "<label>: <text>".
     */
    label = "",
    class: extraClass = "",
  } = $props();

  let title = $derived(String(text ?? "").trim());
  let name = $derived(
    [String(label ?? "").trim(), title].filter(Boolean).join(": "),
  );
</script>

{#if title}
  <button
    class={extraClass ? `info-tip ${extraClass}` : "info-tip"}
    type="button"
    aria-label={name}
    data-info-tip
    use:tooltip={title}
    onclick={(event) => event.preventDefault()}>i</button
  >
{/if}

<style>
  /*
   * A glyph, sized to sit on a text baseline beside a label without changing
   * its line height.
   */
  .info-tip {
    display: inline-flex;
    flex: none;
    align-items: center;
    justify-content: center;
    width: 13px;
    height: 13px;
    padding: 0;
    border: 1px solid var(--line-strong);
    border-radius: 50%;
    background: transparent;
    color: var(--fg-subtle, var(--fg-muted));
    font-size: 9px;
    font-style: italic;
    font-weight: 600;
    line-height: 1;
    vertical-align: middle;
    cursor: help;
  }
  .info-tip:hover,
  .info-tip:focus-visible {
    color: var(--fg);
    border-color: var(--fg-muted);
  }
  .info-tip:focus-visible {
    outline: 2px solid var(--accent-solid);
    outline-offset: 1px;
  }
</style>
