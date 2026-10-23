<script>
  /**
   * The "⋯" on anything ANX placed in front of the reader: one quiet control
   * that tells the reader's agents whether it should have reached them, and
   * where. Choices are the `attention_feedback` signals core stores; an
   * optional note says why. ANX only records the choice — the agents that
   * place items read it and learn.
   *
   * `surface` picks the choices that make sense there: in Decide the question
   * is "did this need me", on Watch it is "do I want more or less of this".
   */
  import { dismissOnEscape } from "$lib/actions/dismissOnEscape.js";
  import { FEEDBACK_CHOICES, SIGNAL_LABELS } from "$lib/attention.js";

  let {
    /** `decide`, `watch`, or `all` (Show everything). */
    surface = "watch",
    /** Accessible name of the item, for the trigger's label. */
    title = "",
    /**
     * Called with `{ signal, note }`. Return a promise; a rejection keeps the
     * menu open with the note so nothing the reader typed is lost.
     */
    onsubmit = null,
    /** Signal already sent for this item, shown instead of the trigger. */
    sent = "",
    onundo = null,
  } = $props();

  let open = $state(false);
  let note = $state("");
  let busy = $state(false);
  let error = $state("");
  let trigger = $state();
  let choices = $derived(FEEDBACK_CHOICES[surface] ?? FEEDBACK_CHOICES.watch);

  function close() {
    open = false;
    error = "";
    trigger?.focus();
  }
  async function choose(signal) {
    if (busy) return;
    busy = true;
    error = "";
    try {
      await onsubmit?.({ signal, note: note.trim() });
      note = "";
      open = false;
    } catch {
      error = "Couldn't send that. Your note is kept; try again.";
    } finally {
      busy = false;
    }
  }
</script>

{#if sent}
  <span class="sent" role="status" data-feedback-sent={sent}>
    “{SIGNAL_LABELS[sent] ?? sent}” sent{#if onundo}
      <button type="button" class="undo" onclick={() => onundo()}>Undo</button
      >{/if}
  </span>
{:else}
  <span class="feedback">
    <button
      type="button"
      class="trigger"
      bind:this={trigger}
      aria-haspopup="menu"
      aria-expanded={open}
      aria-label={title ? `Feedback on ${title}` : "Feedback"}
      onclick={() => (open ? close() : (open = true))}
      data-feedback-trigger>⋯</button
    >
    {#if open}
      <div
        class="menu ui-pop-in"
        role="menu"
        aria-label="Feedback"
        use:dismissOnEscape={{ onDismiss: close }}
        data-feedback-menu
      >
        {#each choices as [signal, label, hint] (signal)}
          <button
            type="button"
            role="menuitem"
            class="item"
            disabled={busy}
            onclick={() => void choose(signal)}
            data-feedback-signal={signal}
          >
            <span class="block">{label}</span>
            {#if hint}<span class="hint">{hint}</span>{/if}
          </button>
        {/each}
        <label class="note">
          <span class="sr-only">Tell the agent why (optional)</span>
          <input
            bind:value={note}
            maxlength="500"
            placeholder="Tell the agent why (optional)"
            data-anx-no-submit-shortcut
          />
        </label>
        {#if error}<p class="error" role="alert">{error}</p>{/if}
      </div>
    {/if}
  </span>
{/if}

<style>
  .feedback {
    position: relative;
    display: inline-flex;
  }
  .trigger {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 2.25rem;
    height: 2rem;
    border-radius: 6px;
    color: var(--fg-muted);
    font-size: 16px;
    line-height: 1;
  }
  .trigger:hover,
  .trigger[aria-expanded="true"] {
    color: var(--fg);
    background: var(--panel-hover);
  }
  .trigger:focus-visible,
  .item:focus-visible {
    outline: 2px solid var(--accent-solid);
    outline-offset: 1px;
  }
  .menu {
    position: absolute;
    right: 0;
    top: calc(100% + 4px);
    z-index: 30;
    width: min(20rem, calc(100vw - 2rem));
    padding: 6px;
    border: 1px solid var(--line-strong);
    border-radius: 10px;
    background: var(--panel);
    box-shadow: 0 12px 30px rgba(0, 0, 0, 0.35);
  }
  .item {
    display: block;
    width: 100%;
    min-height: 2.5rem;
    padding: 8px 10px;
    border-radius: 6px;
    text-align: left;
    color: var(--fg);
    font-size: 13px;
  }
  .item:hover {
    background: var(--panel-hover);
  }
  .hint {
    display: block;
    margin-top: 1px;
    color: var(--fg-muted);
    font-size: 11px;
  }
  .note {
    display: block;
    margin-top: 4px;
    padding: 8px 4px 2px;
    border-top: 1px solid var(--line);
  }
  .note input {
    width: 100%;
    padding: 7px 9px;
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--bg-soft);
    color: var(--fg);
    font-size: 12px;
  }
  .error {
    margin: 6px 4px 2px;
    color: var(--danger-text);
    font-size: 12px;
  }
  .sent {
    color: var(--accent-text);
    font-size: 12px;
  }
  .undo {
    margin-left: 6px;
    color: var(--accent-text);
    text-decoration: underline;
  }
</style>
