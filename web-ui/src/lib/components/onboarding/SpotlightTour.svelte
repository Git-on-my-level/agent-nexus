<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";

  import Button from "$lib/components/Button.svelte";

  /**
   * @typedef {{
   *   selector?: string,
   *   placement?: "auto" | "center",
   *   eyebrow?: string,
   *   title: string,
   *   body: string,
   *   ctaLabel?: string,
   *   ctaHref?: string,
   *   primaryLabel?: string,
   *   skipLabel?: string,
   * }} TourStep
   */

  /** @type {{ steps: TourStep[], onClose: (reason?: string) => void, open: boolean }} */
  let { steps, onClose, open = $bindable(true) } = $props();

  let index = $state(0);
  let hole = $state(
    /** @type {{ top: number, left: number, width: number, height: number } | null} */ (
      null
    ),
  );
  let noTarget = $state(false);
  let panelRef = $state(/** @type {HTMLElement | null} */ (null));
  let cardSize = $state({ width: 360, height: 240 });
  let viewport = $state({ width: 1280, height: 800 });
  let mounted = $state(false);

  const PAD = 8;
  const GAP = 18;
  const MIN_MARGIN = 16;

  let step = $derived(steps[index] ?? null);
  let isCenter = $derived(
    !step?.selector || step?.placement === "center" || noTarget,
  );
  let isLast = $derived(index >= steps.length - 1);
  let isFirst = $derived(index === 0);
  let showCta = $derived(isLast && Boolean(step?.ctaLabel && step?.ctaHref));

  function focusPanel() {
    if (!browser) return;
    requestAnimationFrame(() => {
      panelRef?.focus?.();
    });
  }

  function measure() {
    if (!browser) return;
    viewport = { width: window.innerWidth, height: window.innerHeight };
    if (!step || !step.selector || step.placement === "center") {
      hole = null;
      noTarget = false;
      return;
    }
    const found = findVisibleTarget(step.selector);
    if (!found) {
      hole = null;
      noTarget = true;
      return;
    }
    noTarget = false;
    try {
      found.scrollIntoView({
        behavior: "smooth",
        block: "nearest",
        inline: "nearest",
      });
    } catch {
      /* noop */
    }
    const r = found.getBoundingClientRect();
    hole = {
      top: r.top - PAD,
      left: r.left - PAD,
      width: r.width + PAD * 2,
      height: r.height + PAD * 2,
    };
  }

  function measureCard() {
    if (!browser || !panelRef) return;
    const r = panelRef.getBoundingClientRect();
    if (r.width > 0 && r.height > 0) {
      cardSize = { width: r.width, height: r.height };
    }
  }

  /** @param {string} selector */
  function findVisibleTarget(selector) {
    try {
      const nodes = document.querySelectorAll(selector);
      for (const el of nodes) {
        if (!(el instanceof Element)) continue;
        const r = el.getBoundingClientRect();
        const style = window.getComputedStyle(el);
        if (style.display === "none" || style.visibility === "hidden") {
          continue;
        }
        if (r.width < 2 || r.height < 2) continue;
        return el;
      }
    } catch {
      return null;
    }
    return null;
  }

  $effect(() => {
    if (!open || !browser) return;
    void index;
    void open;
    mounted = true;
    measure();
    requestAnimationFrame(() => {
      measureCard();
      focusPanel();
    });
  });

  $effect(() => {
    if (!browser || !open) return;
    const onScrollOrResize = () => {
      measure();
      measureCard();
    };
    window.addEventListener("resize", onScrollOrResize);
    window.addEventListener("scroll", onScrollOrResize, true);
    return () => {
      window.removeEventListener("resize", onScrollOrResize);
      window.removeEventListener("scroll", onScrollOrResize, true);
    };
  });

  function close(reason = "dismiss") {
    open = false;
    onClose?.(reason);
  }

  function next() {
    if (index < steps.length - 1) {
      index += 1;
    }
  }

  function prev() {
    if (index > 0) {
      index -= 1;
    }
  }

  /** @param {number} i */
  function goTo(i) {
    if (i >= 0 && i < steps.length) {
      index = i;
    }
  }

  /** @param {KeyboardEvent} e */
  function onKeydown(e) {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      close("escape");
      return;
    }
    if (e.key === "ArrowRight" || e.key === "Enter") {
      // Enter advances unless focused on a button (let the button click)
      if (
        e.key === "Enter" &&
        e.target instanceof HTMLElement &&
        (e.target.tagName === "BUTTON" || e.target.tagName === "A")
      ) {
        return;
      }
      e.preventDefault();
      if (showCta && isLast) {
        handleCta();
      } else if (!isLast) {
        next();
      } else {
        close("complete");
      }
      return;
    }
    if (e.key === "ArrowLeft") {
      e.preventDefault();
      prev();
    }
  }

  function handleCta() {
    const href = step?.ctaHref;
    close("cta");
    if (href) {
      void goto(href);
    }
  }

  /** @type {(v: number, lo: number, hi: number) => number} */
  function clamp(v, lo, hi) {
    if (Number.isNaN(v)) return lo;
    if (hi < lo) return lo;
    return Math.min(hi, Math.max(lo, v));
  }

  function computeDims() {
    if (!browser || !hole) {
      return null;
    }
    const w = viewport.width;
    const h = viewport.height;
    const t = Math.max(0, hole.top);
    const l = Math.max(0, hole.left);
    const r = Math.min(w, hole.left + hole.width);
    const b = Math.min(h, hole.top + hole.height);
    return {
      top: t,
      left: l,
      right: r,
      bottom: b,
      holeTop: t,
      holeLeft: l,
      holeW: r - l,
      holeH: b - t,
    };
  }

  function computeAnchor() {
    if (!browser || isCenter || !hole) {
      return { kind: "center" };
    }
    const w = viewport.width;
    const h = viewport.height;
    const cw = cardSize.width || 360;
    const ch = cardSize.height || 240;
    const rightSpace = w - (hole.left + hole.width);
    const leftSpace = hole.left;
    const belowSpace = h - (hole.top + hole.height);
    const aboveSpace = hole.top;

    if (rightSpace >= cw + GAP + MIN_MARGIN) {
      const left = hole.left + hole.width + GAP;
      const top = clamp(
        hole.top + hole.height / 2 - ch / 2,
        MIN_MARGIN,
        h - ch - MIN_MARGIN,
      );
      const arrowTop = clamp(hole.top + hole.height / 2 - top, 16, ch - 16);
      return { kind: "right", top, left, arrowTop };
    }
    if (belowSpace >= ch + GAP + MIN_MARGIN) {
      const top = hole.top + hole.height + GAP;
      const left = clamp(
        hole.left + hole.width / 2 - cw / 2,
        MIN_MARGIN,
        w - cw - MIN_MARGIN,
      );
      const arrowLeft = clamp(hole.left + hole.width / 2 - left, 20, cw - 20);
      return { kind: "below", top, left, arrowLeft };
    }
    if (aboveSpace >= ch + GAP + MIN_MARGIN) {
      const top = hole.top - ch - GAP;
      const left = clamp(
        hole.left + hole.width / 2 - cw / 2,
        MIN_MARGIN,
        w - cw - MIN_MARGIN,
      );
      const arrowLeft = clamp(hole.left + hole.width / 2 - left, 20, cw - 20);
      return { kind: "above", top, left, arrowLeft };
    }
    if (leftSpace >= cw + GAP + MIN_MARGIN) {
      const left = hole.left - cw - GAP;
      const top = clamp(
        hole.top + hole.height / 2 - ch / 2,
        MIN_MARGIN,
        h - ch - MIN_MARGIN,
      );
      const arrowTop = clamp(hole.top + hole.height / 2 - top, 16, ch - 16);
      return { kind: "left", top, left, arrowTop };
    }
    return { kind: "center" };
  }

  function computeCardStyle() {
    if (anchor.kind === "center") {
      return "left:50%;top:50%;transform:translate(-50%,-50%)";
    }
    return `left:${anchor.left}px;top:${anchor.top}px;transform:none`;
  }

  let dims = $derived(computeDims());
  let anchor = $derived(computeAnchor());
  let cardStyle = $derived(computeCardStyle());
  let progressIndices = $derived(steps.map((_s, i) => i));

  let primaryLabel = $derived(
    showCta && step?.ctaLabel
      ? step.ctaLabel
      : isLast
        ? "Done"
        : (step?.primaryLabel ?? "Next"),
  );
