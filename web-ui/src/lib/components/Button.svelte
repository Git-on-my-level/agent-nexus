<script>
  let {
    variant = "secondary",
    size = "default",
    disabled = false,
    type = "button",
    href = undefined,
    busy = false,
    class: className = "",
    children,
    leading,
    trailing,
    ...rest
  } = $props();

  let effectiveDisabled = $derived(disabled || busy);

  let sizeClass = $derived(
    size === "compact"
      ? "h-7 px-3 text-micro"
      : size === "large"
        ? "h-10 px-4 text-body"
        : "h-8 px-4 text-micro",
  );

  let weightClass = $derived(variant === "primary" ? "font-medium" : "font-normal");

  let variantClass = $derived(
    variant === "primary"
      ? "bg-accent text-white hover:bg-accent-hover"
      : variant === "secondary"
        ? "border border-line bg-transparent text-fg hover:bg-panel-hover"
        : variant === "ghost"
          ? "bg-transparent text-fg-muted hover:bg-panel-hover"
          : "bg-transparent text-danger-text hover:bg-danger-soft",
  );

  let classes = $derived(
    [
      "inline-flex items-center justify-center gap-1.5 rounded cursor-pointer transition-colors",
      "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2",
      "disabled:opacity-50 disabled:cursor-not-allowed",
      sizeClass,
      weightClass,
      variantClass,
      className,
    ].join(" "),
  );
</script>

{#if href}
  <a
    {href}
    class={classes}
    aria-busy={busy || undefined}
    role="button"
    {...rest}
  >
    {#if busy}
      <svg
        class="h-3.5 w-3.5 shrink-0 animate-spin"
        viewBox="0 0 24 24"
        fill="none"
        aria-hidden="true"
      >
        <circle
          cx="12"
          cy="12"
          r="10"
          stroke="currentColor"
          stroke-width="3"
          opacity="0.25"
        />
        <path
          d="M12 2a10 10 0 0 1 10 10"
          stroke="currentColor"
          stroke-width="3"
          stroke-linecap="round"
        />
      </svg>
    {:else}
      {@render leading?.()}
    {/if}
    {@render children?.()}
    {@render trailing?.()}
  </a>
{:else}
  <button
    {type}
    disabled={effectiveDisabled}
    class={classes}
    aria-busy={busy || undefined}
    {...rest}
  >
    {#if busy}
      <svg
        class="h-3.5 w-3.5 shrink-0 animate-spin"
        viewBox="0 0 24 24"
        fill="none"
        aria-hidden="true"
      >
        <circle
          cx="12"
          cy="12"
          r="10"
          stroke="currentColor"
          stroke-width="3"
          opacity="0.25"
        />
        <path
          d="M12 2a10 10 0 0 1 10 10"
          stroke="currentColor"
          stroke-width="3"
          stroke-linecap="round"
        />
      </svg>
    {:else}
      {@render leading?.()}
    {/if}
    {@render children?.()}
    {@render trailing?.()}
  </button>
{/if}
