<script>
  import { tooltip } from "$lib/actions/tooltip.js";
  import Button from "$lib/components/Button.svelte";
  import { formatAbsoluteDateTime } from "$lib/formatDate";
  import { formatWait } from "$lib/inboxMailbox.js";
  import Time from "$lib/time/Time.svelte";

  /**
   * One machine asking to join the workspace (`anx host enroll`). Approval
   * is deliberate: the reader compares the code on screen with the code the
   * machine printed, then confirms. Deny needs no second step.
   */
  let {
    enrollment,
    now = Date.now(),
    busy = "",
    error = "",
    onapprove = () => {},
    ondeny = () => {},
  } = $props();

  let confirming = $state(false);

  let expiresIn = $derived.by(() => {
    const at = Date.parse(enrollment?.expires_at ?? "");
    if (!Number.isFinite(at)) return "";
    return at <= now ? "expired" : formatWait(at - now);
  });
  let approved = $derived(enrollment?.status === "approved");
  let expired = $derived(expiresIn === "expired");
  let requestedAt = $derived(
    Number.isFinite(Date.parse(enrollment?.created_at ?? ""))
      ? enrollment.created_at
      : "",
  );
  let adapters = $derived(enrollment?.discovered_adapters ?? []);
  let adoptions = $derived(enrollment?.adoption_names ?? []);
</script>

<li
  class="px-4 py-3"
  data-host-enrollment={enrollment.id}
  aria-label={`Enrollment request from ${enrollment.requested_slug}`}
>
  <div class="flex flex-wrap items-start gap-x-6 gap-y-3">
    <div class="min-w-0 flex-1 space-y-1">
      <p class="text-meta font-medium text-fg">
        {enrollment.requested_slug}
        <span class="font-normal text-fg-muted">wants to join as a host</span>
      </p>
      <dl
        class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-0.5 text-micro"
      >
        <dt class="text-fg-subtle">Machine</dt>
        <dd class="truncate text-fg-muted">
          {enrollment.os_user}@{enrollment.hostname}
        </dd>
        <dt class="text-fg-subtle">From</dt>
        <dd class="truncate text-fg-muted" data-enrollment-ip>
          {enrollment.requesting_ip || "Unknown address"}
        </dd>
        <dt class="text-fg-subtle">Agents found</dt>
        <dd class="text-fg-muted">
          {adapters.length ? adapters.join(", ") : "None detected"}
        </dd>
        {#if adoptions.length}
          <dt class="text-fg-subtle">Adopts</dt>
          <dd class="text-fg-muted">
            {adoptions.join(", ")}
            <span class="text-fg-subtle"
              >(existing agents keep their history)</span
            >
          </dd>
        {/if}
        <dt class="text-fg-subtle">Requested</dt>
        <dd class="text-fg-muted">
          {#if requestedAt}<Time value={requestedAt} {now} />{:else}—{/if}
          <span class="text-fg-subtle">·</span>
          <span
            class={expired ? "text-danger-text" : ""}
            title={formatAbsoluteDateTime(enrollment.expires_at)}
            use:tooltip={formatAbsoluteDateTime(enrollment.expires_at)}
            >{expired ? "expired" : `expires in ${expiresIn}`}</span
          >
        </dd>
      </dl>
    </div>

    <div class="shrink-0 text-right">
      <p class="text-micro text-fg-subtle">Code on the machine</p>
      <p
        class="font-mono text-[18px] font-semibold leading-7 tracking-[0.12em] text-fg"
        data-enrollment-code
      >
        {enrollment.user_code || "—"}
      </p>
    </div>
  </div>

  {#if error}
    <p class="mt-2 text-micro text-danger-text" role="alert">{error}</p>
  {/if}

  {#if confirming && !approved}
    <div
      class="mt-3 flex flex-wrap items-center gap-3 rounded-md border border-line bg-bg px-3 py-2"
      data-enrollment-confirm
    >
      <p class="min-w-0 flex-1 text-micro text-fg-muted">
        Approve only if <span class="font-mono font-semibold text-fg"
          >{enrollment.user_code}</span
        >
        is the code printed on {enrollment.hostname}. Every agent that runs
        there as {enrollment.os_user} can then act in this workspace.
      </p>
      <div class="flex shrink-0 gap-2">
        <Button
          variant="primary"
          size="compact"
          busy={busy === "approve"}
          disabled={approved || expired || Boolean(busy)}
          onclick={() => onapprove(enrollment)}
          >{busy === "approve" ? "Approving…" : "Codes match, approve"}</Button
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
        disabled={approved || expired || Boolean(busy)}
        onclick={() => (confirming = true)}
        >{approved ? "Approved, awaiting completion" : "Approve…"}</Button
      >
      <Button
        variant="ghost"
        size="compact"
        busy={busy === "deny"}
        disabled={Boolean(busy)}
        onclick={() => ondeny(enrollment)}
        >{busy === "deny"
          ? "Denying…"
          : approved
            ? "Cancel approval"
            : "Deny"}</Button
      >
    </div>
  {/if}
</li>
