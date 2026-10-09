<script>
  import { tooltip } from "$lib/actions/tooltip.js";
  import Button from "$lib/components/Button.svelte";
  import CopyButton from "$lib/components/CopyButton.svelte";
  import { formatAbsoluteDateTime } from "$lib/formatDate";
  import { formatWait } from "$lib/inboxMailbox.js";
  import Time from "$lib/time/Time.svelte";

  /**
   * Headless host enrollment for CI and cloud machines that cannot wait for
   * an approval: a one-time token the machine passes to
   * `anx host enroll --token`. The secret is shown once.
   */
  let {
    tokens = [],
    cliBaseUrl = "",
    now = Date.now(),
    oncreate = async () => null,
    onrevoke = async () => {},
  } = $props();

  const LIFETIMES = [
    { label: "10 minutes", minutes: 10 },
    { label: "1 hour", minutes: 60 },
    { label: "8 hours", minutes: 480 },
    { label: "24 hours", minutes: 1440 },
  ];

  let label = $state("");
  let lifetime = $state(60);
  let creating = $state(false);
  let createError = $state("");
  let created = $state(null);
  let revokingId = $state("");
  let revokeError = $state("");
  let showUsed = $state(false);

  let command = $derived(
    created
      ? `anx ${cliBaseUrl ? `--base-url ${cliBaseUrl} ` : ""}host enroll --token ${created.token}`
      : "",
  );

  function status(token) {
    if (token.revoked_at) return { key: "revoked", text: "revoked" };
    if (token.consumed_at) return { key: "used", text: "used" };
    const expires = Date.parse(token.expires_at);
    if (Number.isFinite(expires) && expires <= now)
      return { key: "expired", text: "expired" };
    return {
      key: "active",
      text: Number.isFinite(expires)
        ? `unused · expires in ${formatWait(expires - now)}`
        : "unused",
    };
  }

  let rows = $derived(
    tokens.map((token) => ({ token, status: status(token) })),
  );
  let activeRows = $derived(rows.filter((row) => row.status.key === "active"));
  let otherRows = $derived(rows.filter((row) => row.status.key !== "active"));

  async function create(event) {
    event.preventDefault();
    const name = label.trim();
    if (!name) return;
    creating = true;
    createError = "";
    try {
      const expiresAt = new Date(Date.now() + lifetime * 60_000).toISOString();
      created = await oncreate({ label: name, expires_at: expiresAt });
      label = "";
    } catch (error) {
      createError =
        error?.details ||
        (error instanceof Error ? error.message : "") ||
        "The token was not created.";
    } finally {
      creating = false;
    }
  }

  async function revoke(token) {
    revokingId = token.id;
    revokeError = "";
    try {
      await onrevoke(token);
    } catch (error) {
      revokeError =
        error?.details ||
        (error instanceof Error ? error.message : "") ||
        "The token was not revoked.";
    } finally {
      revokingId = "";
    }
  }
</script>

<div class="space-y-3" data-host-tokens>
  <form
    class="flex flex-wrap items-end gap-2"
    onsubmit={create}
    data-anx-save-scope
  >
    <label class="min-w-[12rem] flex-1">
      <span class="mb-1 block text-micro text-fg-muted">Label</span>
      <input
        bind:value={label}
        class="w-full rounded-md border border-line bg-bg px-2 py-1.5 text-meta text-fg"
        placeholder="e.g. GitHub Actions runner"
        maxlength="120"
        autocomplete="off"
      />
    </label>
    <label>
      <span class="mb-1 block text-micro text-fg-muted">Valid for</span>
      <select
        bind:value={lifetime}
        class="rounded-md border border-line bg-bg px-2 py-1.5 text-meta text-fg"
      >
        {#each LIFETIMES as option (option.minutes)}
          <option value={option.minutes}>{option.label}</option>
        {/each}
      </select>
    </label>
    <Button
      type="submit"
      variant="secondary"
      busy={creating}
      disabled={!label.trim()}
      saveShortcut>{creating ? "Creating…" : "Create token"}</Button
    >
  </form>
  {#if createError}
    <p class="text-micro text-danger-text" role="alert">{createError}</p>
  {/if}

  {#if created}
    <div
      class="space-y-2 rounded-md border border-ok bg-ok-soft px-3 py-2.5"
      role="status"
      data-host-token-created
    >
      <div class="flex items-start justify-between gap-3">
        <p class="text-micro text-ok-text">
          Token for “{created.enrollment_token?.label}” created. It is shown
          once; run this on the machine before it expires.
        </p>
        <button
          class="shrink-0 text-micro text-fg-muted hover:text-fg"
          type="button"
          onclick={() => (created = null)}>Dismiss</button
        >
      </div>
      <div class="flex items-center gap-1 rounded bg-bg px-2 py-1.5">
        <code
          class="min-w-0 flex-1 break-all font-mono text-micro text-fg"
          data-host-token-command>{command}</code
        >
        <CopyButton value={command} label="Copy command" />
      </div>
      <div class="flex items-center gap-1 text-micro text-fg-muted">
        Token only:
        <CopyButton value={created.token} label="Copy token" />
      </div>
    </div>
  {/if}

  {#if rows.length}
    <ul class="overflow-hidden rounded-md border border-line-subtle text-micro">
      {#each showUsed ? [...activeRows, ...otherRows] : activeRows as row (row.token.id)}
        <li
          class="flex items-center gap-3 border-t border-line-subtle px-3 py-1.5 first:border-t-0"
          data-host-token={row.token.id}
        >
          <span class="min-w-0 flex-1 truncate text-fg">{row.token.label}</span>
          <span
            class="shrink-0 {row.status.key === 'active'
              ? 'text-fg-muted'
              : 'text-fg-subtle'}"
            title={`Created ${formatAbsoluteDateTime(row.token.created_at)}`}
            use:tooltip={`Created ${formatAbsoluteDateTime(row.token.created_at)}`}
            >{row.status.text}{#if row.status.key === "used"}
              {" "}<Time value={row.token.consumed_at} {now} />
            {/if}</span
          >
          {#if row.status.key === "active"}
            <button
              class="shrink-0 text-danger-text hover:underline disabled:opacity-50"
              type="button"
              disabled={revokingId === row.token.id}
              onclick={() => revoke(row.token)}
              >{revokingId === row.token.id ? "Revoking…" : "Revoke"}</button
            >
          {/if}
        </li>
      {:else}
        <li class="px-3 py-1.5 text-fg-subtle">No unused tokens.</li>
      {/each}
    </ul>
    {#if otherRows.length}
      <button
        class="text-micro text-fg-muted hover:text-fg"
        type="button"
        onclick={() => (showUsed = !showUsed)}
        >{showUsed
          ? "Hide used and expired"
          : `Show ${otherRows.length} used or expired`}</button
      >
    {/if}
  {/if}
  {#if revokeError}
    <p class="text-micro text-danger-text" role="alert">{revokeError}</p>
  {/if}
</div>
