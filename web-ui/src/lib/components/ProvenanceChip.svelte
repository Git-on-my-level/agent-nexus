<script>
  /**
   * Whether what follows is computed or hand-written, in the header where the
   * reader looks before reading the numbers.
   *
   * Always text first: "Live · updated 2m ago", "Written by claude · 3d ago",
   * "May be stale · written 9d ago". The amber on something past its review
   * date repeats what the words already say, so the signal survives
   * greyscale, a colour-blind reader and a printed page.
   *
   * The exact instant is the tooltip and the accessible name, the way
   * `AgeBadge` does it, so the line costs three words instead of a sentence.
   *
   * Pass a report `panel` (with its computed `freshness`), or a `model` built
   * by `liveProvenance` / `authoredProvenance` for a surface that is not a
   * report panel.
   */
  import { panelProvenance } from "$lib/reportProvenance.js";

  let {
    /** A report panel, after any live observation has been merged in. */
    panel = null,
    /** The panel's computed freshness, when the caller has it. */
    freshness = "",
    /** A pre-built provenance model, for non-panel surfaces. */
    model = null,
    /** Reference time, injectable so the chip is testable. */
    now = Date.now(),
    class: extraClass = "",
  } = $props();

  let line = $derived(model ?? panelProvenance(panel, freshness, now));
</script>

<p
  class={extraClass ? `provenance-chip ${extraClass}` : "provenance-chip"}
  data-anx-provenance={line.state}
  data-anx-provenance-class={line.class}
  title={line.title}
>
  <span class="provenance-mark" aria-hidden="true"></span><span
    >{line.lead}{#if line.age}<time
        datetime={line.datetime}
        aria-label={line.title}>{line.age}</time
      >{/if}</span
  >
</p>

<style>
  .provenance-chip {
    display: inline-flex;
    align-items: baseline;
    gap: 5px;
    flex: none;
    max-width: 100%;
    padding: 2px 7px;
    border: 1px solid var(--line);
    border-radius: 999px;
    color: var(--fg-muted);
    font-size: 10px;
    line-height: 1.5;
    overflow-wrap: anywhere;
    cursor: help;
  }
  .provenance-mark {
    flex: none;
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--fg-subtle);
    align-self: center;
  }
  /* Live reads forward: a filled accent dot and the workspace's own border. */
  .provenance-chip[data-anx-provenance="live"] {
    color: var(--fg);
    border-color: var(--line-strong);
  }
  .provenance-chip[data-anx-provenance="live"] .provenance-mark {
    background: var(--accent);
  }
  .provenance-chip[data-anx-provenance="live-pending"] .provenance-mark,
  .provenance-chip[data-anx-provenance="live-unavailable"] .provenance-mark {
    background: transparent;
    border: 1px solid var(--fg-subtle);
  }
  .provenance-chip[data-anx-provenance="live-stale"],
  .provenance-chip[data-anx-provenance="due-for-review"] {
    color: var(--warn-text);
    border-color: var(--warn);
  }
  .provenance-chip[data-anx-provenance="live-stale"] .provenance-mark,
  .provenance-chip[data-anx-provenance="due-for-review"] .provenance-mark {
    background: var(--warn);
  }
  /* Hand-written recedes: the same line, no dot weight, muted text. */
  .provenance-chip[data-anx-provenance="authored"] {
    border-color: var(--line-subtle);
  }
  .provenance-chip[data-anx-provenance="authored"] .provenance-mark {
    background: transparent;
    border: 1px dashed var(--fg-subtle);
  }
  time {
    font-variant-numeric: tabular-nums;
  }
</style>
