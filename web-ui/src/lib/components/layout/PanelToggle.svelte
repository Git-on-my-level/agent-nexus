<script>
  /**
   * The button that collapses or expands a side panel.
   *
   * One chevron, pointed at where the panel will go. It stays in the same
   * place in both states so it is not a control that moves when you use it,
   * and it says which panel it acts on so a page with both a nav and a rail
   * has two distinguishable buttons.
   *
   * In a window too narrow for the panel the toggle is disabled and says so
   * on hover, rather than being a button that does nothing.
   */
  let {
    /** Human name of the panel: "menu", "details". */
    label = "panel",
    /** Which edge the panel is on: `left` | `right`. */
    side = "left",
    collapsed = false,
    /** True when the window, not the viewer, is what collapsed it. */
    auto = false,
    onToggle = () => {},
    class: extraClass = "",
  } = $props();

  // Collapsing a left panel points left; expanding it points right.
  let pointsRight = $derived(side === "left" ? collapsed : !collapsed);
  let action = $derived(collapsed ? "Show" : "Hide");
  let title = $derived(
    auto
      ? `${label[0].toUpperCase()}${label.slice(1)} is hidden because the window is narrow`
      : `${action} ${label}`,
  );
</script>

<button
  class="panel-toggle {extraClass}"
  type="button"
  aria-expanded={!collapsed}
  aria-label={`${action} ${label}`}
  data-panel-toggle={side}
  disabled={auto}
  {title}
  onclick={() => onToggle(!collapsed)}
>
  <svg
    class="panel-toggle__icon"
    class:panel-toggle__icon--flip={pointsRight}
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path d="M15 6l-6 6 6 6" />
  </svg>
</button>

<style>
  .panel-toggle {
    display: inline-flex;
    flex: none;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    padding: 0;
    border: 1px solid transparent;
    border-radius: var(--radius);
    background: transparent;
    color: var(--fg-muted);
    cursor: pointer;
    transition:
      background 120ms ease,
      color 120ms ease;
  }
  .panel-toggle:hover:not(:disabled) {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .panel-toggle:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .panel-toggle:focus-visible {
    outline: 2px solid var(--accent-solid);
    outline-offset: 1px;
  }
  .panel-toggle__icon {
    width: 14px;
    height: 14px;
  }
  .panel-toggle__icon--flip {
    transform: scaleX(-1);
  }
</style>
