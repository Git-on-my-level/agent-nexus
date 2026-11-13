<script>
  /**
   * Standing rulings: the answers the reader made durable, and the kinds of
   * ask they sent back for good. Collapsed until opened; the list is read
   * only then. Revoking goes through the durable outbox.
   */
  import { coreClient } from "$lib/coreClient";
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  import {
    mutationConfirmations,
    mutationOutbox,
  } from "$lib/mutationOutbox.js";
  import {
    STANDING_OPERATIONS,
    enqueueRulingRevoke,
    revokeTarget,
  } from "$lib/standingRulings.js";
  import { formatUtcDay } from "$lib/time/format.js";

  let { href = "" } = $props();

  let open = $state(false);
  let state = $state("idle"); // idle | loading | ready | failed
  let rulings = $state([]);
  let saveError = $state("");

  async function load() {
    state = "loading";
    try {
      const result = await coreClient.rulingsList();
      rulings = Array.isArray(result?.rulings) ? result.rulings : [];
      state = "ready";
    } catch {
      state = "failed";
    }
  }

  function toggle() {
    open = !open;
    if (open && state !== "ready") void load();
  }

  /** A ruling whose revoke is waiting, sending or confirmed is not listed. */
  let revoking = $derived(
    new Set([
      ...($mutationOutbox ?? [])
        .filter((entry) => entry.state !== "attention")
        .map((entry) => entry.target),
      ...Object.keys($mutationConfirmations ?? {}),
    ]),
  );
  let shown = $derived(
    rulings.filter(
      (ruling) =>
        ruling.active !== false && !revoking.has(revokeTarget(ruling.id)),
    ),
  );
  /** Rulings and send-backs that did not go through and need the reader. */
  let stuck = $derived(
    ($mutationOutbox ?? []).filter(
      (entry) =>
        STANDING_OPERATIONS.includes(entry.operation) &&
        entry.state === "attention",
    ),
  );

  async function revoke(ruling) {
    saveError = "";
    try {
      await enqueueRulingRevoke({
        id: ruling.id,
        question: ruling.question,
        href,
      });
    } catch {
      saveError =
        "Couldn’t save this on this device, so the ruling was not revoked.";
    }
  }

  const uses = (n) => (n === 1 ? "used 1 time" : `used ${n || 0} times`);
</script>

<section class="mt-4" data-standing-rulings>
  {#each stuck as entry (entry.key)}
    <MutationSendStatus {entry} />
  {/each}
  <h2 class="m-0 text-meta">
    <button
      type="button"
      class="flex w-full items-center gap-2 rounded-md border border-line-subtle bg-panel px-3 py-2 text-left"
      aria-expanded={open}
      data-standing-rulings-toggle
      onclick={toggle}
    >
      <span class="font-semibold text-fg"
        >Standing rulings{state === "ready" ? ` · ${shown.length}` : ""}</span
      >
      <span class="ml-auto text-fg-muted">{open ? "Hide" : "Show"}</span>
    </button>
  </h2>
  {#if open}
    <div class="mt-2 space-y-2">
      {#if state === "loading"}
        <p class="text-meta text-fg-muted">Loading rulings…</p>
      {:else if state === "failed"}
        <p class="text-meta text-danger-text" role="alert">
          Couldn’t load standing rulings.
          <button type="button" class="ui-prose-link" onclick={load}
            >Retry</button
          >
        </p>
      {:else if !shown.length}
        <p class="text-meta text-fg-muted" data-standing-rulings-empty>
          None yet. An answer becomes a ruling when you tick “Always answer this
          way” before answering.
        </p>
      {:else}
        <ul class="m-0 list-none space-y-2 p-0">
          {#each shown as ruling (ruling.id)}
            <li
              class="flex flex-wrap items-start gap-x-3 gap-y-1 rounded-md border border-line-subtle bg-panel px-3 py-2"
              data-standing-ruling={ruling.id}
            >
              <div class="min-w-0 flex-1">
                <p
                  class="m-0 text-meta font-medium text-fg [overflow-wrap:anywhere]"
                >
                  {ruling.question}
                </p>
                <p
                  class="m-0 mt-0.5 text-meta text-fg [overflow-wrap:anywhere]"
                >
                  {#if ruling.kind === "not_a_decision"}
                    {ruling.requester
                      ? `Not a decision · from ${ruling.requester}`
                      : "Not a decision"}
                  {:else}
                    → {ruling.answer}
                  {/if}
                </p>
                <p class="m-0 mt-0.5 text-micro text-fg-muted">
                  {uses(ruling.uses)} · made {formatUtcDay(ruling.created_at)}
                </p>
              </div>
              <button
                type="button"
                class="ui-btn-secondary"
                data-standing-ruling-revoke
                onclick={() => revoke(ruling)}>Revoke</button
              >
            </li>
          {/each}
        </ul>
      {/if}
      {#if saveError}
        <p class="text-meta text-danger-text" role="alert">{saveError}</p>
      {/if}
    </div>
  {/if}
</section>
