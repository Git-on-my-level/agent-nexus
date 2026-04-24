<script>
  import {
    normalizeTopicType,
    topicTypeColorVarName,
  } from "$lib/topicTypeGlyph.js";

  let {
    type = "other",
    class: className = "",
    title: titleProp = "",
  } = $props();

  let kind = $derived(normalizeTopicType(type));
  let colorName = $derived(topicTypeColorVarName(type));
  let label = $derived(titleProp.trim() || `Topic type: ${kind}`);
</script>

<span
  class="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded border border-[var(--line)] bg-[var(--bg-soft)] {className}"
  style:color="var({colorName})"
  role="img"
  aria-label={label}
  title={label}
>
  {#if kind === "initiative"}
    <svg
      class="h-3.5 w-3.5"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      stroke-width="1.5"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <path d="M8 3.5v9M5 7.5l3-3.5 3 3.5" />
    </svg>
  {:else if kind === "objective"}
    <svg
      class="h-3.5 w-3.5"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      stroke-width="1.5"
      aria-hidden="true"
    >
      <circle cx="8" cy="8" r="5.25" />
      <circle cx="8" cy="8" r="1.75" fill="currentColor" stroke="none" />
    </svg>
  {:else if kind === "decision"}
    <svg
      class="h-3.5 w-3.5"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      stroke-width="1.5"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <path d="M8 2.5l5 5-5 5-5-5z" />
    </svg>
  {:else if kind === "incident"}
    <svg
      class="h-3.5 w-3.5"
      viewBox="0 0 16 16"
      fill="currentColor"
      stroke="none"
      aria-hidden="true"
    >
      <path d="M9.5 1.5L3.2 9.3h2.8L4.2 15l6.8-5.1H7.1L9.3 1.5H9.5z" />
    </svg>
  {:else if kind === "risk"}
    <svg
      class="h-3.5 w-3.5"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      stroke-width="1.5"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <path d="M8 3l5.5 9H2.5L8 3z" />
      <path d="M8 6.5V9" />
      <path d="M8 11h.01" />
    </svg>
  {:else if kind === "request"}
    <svg
      class="h-3.5 w-3.5"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      stroke-width="1.5"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <path d="M3.5 6.5a4.5 4.5 0 1 0 9 0" />
      <path d="M8 2v2.5" />
      <path d="M5 10.5L8 14l3-3.5" />
    </svg>
  {:else if kind === "note"}
    <svg
      class="h-3.5 w-3.5"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      stroke-width="1.5"
      stroke-linecap="round"
      aria-hidden="true"
    >
      <path
        d="M4.5 3.5h7a1 1 0 0 1 1 1v7l-2.5 2.5H4.5a1 1 0 0 1-1-1v-8a1 1 0 0 1 1-1z"
      />
      <path d="M9.5 3.5v3H13" />
      <path d="M5.5 9.5h5" />
      <path d="M5.5 11.5h3" />
    </svg>
  {:else}
    <svg
      class="h-3.5 w-3.5"
      viewBox="0 0 16 16"
      fill="currentColor"
      stroke="none"
      aria-hidden="true"
    >
      <rect x="2.5" y="2.5" width="4" height="4" rx="0.8" />
      <rect x="9.5" y="2.5" width="4" height="4" rx="0.8" />
      <rect x="2.5" y="9.5" width="4" height="4" rx="0.8" />
      <rect x="9.5" y="9.5" width="4" height="4" rx="0.8" />
    </svg>
  {/if}
</span>
