<script>
  import { tick } from "svelte";

  import { describeGrantAuthority } from "$lib/accessGrant.js";
  import Button from "$lib/components/Button.svelte";
  import { formatAbsoluteDateTime } from "$lib/formatDate";
  import { formatWait } from "$lib/inboxMailbox.js";

  /**
   * One agent asking for a grant, with the reason it gave.
   *
   * Approving hands an agent real authority, so the confirm step says what
   * that authority is in the same words the Administrators section uses, and
   * names the host whose shared key can request this agent — but only when
   * the roster actually says which host that is.
   */
  let {
    request,
    name = "",
    hostSlug = "",
    now = Date.now(),
    busy = "",
    error = "",
    onapprove = () => {},
    ondeny = () => {},
  } = $props();

  let confirming = $state(false);
  let confirmEl = $state(null);

  let who = $derived(name || request?.username || "An agent");
  // The contract allows one grant today. Naming it from the data keeps a
  // second grant from being announced, and confirmed, as this one.
  let grant = $derived(String(request?.grant ?? "").trim());
  let isAuthAdmin = $derived(grant === "auth-admin");
  let handle = $derived(
    request?.username && request.username !== who ? request.username : "",
  );
  let askedAgo = $derived.by(() => {
    const at = Date.parse(request?.created_at ?? "");
    return Number.isFinite(at) ? formatWait(Math.max(0, now - at)) : "";
  });

  async function startConfirm() {
    confirming = true;
    // The panel carries the authority the reader is about to hand over, and
    // clicking Approve unmounts the focused button. Move focus onto it.
    await tick();
    confirmEl?.focus?.();
  }
</script>

<li
  class="px-4 py-3"
  data-access-request={request.id}
  aria-label={`Access request from ${who}`}
>
  <div class="min-w-0 space-y-1">
    <p class="text-meta font-medium text-fg">
      {who}
      <span class="font-normal text-fg-muted"
        >{isAuthAdmin
          ? "asks to administer access"
          : `asks for the ${grant || "unnamed"} grant`}</span
      >
    </p>
    <dl
      class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-micro"
    >
      <dt class="text-fg-subtle">Reason</dt>
      <dd class="text-fg-muted [overflow-wrap:anywhere]">
        {request.reason || "None given"}
      </dd>
      {#if handle}
        <dt class="text-fg-subtle">Agent</dt>
        <dd class="truncate font-mono text-fg-muted">{handle}</dd>
      {/if}
      {#if hostSlug}
        <dt class="text-fg-subtle">Host</dt>
        <dd class="truncate text-fg-muted">{hostSlug}</dd>
      {/if}
      {#if askedAgo}
        <dt class="text-fg-subtle">Asked</dt>
        <dd
          class="text-fg-muted"
          title={formatAbsoluteDateTime(request.created_at)}
        >
          {askedAgo === "<1m" ? "just now" : `${askedAgo} ago`}
        </dd>
      {/if}
    </dl>
  </div>

  {#if error}
    <p class="mt-2 text-micro text-danger-text" role="alert">{error}</p>
  {/if}

  {#if confirming}
    <div
      class="mt-3 flex flex-wrap items-center gap-3 rounded-md border border-line bg-bg px-3 py-2"
      data-access-request-confirm
      role="group"
      aria-label="Confirm this grant"
      tabindex="-1"
      bind:this={confirmEl}
    >
      <!-- Same words as the Inbox review of the same request. -->
      <p class="min-w-0 flex-1 text-micro text-fg-muted">
        {describeGrantAuthority({ who, grant, hostSlug })}
      </p>
      <div class="flex shrink-0 gap-2">
        <Button
          variant="primary"
          size="compact"
          busy={busy === "approve"}
          disabled={Boolean(busy)}
          onclick={() => onapprove(request)}
          >{busy === "approve" ? "Granting…" : "Grant administration"}</Button
        >
        <Button
          variant="ghost"
          size="compact"
          disabled={Boolean(busy)}
          onclick={() => (confirming = false)}>Cancel</Button
        >
      </div>
    </div>
  {:else}
    <div class="mt-3 flex gap-2">
      <Button
        variant="secondary"
        size="compact"
        disabled={Boolean(busy)}
        onclick={startConfirm}>Approve…</Button
      >
      <Button
        variant="ghost"
        size="compact"
        busy={busy === "deny"}
        disabled={Boolean(busy)}
        onclick={() => ondeny(request)}
        >{busy === "deny" ? "Denying…" : "Deny"}</Button
      >
    </div>
  {/if}
</li>
