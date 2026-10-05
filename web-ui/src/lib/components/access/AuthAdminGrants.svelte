<script>
  import { coreClient } from "$lib/coreClient";
  import {
    buildAdminRows,
    grantCandidates,
    hostForTarget,
  } from "$lib/authAdminModel.js";
  import { formatAbsoluteDateTime } from "$lib/formatDate";
  import ActorAvatar from "$lib/components/ActorAvatar.svelte";
  import Button from "$lib/components/Button.svelte";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import CopyableId from "$lib/components/CopyableId.svelte";

  /**
   * Who can administer this workspace, and the two ways that changes.
   *
   * People hold administration implicitly and keep it until their access is
   * revoked in People; agents hold it only from an explicit grant, which a
   * person makes and unmakes here. Both appear in one list, because a list of
   * only the explicit grants would read as if nobody else could act.
   */
  let {
    admins = [],
    principals = [],
    hosts = [],
    auditEvents = [],
    currentPrincipalId = "",
    displayName = (principal) => principal?.username ?? "",
    canEdit = false,
    forbidden = false,
    onchanged = () => {},
  } = $props();

  let target = $state("");
  let pending = $state(null);
  let busy = $state(false);
  let error = $state("");

  let rows = $derived(
    buildAdminRows({
      admins,
      principals,
      hosts,
      auditEvents,
      currentPrincipalId,
      displayName,
    }),
  );
  let candidates = $derived(grantCandidates({ principals, admins, hosts }));

  function grantedLabel(row) {
    if (!row.grantedAt) return "";
    const when = formatAbsoluteDateTime(row.grantedAt);
    if (!when) return "";
    return row.kind === "human"
      ? `admin since joining ${when}`
      : `since ${when}`;
  }

  function prepareGrant() {
    const id = target.trim();
    if (!id) return;
    const match = candidates.find(
      (candidate) => candidate.principalId === id || candidate.username === id,
    );
    pending = {
      id,
      name: match?.username || id,
      grant: true,
      host:
        match?.hostSlug ||
        hostForTarget(id, { principals, hosts }) ||
        "the agent's host",
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
      error = changeFailure(err);
    } finally {
      busy = false;
    }
  }

  // A refused change is a permission answer, not a fault to debug.
  function changeFailure(err) {
    const status = Number(err?.status);
    if (status === 401 || status === 403) {
      return "Only a signed-in person can change administration.";
    }
    const details = String(err?.details ?? "").trim();
    if (details) return details;
    return err?.message || "Administration access was not changed.";
  }
</script>

{#if forbidden}
  <p class="text-meta text-fg-muted" data-auth-admins-forbidden>
    Only workspace administrators can see who administers this workspace.
  </p>
{:else}
  <p class="mb-3 text-meta text-fg-muted">
    Administrators can decide host enrollments, manage enrollment tokens, revoke
    other hosts, and read inventory and audit. Principal and human invitation
    revocation require a person. Only a person can grant or revoke this
    authority. Revocation takes effect on the next request. Granting an agent on
    a host trusts every process that can read that host's shared key and request
    that agent name. Human invitations remain human-only. A person administers
    from the moment they join; take it away by revoking their access under
    People.
  </p>
  <ul class="overflow-hidden rounded-md border border-line bg-bg-soft">
    {#each rows as row (row.key)}
      <li
        class="flex items-center gap-3 border-t border-line-subtle px-4 py-2 first:border-t-0"
        data-auth-admin={row.principalId}
      >
        <ActorAvatar label={row.name} seed={row.principalId} size="xs" />
        <div class="min-w-0 flex-1">
          <p class="truncate text-meta text-fg">
            {row.name}
            {#if row.isYou}<span class="ml-1 text-micro text-fg-muted"
                >(you)</span
              >{/if}
            <span
              class="ml-1 rounded bg-line-subtle px-1.5 py-0.5 text-micro text-fg-muted"
              >{row.kind === "human" ? "Person" : "Agent"}</span
            >
          </p>
          <p
            class="flex min-w-0 items-center gap-1.5 text-micro text-fg-subtle"
          >
            <!-- An agent with no display name reads as its own handle; the
                 line below it would then repeat the line above. -->
            {#if row.kind === "agent" && row.handle && row.handle !== row.name}
              <span class="truncate font-mono">{row.handle}</span>
            {/if}
            {#if row.hostSlug}
              <span aria-hidden="true">·</span>
              <span class="truncate">{row.hostSlug}</span>
            {/if}
            {#if grantedLabel(row)}
              <span aria-hidden="true">·</span>
              <span class="truncate">{grantedLabel(row)}</span>
            {/if}
            <span aria-hidden="true">·</span>
            <CopyableId value={row.principalId} label="Copy principal id" />
          </p>
        </div>
        {#if row.revocable && canEdit}
          <Button
            size="sm"
            variant="secondary"
            onclick={() => {
              error = "";
              pending = {
                id: row.principalId,
                name: row.name,
                grant: false,
              };
            }}>Revoke administration</Button
          >
        {/if}
      </li>
    {:else}
      <li class="px-4 py-3 text-meta text-fg-muted">
        Nobody administers this workspace yet.
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
        {#each candidates as candidate (candidate.principalId)}
          <option
            value={candidate.username}
            label={candidate.hostSlug ? `on ${candidate.hostSlug}` : undefined}
          ></option>
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
{/if}
<ConfirmModal
  open={Boolean(pending)}
  title={pending?.grant
    ? "Grant agent administration"
    : "Revoke agent administration"}
  message={pending?.grant
    ? `Allow ${pending.name} to decide host enrollments, manage enrollment tokens, revoke other hosts, and read inventory and audit? Granting this agent trusts every process that can read the shared key on ${pending.host} and request this agent name. Principal and human invitation revocation require a person. This grant and its host are audited.`
    : `Remove administration access from ${pending?.name ?? "this agent"}? It can no longer approve host enrollments, manage enrollment tokens or revoke hosts. Its next request will use the updated permissions.`}
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
