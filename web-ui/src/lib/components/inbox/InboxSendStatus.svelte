<script>
  import Time from "$lib/time/Time.svelte";
  import {
    inboxUnsentResponses,
    inboxResponseFailures,
    inboxResponseOverlay,
    retryInboxResponse,
    editInboxResponse,
    discardInboxResponse,
  } from "$lib/inboxResponseQueue.js";
  let { itemId } = $props();
  let record = $derived(
    $inboxUnsentResponses.find((entry) => entry.itemId === itemId),
  );
  let failure = $derived($inboxResponseFailures[itemId] || record?.failure);
  let confirmed = $derived(
    $inboxResponseOverlay[itemId]?.status === "committed",
  );
  let state = $derived(
    confirmed ? "sent" : failure?.state || record?.state || "",
  );
  let text = $derived(
    record?.request.response_text || failure?.response_text || "",
  );
  // Core no longer has this item (404): no retry or edit can succeed.
  let gone = $derived(failure?.status === 404);
  let copied = $state(false);
  let copyError = $state("");
  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      copied = true;
      copyError = "";
    } catch {
      copyError = "Select and copy the saved text below.";
    }
  }
</script>

{#if record || failure || confirmed}
  <div
    class="space-y-2 rounded-md border border-line bg-bg-soft px-3 py-2 text-meta"
    role="status"
    data-inbox-send-state={state}
    data-inbox-response-error={failure ? "" : undefined}
  >
    <p class="font-medium text-fg">
      {state === "sent"
        ? "Sent"
        : state === "attention"
          ? "Needs your attention"
          : state === "retrying"
            ? "Retrying"
            : state === "sending"
              ? "Sending…"
              : "Pending"}
    </p>
    {#if state !== "sent"}<p class="text-fg-muted">
        {failure?.error ||
          "Your response is kept on this device until the service confirms it."}
      </p>{/if}
    {#if failure?.nextAt}
      <p class="text-micro text-fg-subtle">
        Next attempt: <Time value={failure.nextAt} style="clock" />.
      </p>
    {/if}
    {#if failure || state === "attention"}
      <textarea
        class="ui-input min-h-20"
        aria-label="Saved response"
        value={text}
        readonly
      ></textarea>
      <div class="flex flex-wrap gap-3">
        <button type="button" class="ui-btn-secondary" onclick={copy}
          >{copied ? "Copied" : "Copy text"}</button
        >
        {#if gone}
          <button
            type="button"
            class="ui-btn-secondary"
            data-inbox-discard
            onclick={() => discardInboxResponse(itemId)}>Discard</button
          >
        {:else}
          <button
            type="button"
            class="ui-btn-secondary"
            onclick={() => editInboxResponse(itemId)}>Edit response</button
          >
          <button
            type="button"
            class="ui-btn-secondary"
            onclick={() =>
              void retryInboxResponse(itemId, { asNew: !!failure?.expired })}
            >{failure?.expired ? "Retry as new" : "Retry send"}</button
          >
        {/if}
      </div>
      {#if copyError}<p class="text-micro text-fg-muted">{copyError}</p>{/if}
      {#if failure?.detail}
        <details class="text-micro text-fg-subtle">
          <summary>Technical details</summary>
          <pre
            class="whitespace-pre-wrap [overflow-wrap:anywhere]">{failure.detail}</pre>
        </details>
      {/if}
    {/if}
  </div>
{/if}
