<script>
  import { tick } from "svelte";

  import Button from "$lib/components/Button.svelte";

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

  let confirmActionWrapEl = $state(null);
  let typedInputEl = $state(null);
  let typedValue = $state("");
  let confirmVariant = $derived(
    variant === "warning" ? "secondary" : "destructive",
  );
  let confirmClass = $derived(
    variant === "warning" ? "border-warn bg-warn text-white hover:bg-warn" : "",
  );

  let needsTyped = $derived(typedConfirmation.length > 0);
  let typedMatch = $derived(
    needsTyped && typedValue.trim() === typedConfirmation,
  );
  let confirmDisabled = $derived(busy || (needsTyped && !typedMatch));

  $effect(() => {
    if (!open) return;
    typedValue = "";
    void tick().then(() => {
      if (needsTyped) {
        typedInputEl?.focus();
      } else {
        confirmActionWrapEl?.querySelector("button, a[role='button']")?.focus();
      }
    });
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
        <Button
          variant="secondary"
          size="compact"
          disabled={busy}
          onclick={oncancel}
        >
          {cancelLabel}
        </Button>
        <span bind:this={confirmActionWrapEl} class="contents">
          <Button
            variant={confirmVariant}
            size="compact"
            class={confirmClass}
            disabled={confirmDisabled}
            {busy}
            onclick={onconfirm}
          >
            {busy ? "Working…" : confirmLabel}
          </Button>
        </span>
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
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-modal);
    padding: 20px 24px 16px;
  }

  .confirm-title {
    margin: 0;
    font-size: 17px;
    font-weight: 600;
    color: var(--fg);
    line-height: 1.3;
  }

  .confirm-message {
    margin: 8px 0 0;
    font-size: 13px;
    color: var(--fg-muted);
    line-height: 1.5;
  }

  .confirm-typed {
    display: block;
    margin-top: 14px;
  }

  .confirm-typed-label {
    display: block;
    font-size: 12px;
    color: var(--fg-muted);
    margin-bottom: 6px;
  }

  .confirm-typed-phrase {
    font-weight: 600;
    color: var(--fg);
  }

  .confirm-typed-input {
    width: 100%;
    padding: 6px 10px;
    font-size: 13px;
    font-family: var(--font-sans);
    color: var(--fg);
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: var(--radius-md);
    outline: none;
    transition: border-color 80ms ease;
  }

  .confirm-typed-input:focus {
    border-color: var(--accent);
  }

  .confirm-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 20px;
  }
</style>
