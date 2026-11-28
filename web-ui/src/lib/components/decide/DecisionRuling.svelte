<script>
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  import {
    mutationConfirmations,
    mutationOutbox,
  } from "$lib/mutationOutbox.js";
  import {
    confirmRulingTarget,
    enqueueRulingConfirm,
    enqueueRulingRevoke,
    revokeTarget,
  } from "$lib/standingRulings.js";

  let { ruling, href, crossWorkspace = false } = $props();
  let sending = $state(false);
  let saveError = $state("");
  let confirmTarget = $derived(confirmRulingTarget(ruling.ruling_id));
  let dismissTarget = $derived(revokeTarget(ruling.ruling_id));
  let target = $derived(
    $mutationOutbox.find((row) =>
      [confirmTarget, dismissTarget].includes(row.target),
    )?.target ||
      [confirmTarget, dismissTarget].find((key) => $mutationConfirmations[key]),
  );

  async function submit(confirm) {
    if (sending || target) return;
    sending = true;
    saveError = "";
    try {
      const options = {
        id: ruling.ruling_id,
        question: ruling.question,
        href,
      };
      await (confirm
        ? enqueueRulingConfirm(options)
        : enqueueRulingRevoke(options));
    } catch {
      saveError = "Couldn’t save this choice on this device. It was not sent.";
    } finally {
      sending = false;
    }
  }
</script>

<div class="space-y-2 text-meta">
  <p class="text-fg-muted">
    <span class="font-mono">{ruling.ruling_id}</span>
    · {ruling.scope === "account" ? "Account" : "Workspace"} · {ruling.topic}
  </p>
  {#if ruling.source_quote}
    <blockquote
      class="whitespace-pre-wrap border-l border-line pl-3 [overflow-wrap:anywhere]"
    >
      {ruling.source_quote}
    </blockquote>
  {/if}
  {#if ruling.source}
    <p class="break-all text-fg-muted">Source {ruling.source}</p>
  {/if}
  {#if ruling.status_hint}
    <p class="text-fg-muted">
      Imported status {ruling.status_hint}
      {#if ruling.superseded_by_hint}· superseded by {ruling.superseded_by_hint}{/if}
    </p>
  {/if}
  {#if ruling.relations?.length}
    <ul class="text-fg-muted">
      {#each ruling.relations as relation (relation.ruling_id)}
        <li class="capitalize">
          {relation.kind} <span class="font-mono">{relation.ruling_id}</span>
        </li>
      {/each}
    </ul>
  {/if}
  {#if target}
    <MutationSendStatus {target} />
  {:else if crossWorkspace}
    <a class="ui-btn-secondary" {href}>Open in Decide</a>
  {:else}
    <div class="flex flex-wrap gap-2">
      <button
        type="button"
        class="ui-btn-primary max-lg:min-h-11"
        disabled={sending}
        onclick={() => submit(true)}
      >
        Confirm ruling
      </button>
      <button
        type="button"
        class="ui-btn-secondary max-lg:min-h-11"
        disabled={sending}
        onclick={() => submit(false)}
      >
        Dismiss proposal
      </button>
    </div>
  {/if}
  {#if saveError}
    <p role="alert">{saveError}</p>
  {/if}
</div>
