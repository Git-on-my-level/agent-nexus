<script>
  import Button from "$lib/components/Button.svelte";

  let { ondismiss, onsubmit } = $props();

  let tagText = $state("");
  let submitting = $state(false);

  async function handleSubmit(e) {
    e.preventDefault();
    const text = tagText.trim();
    if (!text || submitting) return;

    submitting = true;
    try {
      await onsubmit?.(text);
    } finally {
      submitting = false;
    }
  }
</script>

<div
  class="relative rounded-lg border border-[var(--line)] bg-[var(--bg-soft)] px-6 py-8"
  data-testid="inbox-first-run-tour"
>
  <div class="absolute top-3 right-3">
    <Button variant="ghost" size="compact" onclick={ondismiss}>
      Skip tour
    </Button>
  </div>

  <h2 class="text-subtitle font-semibold text-[var(--fg)]">
    Your inbox is where the agent asks you questions.
  </h2>

  <div
    class="mt-5 grid grid-cols-1 gap-3 sm:grid-cols-3"
    data-testid="inbox-tour-cards"
  >
    <div
      class="rounded-md border border-[var(--line)] bg-[var(--panel)] px-4 py-3"
      data-testid="inbox-tour-card-ask"
    >
      <span
        class="inline-flex items-center rounded px-1.5 py-0.5 text-micro font-semibold tracking-wide bg-accent-soft text-accent-text"
      >
        ASK
      </span>
      <p class="mt-2 text-micro leading-relaxed text-[var(--fg-subtle)]">
        The agent is blocked and needs a call from you. You answer. It unblocks.
      </p>
    </div>

    <div
      class="rounded-md border border-[var(--line)] bg-[var(--panel)] px-4 py-3"
      data-testid="inbox-tour-card-tag"
    >
      <span
        class="inline-flex items-center rounded px-1.5 py-0.5 text-micro font-semibold tracking-wide bg-[var(--line)] text-[var(--fg-muted)]"
      >
        TAG
      </span>
      <p class="mt-2 text-micro leading-relaxed text-[var(--fg-subtle)]">
        You want the agent to notice something, but it's not urgent. You drop a
        tag; it picks it up when it's ready.
      </p>
    </div>

    <div
      class="rounded-md border border-[var(--line)] bg-[var(--panel)] px-4 py-3"
      data-testid="inbox-tour-card-wake"
    >
      <span
        class="inline-flex items-center rounded px-1.5 py-0.5 text-micro font-semibold tracking-wide bg-[var(--line)] text-[var(--fg-muted)]"
      >
        WAKE
      </span>
      <p class="mt-2 text-micro leading-relaxed text-[var(--fg-subtle)]">
        A scheduled check-in. The agent wakes up on a cadence to re-evaluate a
        topic.
      </p>
    </div>
  </div>

  <p class="mt-5 text-body text-[var(--fg)]">
    To see how it works, drop your first tag now:
  </p>

  <form
    class="mt-3 flex items-center gap-2"
    onsubmit={handleSubmit}
    data-testid="inbox-tour-tag-form"
  >
    <input
      type="text"
      class="flex-1 rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-1.5 text-meta transition-colors focus:bg-[var(--panel)]"
      placeholder="e.g., check the supply forecast for Q3"
      bind:value={tagText}
      disabled={submitting}
      data-testid="inbox-tour-tag-input"
    />
    <Button
      type="submit"
      variant="primary"
      size="compact"
      disabled={!tagText.trim() || submitting}
      busy={submitting}
    >
      Drop tag
    </Button>
  </form>
</div>
