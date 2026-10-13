<script>
  import {
    mutationOutbox,
    mutationConfirmations,
    editMutation,
    retryMutation,
  } from "$lib/mutationOutbox.js";
  import Time from "$lib/time/Time.svelte";
  let { target = "", entry: provided = null } = $props();
  let entry = $derived(
    provided ||
      $mutationOutbox.find((row) => row.target === target) ||
      $mutationConfirmations[target],
  );
  let localError = $state("");
  async function copy() {
    try {
      await navigator.clipboard.writeText(entry.text);
      localError = "";
    } catch {
      localError = "Select and copy the saved text above.";
    }
  }
  async function edit() {
    try {
      await editMutation(entry.key);
      localError = "";
    } catch {
      localError = "Your text is still here. Copy it before closing this tab.";
    }
  }
</script>

{#if entry}
  <div
    class="mb-2 rounded-md border border-line bg-panel p-3 text-meta"
    data-mutation-state={entry.state}
    role="status"
  >
    <p class="font-medium text-fg">
      {entry.state === "sent"
        ? "Sent"
        : entry.state === "attention"
          ? "Needs your attention"
          : entry.state === "retrying"
            ? "Retrying"
            : "Pending"}
    </p>
    {#if entry.state !== "sent"}
      <p class="mt-1 text-fg-muted">
        {entry.failure?.message ||
          "Saved on this device. Waiting for confirmation."}
      </p>
      {#if entry.state === "retrying"}<p class="mt-1 text-fg-muted">
          Next attempt: <Time value={entry.nextAt} style="clock" />
        </p>{/if}
      <textarea
        class="mt-2 w-full rounded border border-line bg-bg-soft p-2 text-fg"
        aria-label="Saved message"
        readonly
        value={entry.text}
      ></textarea>
      <div class="mt-2 flex gap-2">
        <button
          type="button"
          class="rounded border border-line px-2 py-1 text-fg"
          onclick={copy}>Copy text</button
        >
        {#if entry.state === "attention"}
          <button
            type="button"
            class="rounded border border-line px-2 py-1 text-fg"
            onclick={edit}>Edit message</button
          >
          <button
            type="button"
            class="rounded border border-line px-2 py-1 text-fg"
            onclick={() => retryMutation(entry.key)}>Retry send</button
          >
        {/if}
      </div>
      {#if localError}<p class="mt-1 text-danger-text">{localError}</p>{/if}
      {#if entry.failure?.detail}<details class="mt-2 text-micro text-fg-muted">
          <summary>Technical details</summary>
          <pre class="mt-2 whitespace-pre-wrap break-words">{entry.failure
              .detail}</pre>
        </details>{/if}
    {/if}
  </div>
{/if}
