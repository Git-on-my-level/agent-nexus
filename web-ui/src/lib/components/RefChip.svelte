<script>
  /**
   * Shared chrome for ref pills (compact-ref-link breakpoint CSS applies here too).
   */
  let {
    href = "",
    external = false,
    title = "",
    /** @type {string} */
    class: clazz = "",
    navigable = true,
    ariaLabel = "",
    ariaBusy = null,
    role = null,
    accentText = true,
    /** Use inside an outer bordered row (e.g. ref composer); drops inner chrome. */
    embedded = false,
    children,
  } = $props();

  const chipBase =
    "ref-chip compact-ref-link inline-flex min-w-0 max-w-full gap-1 text-micro leading-tight focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-solid/40";
  let shell = $derived(
    embedded
      ? `${chipBase} min-h-[26px] flex-1 items-center rounded-none border-0 bg-transparent px-2 py-0`
      : `${chipBase} items-baseline rounded border border-[var(--line)] bg-[var(--bg)] px-1.5 py-0.5`,
  );
  let accentClasses = $derived(
    accentText
      ? embedded
        ? "text-accent-text hover:text-accent-text"
        : "text-accent-text hover:border-[var(--line-strong)] hover:text-accent-text"
      : "text-[var(--fg-muted)]",
  );
</script>

{#if navigable && href}
  <a
    class="{shell} {accentClasses} {clazz}"
    {href}
    rel={external ? "noreferrer noopener" : undefined}
    target={external ? "_blank" : undefined}
    {title}
    aria-label={ariaLabel || undefined}
    aria-busy={ariaBusy ?? undefined}
  >
    {@render children?.()}
  </a>
{:else}
  <span
    class="{shell} {accentClasses} {clazz}"
    {title}
    aria-label={ariaLabel || undefined}
    aria-busy={ariaBusy ?? undefined}
    {role}
  >
    {@render children?.()}
  </span>
{/if}
