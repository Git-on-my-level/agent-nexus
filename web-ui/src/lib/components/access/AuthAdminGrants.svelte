<script>
  import { coreClient } from "$lib/coreClient";
  import {
    buildAdminRows,
    grantCandidates,
    hostForTarget,
  } from "$lib/authAdminModel.js";
  import { isAdministrationRefusal } from "$lib/coreAuthErrors.js";
  import { formatAbsoluteDate, formatAbsoluteDateTime } from "$lib/formatDate";
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
    activeHumanCount = 0,
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
  // The principal list is one page. Core's own active-human count is
  // authoritative, so the section can say how many people it is not showing
  // instead of implying that the page is the whole truth.
  let unlistedHumans = $derived(
    Math.max(
      0,
      activeHumanCount - rows.filter((row) => row.kind === "human").length,
    ),
  );

  // An unparseable timestamp has no date to show, and nothing else on the row
  // may be substituted for it.
  function grantedLabel(row) {
    const when = formatAbsoluteDate(row.grantedAt);
    if (!when) return "";
    return row.kind === "human"
      ? `admin since joining ${when}`
      : `admin since ${when}`;
  }

  /** The row's detail line, so a missing part never leaves a stray "·". */
  function details(row) {
    return [
      // An agent with no display name reads as its own handle; repeating it
      // here would restate the line above.
      row.kind === "agent" && row.handle !== row.name ? row.handle : "",
      row.hostSlug,
      grantedLabel(row),
    ].filter(Boolean);
  }

  function prepareGrant() {
    const id = target.trim();
    if (!id) return;
    // Administration is not granted to a person; they hold it already. Saying
    // so beats a confirm step that promises something core will reject.
    const human = principals.find(
      (principal) =>
        principal?.principal_kind === "human" &&
        (principal.agent_id === id || principal.username === id),
    );
    if (human) {
      error =
        "People already administer this workspace. Only agents are granted administration.";
      return;
    }
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
    if (isAdministrationRefusal(err) || Number(err?.status) === 403) {
      return "Only a person can change administration.";
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
  <!-- What a grant actually trusts is spelled out in the confirm step, where
       the reader is about to make one. Here: who holds it, and how it moves. -->
  <p class="mb-3 text-meta text-fg-muted">
    Administrators decide host enrollments, manage enrollment tokens, revoke
    other hosts, and read inventory and audit. Revoking a principal or a human
    invitation still needs a person, and only a person can grant or revoke
    administration. A person administers from the moment they join; take that
    away by revoking their access under People.
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
            {#each details(row) as detail, index (index)}
              {#if index > 0}<span aria-hidden="true">·</span>{/if}
              <span
                class="truncate {index === 0 && row.kind === 'agent'
                  ? 'font-mono'
                  : ''}"
                title={detail === grantedLabel(row)
                  ? formatAbsoluteDateTime(row.grantedAt)
                  : undefined}>{detail}</span
              >
            {/each}
            {#if details(row).length}<span aria-hidden="true">·</span>{/if}
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
        No agents have administration access.
      </li>
    {/each}
  </ul>
  {#if unlistedHumans}
    <p class="mt-2 text-micro text-fg-subtle" data-auth-admins-unlisted>
      {unlistedHumans === 1 ? "1 more person" : `${unlistedHumans} more people`}
      {unlistedHumans === 1 ? "administers" : "administer"} this workspace and is
      not shown here. People lists everyone.
    </p>
  {/if}
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
          <!-- No `label`: Firefox renders it instead of the value, which
               would hide every agent name behind its host. -->
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
    {#if error && !pending}
      <!-- A refusal raised before the confirm step opens has no dialog to
           appear in. -->
      <p class="mt-2 text-micro text-danger-text" role="alert">{error}</p>
    {/if}
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
