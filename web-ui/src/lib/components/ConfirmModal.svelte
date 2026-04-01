<script>
  let {
    open = false,
    title = "Confirm",
    message = "",
    confirmLabel = "Confirm",
    cancelLabel = "Cancel",
    variant = "danger",
    busy = false,
    typedConfirmation = "",
    onconfirm = () => {},
    oncancel = () => {},
  } = $props();

  let confirmBtnEl = $state(null);
  let typedInputEl = $state(null);
  let typedValue = $state("");

  let needsTyped = $derived(typedConfirmation.length > 0);
  let typedMatch = $derived(
    needsTyped && typedValue.trim() === typedConfirmation,
  );
  let confirmDisabled = $derived(busy || (needsTyped && !typedMatch));

  $effect(() => {
    if (!open) return;
    typedValue = "";
    if (needsTyped) {
      setTimeout(() => typedInputEl?.focus(), 0);
    } else {
      confirmBtnEl?.focus();
    }
    function onKeydown(e) {
      if (e.key === "Escape" && !busy) {
        e.preventDefault();
        e.stopPropagation();
        oncancel();
      }
    }
    document.addEventListener("keydown", onKeydown, true);
    return () => document.removeEventListener("keydown", onKeydown, true);
  });

  function handleBackdropClick(e) {
    if (e.target === e.currentTarget && !busy) {
      oncancel();
    }
  }

  function handleTypedKeydown(e) {
    if (e.key === "Enter" && typedMatch && !busy) {
      e.preventDefault();
      onconfirm();
    }
  }
</script>

{#if open}
  <div
    class="confirm-backdrop"
    role="dialog"
    aria-modal="true"
    aria-label={title}
  >
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="confirm-overlay" onclick={handleBackdropClick}></div>
    <div class="confirm-panel">
      <h2 class="confirm-title">{title}</h2>
      {#if message}
        <p class="confirm-message">{message}</p>
      {/if}
      {#if needsTyped}
        <label class="confirm-typed">
          <span class="confirm-typed-label">
            Type <span class="confirm-typed-phrase">{typedConfirmation}</span> to
            confirm
          </span>
          <input
            bind:this={typedInputEl}
            bind:value={typedValue}
            class="confirm-typed-input"
            autocomplete="off"
            spellcheck="false"
            onkeydown={handleTypedKeydown}
          />
        </label>
      {/if}
      <div class="confirm-actions">
        <button
          class="confirm-btn confirm-btn--cancel"
          disabled={busy}
          onclick={oncancel}
          type="button"
        >
          {cancelLabel}
        </button>
        <button
          class="confirm-btn {variant === 'danger'
            ? 'confirm-btn--danger'
            : 'confirm-btn--warning'}"
          bind:this={confirmBtnEl}
          disabled={confirmDisabled}
          onclick={onconfirm}
          type="button"
        >
          {busy ? "Working…" : confirmLabel}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .confirm-backdrop {
    position: fixed;
    inset: 0;
    z-index: 9999;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .confirm-overlay {
    position: absolute;
    inset: 0;
    background: rgba(0, 0, 0, 0.6);
    backdrop-filter: blur(2px);
  }

  .confirm-panel {
    position: relative;
    width: 380px;
    max-width: calc(100vw - 2rem);
    background: var(--ui-panel);
    border: 1px solid var(--ui-border);
    border-radius: var(--ui-radius-lg);
    box-shadow: var(--ui-shadow-elevated);
    padding: 20px 24px 16px;
  }

  .confirm-title {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
    color: var(--ui-text);
    line-height: 1.3;
  }

  .confirm-message {
    margin: 8px 0 0;
    font-size: 13px;
    color: var(--ui-text-muted);
    line-height: 1.5;
  }

  .confirm-typed {
    display: block;
    margin-top: 14px;
  }

  .confirm-typed-label {
    display: block;
    font-size: 12px;
    color: var(--ui-text-muted);
    margin-bottom: 6px;
  }

  .confirm-typed-phrase {
    font-weight: 600;
    color: var(--ui-text);
  }

  .confirm-typed-input {
    width: 100%;
    padding: 6px 10px;
    font-size: 13px;
    font-family: var(--ui-font-sans);
    color: var(--ui-text);
    background: var(--ui-bg);
    border: 1px solid var(--ui-border);
    border-radius: var(--ui-radius-md);
    outline: none;
    transition: border-color 80ms ease;
  }

  .confirm-typed-input:focus {
    border-color: var(--ui-accent);
  }

  .confirm-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 20px;
  }

  .confirm-btn {
    padding: 6px 14px;
    font-size: 12px;
    font-weight: 500;
    font-family: var(--ui-font-sans);
    border-radius: var(--ui-radius-md);
    cursor: pointer;
    border: none;
    transition: background 80ms ease;
  }

  .confirm-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .confirm-btn--cancel {
    background: var(--ui-bg-soft);
    color: var(--ui-text-muted);
    border: 1px solid var(--ui-border);
  }

  .confirm-btn--cancel:hover:not(:disabled) {
    background: var(--ui-border);
  }

  .confirm-btn--danger {
    background: #dc2626;
    color: #fff;
  }

  .confirm-btn--danger:hover:not(:disabled) {
    background: #ef4444;
  }

  .confirm-btn--warning {
    background: #d97706;
    color: #fff;
  }

  .confirm-btn--warning:hover:not(:disabled) {
    background: #f59e0b;
  }
</style>
