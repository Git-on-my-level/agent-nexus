<script>
  import {
    KNOWN_ADAPTERS,
    agentPath,
    agentStateShortLabel,
    formatAge,
  } from "$lib/agentPresence.js";
  import AgentBridgeIndicator from "$lib/components/agents/AgentBridgeIndicator.svelte";
  import AgentStateDot from "$lib/components/agents/AgentStateDot.svelte";
  import Button from "$lib/components/Button.svelte";
  import CopyableId from "$lib/components/CopyableId.svelte";
  import { formatAbsoluteDateTime } from "$lib/formatDate";

  /**
   * One enrolled machine and the agents derived on it. Exclusions and
   * revocation are edited in place; revoking asks for the host name first
   * because every agent on the machine loses access with it.
   */
  let {
    host,
    agents = [],
    canManage = false,
    workspaceHref = (path) => path,
    now = Date.now(),
    onexclusions = async () => {},
    onrevoke = async () => {},
  } = $props();

  const NAME_PATTERN = /^[a-z][a-z0-9-]{0,31}$/;

  let revoked = $derived(Boolean(host?.revoked_at));
  let activeAgents = $derived(agents.filter((agent) => !agent.revoked_at));
  let bridgeOnline = $derived(
    activeAgents.some((agent) => agent.bridge_online),
  );
  let excluded = $derived(host?.excluded_names ?? []);
  let unusedAdapters = $derived(
    (host?.discovered_adapters ?? []).filter(
      (name) =>
        !agents.some((agent) => agent.name === name) &&
        !excluded.includes(name),
    ),
  );

  let editing = $state(false);
  let newName = $state("");
  let savingExclusions = $state(false);
  let exclusionsError = $state("");

  let revoking = $state(false);
  let revokeTyped = $state("");
  let revokeBusy = $state(false);
  let revokeError = $state("");

  let nameError = $derived(
    newName.trim() && !NAME_PATTERN.test(newName.trim())
      ? "Use lowercase letters, digits and dashes, starting with a letter."
      : "",
  );

  async function saveExclusions(names) {
    savingExclusions = true;
    exclusionsError = "";
    try {
      await onexclusions(host, names);
      newName = "";
    } catch (error) {
      exclusionsError =
        error?.details ||
        (error instanceof Error ? error.message : "") ||
        "Exclusions were not saved.";
    } finally {
      savingExclusions = false;
    }
  }

  function addExclusion(event) {
    event.preventDefault();
    const name = newName.trim();
    if (!name || nameError || excluded.includes(name)) return;
    void saveExclusions([...excluded, name]);
  }

  async function confirmRevoke() {
    if (revokeTyped.trim() !== host.slug) return;
    revokeBusy = true;
    revokeError = "";
    try {
      await onrevoke(host);
      revoking = false;
      revokeTyped = "";
    } catch (error) {
      revokeError =
        error?.details ||
        (error instanceof Error ? error.message : "") ||
        "The host was not revoked.";
    } finally {
      revokeBusy = false;
    }
  }

  function agentKind(agent) {
    if (agent.identity_kind === "adopted") return "adopted";
    return KNOWN_ADAPTERS.has(agent.name) ? "adapter" : "persona";
  }
</script>

<article
  id={`host-${host.slug}`}
  class="scroll-mt-20 overflow-hidden rounded-md border border-line bg-bg-soft {revoked
    ? 'opacity-60'
    : ''}"
  data-host={host.slug}
