<script>
  import ActorAvatar from "$lib/components/ActorAvatar.svelte";
  import { truncateActorDisplayName } from "$lib/avatarModel.js";
  import { formatTimestamp } from "$lib/formatDate";

  /** @type {{
   *   label?: string,
   *   seed?: string,
   *   size?: 'xs'|'sm'|'md'|'lg',
   *   timestamp?: string,
   *   prefix?: string,
   *   truncate?: boolean,
   *   nameClass?: string,
   *   class?: string,
   *   avatarClass?: string,
   * }} */
  let {
    label = "",
    seed = "",
    size = "sm",
    timestamp = "",
    prefix = "",
    truncate = true,
    nameClass = "text-meta font-semibold leading-tight text-fg",
    class: extraClass = "",
    avatarClass = "",
  } = $props();

  let displayLine = $derived(
    truncate
      ? truncateActorDisplayName(label)
      : String(label ?? "").trim() || "—",
  );
  let tsLine = $derived(timestamp ? formatTimestamp(timestamp) || "—" : "");
</script>

<span class="inline-flex min-w-0 items-center gap-2 {extraClass}">
  <ActorAvatar {label} {seed} {size} class={avatarClass} />
  <span class="flex min-w-0 items-center gap-1">
    {#if prefix}
      <span class="shrink-0 text-micro text-fg-muted">{prefix}</span>
    {/if}
    <span class="min-w-0 truncate {nameClass}" title={label}>{displayLine}</span
    >
    {#if tsLine}
      <span class="shrink-0 text-micro leading-tight text-fg-muted"
        >{tsLine}</span
      >
    {/if}
  </span>
</span>
