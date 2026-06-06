<script>
  import Button from "$lib/components/Button.svelte";

  let {
    open = false,
    cardTitle = "",
    busy = false,
    onconfirm = async () => {},
    oncancel = () => {},
  } = $props();

  let note = $state("");

  $effect(() => {
    if (open) note = "";
  });

  function handleBackdropClick(e) {
    if (e.target === e.currentTarget && !busy) oncancel();
  }

  async function handleConfirm() {
    await onconfirm(note.trim());
  }
</script>

{#if open}
  <div
    class="mark-done-backdrop"
    role="dialog"
    aria-modal="true"
    aria-label="Mark card as done"
  >
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="mark-done-overlay" onclick={handleBackdropClick}></div>
    <div class="mark-done-panel">
      <h2 class="text-subtitle font-semibold text-fg">Mark as done</h2>
      <p class="mt-2 text-meta text-fg-muted">
        {#if cardTitle}
          <span class="font-medium text-fg">{cardTitle}</span>
          will move to Done. We'll record a short attestation as evidence.
        {:else}
          This card will move to Done. We'll record a short attestation as
          evidence.
        {/if}
      </p>
      <label class="mt-4 block text-micro">
        <span class="font-medium text-fg-muted">Note (optional)</span>
        <textarea
          bind:value={note}
          class="mt-1 w-full rounded-md border border-line bg-bg-soft px-2.5 py-2 text-meta text-fg focus:outline-none focus:ring-1 focus:ring-accent"
          placeholder="What was completed?"
          rows="3"
          disabled={busy}
        ></textarea>
      </label>
      <div class="mt-4 flex justify-end gap-2">
        <Button
          variant="secondary"
          size="compact"
          disabled={busy}
          onclick={oncancel}
        >
          Cancel
        </Button>
        <Button
          variant="primary"
          size="compact"
          disabled={busy}
          onclick={() => void handleConfirm()}
        >
          {busy ? "Saving…" : "Mark done"}
        </Button>
      </div>
    </div>
  </div>
{/if}

<style>
  .mark-done-backdrop {
    position: fixed;
    inset: 0;
    z-index: 60;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 1rem;
  }
  .mark-done-overlay {
    position: absolute;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
  }
  .mark-done-panel {
    position: relative;
    z-index: 1;
    width: 100%;
    max-width: 24rem;
    border-radius: 0.5rem;
    border: 1px solid var(--line);
    background: var(--panel);
    padding: 1.25rem;
    box-shadow: var(--shadow-modal);
  }
</style>
