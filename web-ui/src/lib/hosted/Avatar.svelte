<script>
  /**
   * Square avatar with deterministic colour seeded by `seed`.
   * Shares palette/initials with workspace ActorAvatar via avatarModel.
   */

  import {
    avatarSizeClasses,
    initialsOf,
    paletteForSeed,
  } from "$lib/avatarModel.js";

  /** @type {{
   *   label?: string,
   *   seed?: string,
   *   size?: 'xs'|'sm'|'md'|'lg',
   *   class?: string,
   * }} */
  let { label = "", seed = "", size = "md", class: extraClass = "" } = $props();

  const palette = $derived(paletteForSeed(seed, label));
  const initials = $derived(initialsOf(label));
  const sizeClasses = $derived(avatarSizeClasses(size));
</script>

<span
  aria-hidden="true"
  class="inline-flex shrink-0 items-center justify-center font-semibold uppercase {sizeClasses} {palette.bg} {palette.fg} {extraClass}"
>
  {initials}
</span>
