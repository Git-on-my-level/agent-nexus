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
    children,
  } = $props();

  const shell =
    "ref-chip compact-ref-link inline-flex min-w-0 max-w-full items-baseline gap-1 rounded border border-[var(--line)] bg-[var(--bg)] px-1.5 py-0.5 text-micro leading-tight focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-solid/40";
  let accentClasses = $derived(
    accentText
      ? "text-accent-text hover:border-[var(--line-strong)] hover:text-accent-text"
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