</script>

<svelte:window onkeydown={open ? onKeydown : undefined} />

{#if open && step}
  <div
    class="tour-spotlight-root"
    class:tour-mounted={mounted}
    data-testid="workspace-spotlight-tour"
    role="dialog"
    aria-modal="true"
    aria-label={step.title}
  >
    {#if dims && !isCenter}
      <div
        class="tour-dim tour-dim--top"
        style="top:0;left:0;right:0;height:{dims.top}px"
        aria-hidden="true"
        onclick={() => close("dim")}
      ></div>
      <div
        class="tour-dim tour-dim--left"
        style="top:{dims.holeTop}px;left:0;width:{dims.holeLeft}px;height:{dims.holeH}px"
        aria-hidden="true"
        onclick={() => close("dim")}
      ></div>
      <div
        class="tour-dim tour-dim--right"
        style="top:{dims.holeTop}px;left:{dims.holeLeft +
          dims.holeW}px;right:0;height:{dims.holeH}px"
        aria-hidden="true"
        onclick={() => close("dim")}
      ></div>
      <div
        class="tour-dim tour-dim--bottom"
        style="top:{dims.holeTop + dims.holeH}px;left:0;right:0;bottom:0"
        aria-hidden="true"
        onclick={() => close("dim")}
      ></div>
      <!-- Halo ring around the spotlighted target -->
      <div
        class="tour-halo"
        aria-hidden="true"
        style="top:{dims.holeTop}px;left:{dims.holeLeft}px;width:{dims.holeW}px;height:{dims.holeH}px"
      ></div>
    {:else}
      <div
        class="tour-dim tour-dim--full"
        aria-hidden="true"
        onclick={() => close("dim")}
      ></div>
    {/if}

    <div
      bind:this={panelRef}
      class="tour-card tour-card--{anchor.kind}"
      tabindex="-1"
      role="document"
      style={cardStyle}
    >
      {#if anchor.kind === "right"}
        <span
          class="tour-arrow tour-arrow--left"
          style="top:{anchor.arrowTop}px"
          aria-hidden="true"
        ></span>
      {:else if anchor.kind === "left"}
        <span
          class="tour-arrow tour-arrow--right"
          style="top:{anchor.arrowTop}px"
          aria-hidden="true"
        ></span>
      {:else if anchor.kind === "below"}
        <span
          class="tour-arrow tour-arrow--up"
          style="left:{anchor.arrowLeft}px"
          aria-hidden="true"
        ></span>
      {:else if anchor.kind === "above"}
        <span
          class="tour-arrow tour-arrow--down"
          style="left:{anchor.arrowLeft}px"
          aria-hidden="true"
        ></span>
      {/if}

      <div class="tour-card-header">
        <p class="tour-eyebrow">
          {step.eyebrow ?? `Step ${index + 1} of ${steps.length}`}
        </p>
        <button
          type="button"
          class="tour-close"
          aria-label="Close tour"
          onclick={() => close("close-button")}
        >
          <svg
            class="h-4 w-4"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
            stroke-width="2"
            aria-hidden="true"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              d="M6 18L18 6M6 6l12 12"
            />
          </svg>
        </button>
      </div>

      <h2 class="tour-title">{step.title}</h2>
      <p class="tour-body">{step.body}</p>

      <div class="tour-progress" aria-hidden="true">
        {#each progressIndices as i (i)}
          <button
            type="button"
            class="tour-progress-dot"
            class:tour-progress-dot--active={i === index}
            class:tour-progress-dot--done={i < index}
            tabindex="-1"
            aria-label={`Go to step ${i + 1}`}
            onclick={() => goTo(i)}
          ></button>
        {/each}
      </div>

      <div class="tour-actions">
        <Button
          variant="ghost"
          size="compact"
          type="button"
          onclick={() => close("skip")}
        >
          {step.skipLabel ?? (isFirst ? "Maybe later" : "Skip tour")}
        </Button>
        <div class="tour-nav">
          {#if !isFirst}
            <Button
              variant="secondary"
              size="compact"
              type="button"
              onclick={prev}
            >
              Back
            </Button>
          {/if}
          {#if showCta}
            <Button
              variant="primary"
              size="compact"
              type="button"
              onclick={handleCta}
            >
              {primaryLabel}
            </Button>
          {:else if !isLast}
            <Button
              variant="primary"
              size="compact"
              type="button"
              onclick={next}
            >
              {primaryLabel}
            </Button>
          {:else}
            <Button
              variant="primary"
              size="compact"
              type="button"
              onclick={() => close("complete")}
            >
              {primaryLabel}
            </Button>
          {/if}
        </div>
      </div>

      <p class="tour-keyhint" aria-hidden="true">
        <kbd>←</kbd> <kbd>→</kbd> to navigate &middot; <kbd>Esc</kbd> to close
      </p>
    </div>
  </div>
{/if}

<style>
  .tour-spotlight-root {
    position: fixed;
    inset: 0;
    z-index: 200;
    pointer-events: none;
  }
  .tour-spotlight-root :global(.tour-dim) {
    position: fixed;
    pointer-events: auto;
    background: rgba(6, 8, 14, 0.62);
    z-index: 0;
    transition:
      top var(--motion-base, 220ms cubic-bezier(0.2, 0, 0, 1)),
      left var(--motion-base, 220ms cubic-bezier(0.2, 0, 0, 1)),
      right var(--motion-base, 220ms cubic-bezier(0.2, 0, 0, 1)),
      bottom var(--motion-base, 220ms cubic-bezier(0.2, 0, 0, 1)),
      width var(--motion-base, 220ms cubic-bezier(0.2, 0, 0, 1)),
      height var(--motion-base, 220ms cubic-bezier(0.2, 0, 0, 1)),
      opacity 180ms ease-out;
    backdrop-filter: blur(2px);
  }
  .tour-dim--full {
    inset: 0;
    width: 100%;
    height: 100%;
    background: rgba(6, 8, 14, 0.72) !important;
  }

  /* Animated halo ring around the spotlighted target. */
  .tour-halo {
    position: fixed;
    z-index: 0;
    pointer-events: none;
    border-radius: 0.6rem;
    box-shadow:
      0 0 0 1.5px var(--accent, #22d3ee),
      0 0 0 6px color-mix(in srgb, var(--accent, #22d3ee) 22%, transparent),
      0 0 32px color-mix(in srgb, var(--accent, #22d3ee) 30%, transparent);
    transition:
      top var(--motion-base, 220ms cubic-bezier(0.2, 0, 0, 1)),
      left var(--motion-base, 220ms cubic-bezier(0.2, 0, 0, 1)),
      width var(--motion-base, 220ms cubic-bezier(0.2, 0, 0, 1)),
      height var(--motion-base, 220ms cubic-bezier(0.2, 0, 0, 1));
    animation: tour-halo-pulse 2.4s ease-in-out infinite;
  }
  @keyframes tour-halo-pulse {
    0%,
    100% {
      box-shadow:
        0 0 0 1.5px var(--accent, #22d3ee),
        0 0 0 6px color-mix(in srgb, var(--accent, #22d3ee) 22%, transparent),
        0 0 24px color-mix(in srgb, var(--accent, #22d3ee) 25%, transparent);
    }
    50% {
      box-shadow:
        0 0 0 1.5px var(--accent, #22d3ee),
        0 0 0 8px color-mix(in srgb, var(--accent, #22d3ee) 32%, transparent),
        0 0 36px color-mix(in srgb, var(--accent, #22d3ee) 45%, transparent);
    }
  }

  .tour-card {
    pointer-events: auto;
    position: fixed;
    z-index: 1;
    width: min(22.5rem, calc(100vw - 2rem));
    border-radius: 0.85rem;
    border: 1px solid var(--line);
    background: var(--panel);
    box-shadow: var(--shadow-modal, 0 24px 60px rgba(0, 0, 0, 0.55));
    padding: 1.05rem 1.15rem 0.9rem;
    outline: none;
    transition:
      top var(--motion-base, 220ms cubic-bezier(0.2, 0, 0, 1)),
      left var(--motion-base, 220ms cubic-bezier(0.2, 0, 0, 1)),
      transform var(--motion-base, 220ms cubic-bezier(0.2, 0, 0, 1));
    animation: tour-card-in 280ms cubic-bezier(0.2, 0.7, 0.2, 1) both;
  }
  .tour-card--center {
    width: min(28rem, calc(100vw - 2rem));
    padding: 1.4rem 1.5rem 1.15rem;
  }
  @keyframes tour-card-in {
    from {
      opacity: 0;
      transform: translateY(6px) scale(0.985);
    }
    to {
      opacity: 1;
    }
  }
  .tour-card--center {
    animation-name: tour-card-in-center;
  }
  @keyframes tour-card-in-center {
    from {
      opacity: 0;
      transform: translate(-50%, calc(-50% + 8px)) scale(0.985);
    }
    to {
      opacity: 1;
      transform: translate(-50%, -50%);
    }
  }

  /* Pointer arrow, drawn as a rotated square with matching border. */
  .tour-arrow {
    position: absolute;
    width: 12px;
    height: 12px;
    background: var(--panel);
    border: 1px solid var(--line);
    transform: rotate(45deg);
  }
  .tour-arrow--left {
    left: -7px;
    border-right: none;
    border-top: none;
  }
  .tour-arrow--right {
    right: -7px;
    border-left: none;
    border-bottom: none;
  }
  .tour-arrow--up {
    top: -7px;
    border-right: none;
    border-bottom: none;
  }
  .tour-arrow--down {
    bottom: -7px;
    border-left: none;
    border-top: none;
  }

  .tour-card-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.5rem;
    margin-bottom: 0.45rem;
  }
  .tour-eyebrow {
    font-size: 0.68rem;
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--accent-text, var(--accent));
    margin: 0;
  }
  .tour-close {
    margin: -0.25rem -0.25rem 0 0;
    padding: 0.25rem;
    color: var(--fg-muted);
    background: none;
    border: none;
    border-radius: 0.3rem;
    cursor: pointer;
    transition:
      color 120ms ease,
      background-color 120ms ease;
  }
  .tour-close:hover {
    color: var(--fg);
    background: var(--panel-hover, rgba(255, 255, 255, 0.06));
  }
  .tour-title {
    font-size: 1.1rem;
    font-weight: 600;
    color: var(--fg);
    margin: 0 0 0.4rem 0;
    line-height: 1.25;
  }
  .tour-card--center .tour-title {
    font-size: 1.35rem;
    line-height: 1.2;
  }
  .tour-body {
    font-size: 0.9rem;
    line-height: 1.5;
    color: var(--fg-subtle, var(--fg-muted));
    margin: 0 0 0.85rem 0;
  }
  .tour-card--center .tour-body {
    font-size: 0.95rem;
    margin-bottom: 1.1rem;
  }

  .tour-progress {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0 0 0.85rem 0;
  }
  .tour-progress-dot {
    appearance: none;
    border: none;
    background: var(--line);
    height: 4px;
    width: 18px;
    border-radius: 999px;
    padding: 0;
    cursor: pointer;
    transition:
      background-color 160ms ease,
      width 220ms ease;
  }
  .tour-progress-dot--done {
    background: color-mix(in srgb, var(--accent, #22d3ee) 55%, transparent);
  }
  .tour-progress-dot--active {
    background: var(--accent, #22d3ee);
    width: 28px;
  }

  .tour-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
  }
  .tour-nav {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    margin-left: auto;
  }

  .tour-keyhint {
    margin: 0.65rem 0 0 0;
    text-align: right;
    font-size: 0.65rem;
    letter-spacing: 0.04em;
    color: var(--fg-muted);
    opacity: 0.7;
  }
  .tour-keyhint kbd {
    display: inline-block;
    min-width: 1.1rem;
    padding: 0 0.25rem;
    border: 1px solid var(--line);
    border-radius: 0.25rem;
    background: var(--bg-soft, rgba(255, 255, 255, 0.04));
    font-family: inherit;
    font-size: 0.65rem;
    line-height: 1.2;
    color: var(--fg-muted);
  }

  @media (max-width: 640px) {
    .tour-card,
    .tour-card--center {
      left: 50% !important;
      top: auto !important;
      bottom: 16px;
      transform: translateX(-50%) !important;
      width: calc(100vw - 1.5rem);
      animation: tour-card-in-mobile 240ms cubic-bezier(0.2, 0.7, 0.2, 1) both;
    }
    .tour-arrow {
      display: none;
    }
    .tour-keyhint {
      display: none;
    }
  }
  @keyframes tour-card-in-mobile {
    from {
      opacity: 0;
      transform: translate(-50%, 12px);
    }
    to {
      opacity: 1;
      transform: translateX(-50%);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .tour-spotlight-root :global(.tour-dim),
    .tour-card,
    .tour-card--center,
    .tour-halo {
      transition: none !important;
      animation: none !important;
    }
    .tour-progress-dot {
      transition: none !important;
    }
  }
</style>
