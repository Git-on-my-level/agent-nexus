<script>
  import {
    mutationOutbox,
    mutationConfirmations,
    editMutation,
    retryMutation,
  } from "$lib/mutationOutbox.js";
  import Time from "$lib/time/Time.svelte";
  import { attachmentFile } from "$lib/attachmentWork.js";
  let { target = "", entry: provided = null } = $props();
  let entry = $derived(
    provided ||
      $mutationOutbox.find((row) => row.target === target) ||
      $mutationConfirmations[target],
  );
  let localError = $state("");
  function download() {
    const url = URL.createObjectURL(attachmentFile(entry.request));
    const link = document.createElement("a");
    link.href = url;
    link.download = entry.request.file.name;
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
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
    {#if entry.operation === "inbox.controls"}
      <p class="mt-1 text-fg-muted">
        {entry.receipt?.controls?.length || 0} of {entry.request.items.length} actions
        confirmed.
      </p>
    {/if}
    {#if entry.operation === "events.lifecycle"}
      <p class="mt-1 text-fg-muted">
        {entry.receipt?.events?.length || 0} of {entry.request.items.length} events
        confirmed.
      </p>
    {/if}
    {#if entry.operation === "docs.lifecycle"}
      <p class="mt-1 text-fg-muted">
        {entry.receipt?.documents?.length || 0} of {entry.request.items.length} documents
        confirmed.
      </p>
    {/if}
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
        {#if entry.operation === "artifacts.attachments.create"}
          <button
            type="button"
            class="rounded border border-line px-2 py-1 text-fg"
            onclick={download}>Save file</button
          >
        {/if}
        <button
          type="button"
          class="rounded border border-line px-2 py-1 text-fg"
          onclick={copy}>Copy text</button
        >
        {#if entry.state === "attention"}
          {#if (entry.operation !== "docs.lifecycle" || entry.receipt?.documents?.length !== entry.request.items.length) && (entry.operation !== "events.lifecycle" || entry.receipt?.events?.length !== entry.request.items.length)}
            <button
              type="button"
              class="rounded border border-line px-2 py-1 text-fg"
              onclick={edit}
              >{entry.operation === "artifacts.attachments.create"
                ? "Choose replacement"
                : ["cards.move", "pm.decisions.create"].includes(
                      entry.operation,
                    )
                  ? "Edit move"
                  : entry.operation === "cards.patch"
                    ? "Edit assignment"
                    : [
                          "docs.lifecycle",
                          "events.lifecycle",
                          "inbox.controls",
                        ].includes(entry.operation)
                      ? "Edit request"
                      : entry.operation === "topics.patch"
                        ? "Edit project"
                        : "Edit message"}</button
            >
          {/if}
        {/if}
        {#if entry.state === "attention" || (entry.operation === "inbox.controls" && entry.state === "retrying")}
          <button
            type="button"
            class="rounded border border-line px-2 py-1 text-fg"
            onclick={() =>
              retryMutation(entry.key, { asNew: !!entry.failure?.expired })}
            >{entry.failure?.expired
              ? "Retry as new"
              : entry.state === "retrying"
                ? "Retry confirmation"
                : "Retry send"}</button
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
