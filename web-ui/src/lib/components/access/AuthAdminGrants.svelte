<script>
  import { coreClient } from "$lib/coreClient";
  import Button from "$lib/components/Button.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";

  let {
    admins = [],
    principals = [],
    hosts = [],
    canEdit = false,
    onchanged = () => {},
  } = $props();
  let target = $state("");
  let pending = $state(null);
  let busy = $state(false);
  let error = $state("");
  let candidates = $derived(
    principals.filter((p) => p.principal_kind === "agent" && !p.revoked),
  );

  function prepareGrant() {
    const id = target.trim();
    const host = hosts.find((h) =>
      h.agents?.some((a) => a.id === id || a.handle === id),
    );
    pending = {
      id,
      name: id,
      grant: true,
      host: host?.slug ?? "the agent's host",
    };
  }

  async function changeGrant() {
    if (!pending) return;
    busy = true;
    error = "";
    try {
      if (pending.grant) await coreClient.grantAuthAdmin(pending.id);
      else await coreClient.revokeAuthAdmin(pending.id);
      pending = null;
      target = "";
      await onchanged();
    } catch (err) {
      error = err?.message || "Administration access was not changed.";
    } finally {
      busy = false;
    }
  }
</script>

<p class="mb-3 text-meta text-fg-muted">
  Granted agents can decide host enrollments, manage enrollment tokens, revoke
  other hosts, and read inventory and audit. Principal and human invitation
  revocation require a person. Only a person can grant or revoke this authority.
  Revocation takes effect on the next request. Granting an agent on a host
  trusts every process that can read that host's shared key and request that
  agent name. Human invitations remain human-only.
</p>
<ul class="space-y-2">
  {#each admins as admin (admin.principal_id)}
    <li
      class="flex items-center justify-between gap-3 rounded-md border border-line px-3 py-2"
    >
      <span class="min-w-0 break-all text-meta text-fg"
        >{admin.username}{admin.host_slug ? ` (${admin.host_slug})` : ""}</span
      >
      {#if canEdit}
        <Button
          size="sm"
          variant="secondary"
          onclick={() => {
            error = "";
            pending = {
              id: admin.principal_id,
              name: admin.username,
              grant: false,
            };
          }}>Revoke administration</Button
        >
      {/if}
    </li>
  {:else}
    <li class="text-meta text-fg-muted">
      No agents have administration access.
    </li>
  {/each}
</ul>
{#if canEdit}
  <form
    class="mt-3 flex flex-wrap items-end gap-2"
    onsubmit={(event) => {
      event.preventDefault();
      error = "";
      prepareGrant();
    }}
  >
    <label class="min-w-48 flex-1 text-meta text-fg-muted">
      Agent username or principal ID
      <input
        class="mt-1 block w-full rounded-md border border-line bg-bg px-3 py-2 text-meta text-fg"
        bind:value={target}
        list="auth-admin-candidates"
        required
      />
    </label>
    <datalist id="auth-admin-candidates">
      {#each candidates as candidate (candidate.agent_id)}
        <option value={candidate.username}></option>
      {/each}
    </datalist>
    <Button
      type="submit"
      size="sm"
      variant="secondary"
      disabled={!target.trim()}>Grant administration</Button
    >
  </form>
{/if}
<ConfirmModal
  open={Boolean(pending)}
  title={pending?.grant
    ? "Grant agent administration"
    : "Revoke agent administration"}
  message={pending?.grant
    ? `Allow ${pending.name} to decide host enrollments, manage enrollment tokens, revoke other hosts, and read inventory and audit? Granting this agent trusts every process that can read the shared key on ${pending.host} and request this agent name. Principal and human invitation revocation require a person. This grant and its host are audited.`
    : `Remove administration access from ${pending?.name ?? "this agent"}? Its next request will use the updated permissions.`}
  confirmLabel={pending?.grant
    ? "Grant administration"
    : "Revoke administration"}
  {busy}
  {error}
  onconfirm={changeGrant}
  oncancel={() => {
    pending = null;
    error = "";
  }}
/>
