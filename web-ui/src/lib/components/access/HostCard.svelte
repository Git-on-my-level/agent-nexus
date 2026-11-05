<script>
  import { onMount } from "svelte";
  import { get } from "svelte/store";
  import { durableForm } from "$lib/actions/durableForm.js";
  import { localWorkScope, canUseLocalWorkScope } from "$lib/localWorkScope.js";
  import {
    loadLocalWork,
    localWorkEpoch,
    saveLocalWork,
  } from "$lib/localWorkStore.js";
  import HostBridgeBadge from "$lib/components/agents/HostBridgeBadge.svelte";
  import HostPersonaList from "$lib/components/access/HostPersonaList.svelte";
  import Button from "$lib/components/Button.svelte";
  import CopyableId from "$lib/components/CopyableId.svelte";
  import Time from "$lib/time/Time.svelte";

  /**
   * One enrolled machine and the agents derived on it. Exclusions and
   * revocation are edited in place; revoking asks for the host name first
   * because every agent on the machine loses access with it.
   */
  let {
    host,
    agents = [],
    canManage = false,
    canArchive = false,
    showIdle = false,
    query = "",
    workspaceHref = (path) => path,
    now = Date.now(),
    onexclusions = async () => {},
    onrevoke = async () => {},
    onarchive = async () => {},
  } = $props();

  const NAME_PATTERN = /^[a-z][a-z0-9-]{0,31}$/;

  let revoked = $derived(Boolean(host?.revoked_at));
  let activeAgents = $derived(
    agents.filter((agent) => !agent.revoked_at && !agent.archived_at),
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
  let draftReady = $state(false);
  let draftError = $state("");
  let draftTarget = $derived(`host:${host.id}:exclusion`);
  let alive = true;
  onMount(() => () => {
    alive = false;
  });
  function exclusionDraft() {
    return newName ? { name: newName } : null;
  }
  function restoreExclusion(value) {
    newName = value?.name ?? "";
    editing = Boolean(value?.name);
    draftError = "";
    exclusionsError = "";
  }

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
    if (savingExclusions || !draftReady) return;
    const scope = get(localWorkScope);
    const epoch = localWorkEpoch(scope);
    const target = draftTarget;
    const submitted = newName;
    const adding =
      names.includes(submitted.trim()) && !excluded.includes(submitted.trim());
    const current = () =>
      alive &&
      scope === get(localWorkScope) &&
      epoch === localWorkEpoch(scope) &&
      target === draftTarget;
    savingExclusions = true;
    exclusionsError = "";
    try {
      if (!canUseLocalWorkScope(scope))
        throw new Error("Sign in before saving.");
      await saveLocalWork(scope, "draft", target, exclusionDraft(), { epoch });
      if (!current()) return;
      await onexclusions(host, names);
      if (!adding) return;
      const latest = await loadLocalWork(scope, "draft", target);
      if (!current() || newName !== submitted) return;
      if (
        JSON.stringify(latest) !==
        JSON.stringify(submitted ? { name: submitted } : null)
      )
        return;
      const clearing = saveLocalWork(scope, "draft", target, null, { epoch });
      newName = "";
      await clearing;
    } catch {
      if (!current()) return;
      exclusionsError =
        "The change could not be confirmed. Your draft is saved if device storage is available. Check the current exclusions before trying again.";
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
</script>

<article
  id={`host-${host.slug}`}
  class="scroll-mt-20 overflow-hidden rounded-md border border-line bg-bg-soft {revoked
    ? 'opacity-60'
    : ''}"
  data-host={host.slug}
  use:durableForm={{
    target: draftTarget,
    value: exclusionDraft(),
    read: exclusionDraft,
    onRestore: restoreExclusion,
    onReady: (ready) => {
      draftReady = ready;
    },
    checkpointClicks: false,
  }}
  onlocal-save-error={(event) => {
    draftError = event.detail;
  }}
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
          <span>revoked <Time value={host.revoked_at} {now} /></span>
        {:else}
          <span>enrolled <Time value={host.created_at} {now} /></span>
        {/if}
        <span class="text-fg-subtle" aria-hidden="true">·</span>
        <span class="inline-flex items-center gap-1"
          >key <CopyableId value={host.key_id} label="Copy host key id" /></span
        >
      </p>
    </div>
    {#if !revoked}
      <div class="shrink-0 pt-0.5 text-micro">
        <HostBridgeBadge {host} variant="pill" />
      </div>
    {/if}
  </header>
  {#if !revoked}
    <div class="px-4 pb-2 empty:hidden">
      <HostBridgeBadge {host} variant="details" />
    </div>
  {/if}

  <div class="border-t border-line-subtle">
    <HostPersonaList
      {host}
      agents={agents.filter((agent) => !agent.archived_at)}
      {excluded}
      canArchive={canArchive && !revoked}
      {showIdle}
      {query}
      {workspaceHref}
      {now}
      {onarchive}
    />
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
            disabled={!draftReady}
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
            disabled={!draftReady || !newName.trim() || Boolean(nameError)}
            >Exclude</Button
          >
          <Button
            variant="ghost"
            size="compact"
            onclick={() => {
              editing = false;
              exclusionsError = "";
            }}>Done</Button
          >
          <p class="basis-full text-micro text-fg-subtle">
            An excluded name cannot act from this machine; its open sessions end
            and its history stays.
          </p>
        </form>
      {/if}
      {#if draftError}
        <p class="text-micro text-danger-text" role="alert">{draftError}</p>
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
