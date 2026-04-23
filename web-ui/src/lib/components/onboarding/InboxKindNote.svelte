<script>
  import { browser } from "$app/environment";

  import { isInboxKindSeen, markInboxKindSeen } from "$lib/tourState";

  /** Inbox `kind` value (e.g. ask) */
  let { kind = "unknown" } = $props();

  let k = $derived(String(kind ?? "unknown").trim() || "unknown");
  let dismissed = $state(true);

  $effect(() => {
    if (!browser) {
      return;
    }
    dismissed = isInboxKindSeen(k);
  });

  const messages = /** @type {Record<string, string>} */ ({
    ask: "Ask means an agent (or person) is blocked and is waiting for your input on this item.",
  });

  let body = $derived(messages[k] ?? "");
  let visible = $derived(Boolean(body) && !dismissed);
</script>

{#if visible}
  <div
    class="mb-2 flex items-start justify-between gap-2 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2 text-meta text-[var(--fg-subtle)]"
    data-testid="inbox-kind-note-{k}"
  >
    <p class="min-w-0 flex-1 leading-snug">
      {body}
    </p>
    <button
      type="button"
      class="shrink-0 cursor-pointer text-[var(--fg-muted)] hover:text-[var(--fg)]"
      aria-label="Dismiss this tip"
      onclick={() => {
        markInboxKindSeen(k);
        dismissed = true;
      }}
    >
      <svg
        class="h-3.5 w-3.5"
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
{/if}