>
  <header class="flex flex-wrap items-start gap-x-4 gap-y-1 px-4 py-3">
    <div class="min-w-0 flex-1">
      <h3
        class="flex flex-wrap items-center gap-2 text-meta font-semibold text-fg"
      >
        {host.display_name || host.slug}
        {#if host.display_name && host.display_name !== host.slug}
          <span class="font-normal text-fg-muted">{host.slug}</span>
        {/if}
        {#if revoked}
          <span
            class="rounded bg-danger-soft px-1.5 py-0.5 text-micro font-medium text-danger-text"
            >Revoked</span
          >
        {/if}
      </h3>
      <p
        class="mt-0.5 flex flex-wrap items-center gap-x-1.5 text-micro text-fg-muted"
      >
        <span>{host.os_user}@{host.hostname}</span>
        <span class="text-fg-subtle" aria-hidden="true">·</span>
        {#if revoked}
          <span title={formatAbsoluteDateTime(host.revoked_at)}
            >revoked {formatAge(host.revoked_at, now)} ago</span
          >
        {:else}
          <span title={formatAbsoluteDateTime(host.created_at)}
            >enrolled {formatAge(host.created_at, now) === "<1m"
              ? "just now"
              : `${formatAge(host.created_at, now)} ago`}</span
          >
        {/if}
        <span class="text-fg-subtle" aria-hidden="true">·</span>
        <span class="inline-flex items-center gap-1"
          >key <CopyableId value={host.key_id} label="Copy host key id" /></span
        >
      </p>
    </div>
    {#if !revoked}
      <div class="shrink-0 pt-0.5 text-micro">
        <AgentBridgeIndicator online={bridgeOnline} showLabel />
      </div>
    {/if}
  </header>

  <div class="border-t border-line-subtle">
    {#if agents.length}
      <ul>
        {#each agents as agent (agent.id)}
          <li
            class="flex items-center gap-3 border-t border-line-subtle px-4 py-1.5 first:border-t-0 {agent.revoked_at
              ? 'opacity-60'
              : ''}"
            data-host-agent={agent.handle}
          >
            <AgentStateDot state={agent.state} />
            <a
              class="min-w-0 truncate text-meta text-fg hover:underline"
              href={workspaceHref(agentPath(agent))}>{agent.name}</a
            >
            <span class="min-w-0 truncate text-micro text-fg-subtle"
              >@{agent.handle}</span
            >
            <span class="ml-auto shrink-0 text-micro text-fg-subtle"
              >{agentKind(agent)}</span
            >
            <span class="w-16 shrink-0 text-right text-micro text-fg-muted"
              >{agent.revoked_at
                ? "revoked"
                : excluded.includes(agent.name)
                  ? "excluded"
                  : agentStateShortLabel(agent.state)}</span
            >
          </li>
        {/each}
      </ul>
    {:else}
      <p class="px-4 py-2 text-micro text-fg-muted">
        No agents yet. An agent appears here the first time it runs <code
          class="rounded bg-line px-1 py-px text-fg">anx</code
        > on this machine.
      </p>
    {/if}
    {#if unusedAdapters.length && !revoked}
      <p
        class="border-t border-line-subtle px-4 py-1.5 text-micro text-fg-subtle"
      >
        Also installed: {unusedAdapters.join(", ")}
      </p>
    {/if}
  </div>

  {#if !revoked}
    <footer class="space-y-2 border-t border-line-subtle px-4 py-2.5">
      <div class="flex flex-wrap items-center gap-2 text-micro">
        <span class="text-fg-subtle">Excluded</span>
        {#if excluded.length}
          {#each excluded as name (name)}
            <span
              class="inline-flex items-center gap-1 rounded bg-line-subtle px-1.5 py-0.5 text-fg-muted"
              data-host-exclusion={name}
            >
              {name}
              {#if editing && canManage}
                <button
                  class="text-fg-subtle hover:text-danger-text"
                  type="button"
                  aria-label={`Allow ${name} on ${host.slug} again`}
                  disabled={savingExclusions}
                  onclick={() =>
                    saveExclusions(excluded.filter((entry) => entry !== name))}
                  >×</button
                >
              {/if}
            </span>
          {/each}
        {:else}
          <span class="text-fg-muted">none</span>
        {/if}
        {#if canManage && !editing}
          <button
            class="text-fg-muted hover:text-fg hover:underline"
            type="button"
            onclick={() => (editing = true)}>Edit</button
          >
        {/if}
        {#if canManage && !revoking}
          <button
            class="ml-auto text-danger-text hover:underline"
            type="button"
            onclick={() => {
              revoking = true;
              revokeTyped = "";
              revokeError = "";
            }}>Revoke host…</button
          >
        {/if}
      </div>

      {#if editing && canManage}
        <form
          class="flex flex-wrap items-start gap-2"
          onsubmit={addExclusion}
          data-anx-no-submit-shortcut
        >
          <div class="min-w-0">
            <input
              bind:value={newName}
              class="w-44 rounded-md border border-line bg-bg px-2 py-1 text-micro text-fg"
              placeholder="Name to block, e.g. cursor"
              aria-label={`Name to exclude on ${host.slug}`}
              autocomplete="off"
            />
            {#if nameError}
              <p class="mt-1 text-micro text-danger-text">{nameError}</p>
            {/if}
          </div>
          <Button
            type="submit"
            variant="secondary"
            size="compact"
            busy={savingExclusions}
            disabled={!newName.trim() || Boolean(nameError)}>Exclude</Button
          >
          <Button
            variant="ghost"
            size="compact"
            onclick={() => {
              editing = false;
              newName = "";
              exclusionsError = "";
            }}>Done</Button
          >
          <p class="basis-full text-micro text-fg-subtle">
            An excluded name cannot act from this machine; its open sessions end
            and its history stays.
          </p>
        </form>
      {/if}
      {#if exclusionsError}
        <p class="text-micro text-danger-text" role="alert">
          {exclusionsError}
        </p>
      {/if}

      {#if revoking && canManage}
        <div
          class="space-y-2 rounded-md border px-3 py-2.5"
          style="border-color: color-mix(in srgb, var(--danger) 45%, var(--line))"
          data-host-revoke-confirm
        >
          <p class="text-micro text-fg">
            Revoking <span class="font-semibold">{host.slug}</span> cuts off
            {activeAgents.length === 1
              ? "its only agent"
              : `all ${activeAgents.length} of its agents`} now{#if activeAgents.length}{" "}({activeAgents
                .map((agent) => agent.name)
                .join(", ")}){/if}: their sessions and tokens stop working.
            Their history stays. The machine has to enroll again to come back.
          </p>
          <form
            class="flex flex-wrap items-center gap-2"
            data-anx-no-submit-shortcut
            onsubmit={(event) => {
              event.preventDefault();
              void confirmRevoke();
            }}
          >
            <input
              bind:value={revokeTyped}
              class="w-44 rounded-md border border-line bg-bg px-2 py-1 font-mono text-micro text-fg"
              placeholder={host.slug}
              aria-label={`Type ${host.slug} to confirm`}
              autocomplete="off"
            />
            <Button
              type="submit"
              variant="destructive"
              size="compact"
              busy={revokeBusy}
              disabled={revokeTyped.trim() !== host.slug}
              >{revokeBusy ? "Revoking…" : "Revoke host"}</Button
            >
            <Button
              variant="ghost"
              size="compact"
              disabled={revokeBusy}
              onclick={() => (revoking = false)}>Cancel</Button
            >
          </form>
          {#if revokeError}
            <p class="text-micro text-danger-text" role="alert">
              {revokeError}
            </p>
          {/if}
        </div>
      {/if}
    </footer>
  {/if}
</article>
