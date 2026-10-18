<script>
  import { onMount } from "svelte";
  import { get } from "svelte/store";
  import {
    authenticatedAgent,
    isHumanWorkspacePrincipal,
  } from "$lib/authSession";
  import Button from "$lib/components/Button.svelte";
  import InlineAlert from "$lib/components/InlineAlert.svelte";
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  import { durableForm } from "$lib/actions/durableForm.js";
  import { coreClient } from "$lib/coreClient";
  import { localWorkScope } from "$lib/localWorkScope.js";
  import { localWorkEpoch, saveLocalWork } from "$lib/localWorkStore.js";
  import { mutationOutbox } from "$lib/mutationOutbox.js";
  import {
    NOTIFICATION_SETTINGS_OPERATION,
    NOTIFICATION_SETTINGS_TARGET as target,
    addressLabel,
    channelLabel,
    draftDirty,
    draftFromSettings,
    enqueueNotificationSettings,
    isPrincipalRow,
    reconcileDraft,
    rowEnabled,
    setRowEnabled,
    validateDraft,
  } from "$lib/notificationSettings.js";
  import { usePageTitle } from "$lib/pageTitle.svelte.js";

  let settings = $state(null);
  let draft = $state(null);
  let draftReady = $state(false);
  let loading = $state(true);
  let loadError = $state("");
  let saveError = $state("");
  let notice = $state("");
  let preparing = $state(false);
  let alive = true;
  let ticket = 0;

  const isHuman = $derived(isHumanWorkspacePrincipal($authenticatedAgent));
  const pending = $derived(
    $mutationOutbox.find((entry) => entry.target === target),
  );
  const problems = $derived(validateDraft(draft));
  const dirty = $derived(
    Boolean(draft && settings) && draftDirty(draft, settings),
  );
  const available = $derived(settings?.available_channels ?? []);

  const snapshot = () => (draft ? JSON.parse(JSON.stringify(draft)) : null);

  function restore(value, reason = "") {
    if (reason === "privacy") {
      draft = settings ? draftFromSettings(settings) : null;
      notice = "";
      return;
    }
    // A draft can restore before the canonical list arrives; load() reconciles.
    draft = value && settings ? reconcileDraft(value, settings) : value;
  }

  async function load() {
    const mine = ++ticket;
    const scope = get(localWorkScope);
    const epoch = localWorkEpoch(scope);
    loading = true;
    loadError = "";
    try {
      const result = await coreClient.getNotificationSettings();
      if (
        !alive ||
        mine !== ticket ||
        scope !== get(localWorkScope) ||
        epoch !== localWorkEpoch(scope)
      )
        return;
      settings = result;
      draft = draft ? reconcileDraft(draft, result) : draftFromSettings(result);
    } catch {
      if (mine === ticket)
        loadError =
          "Notification settings could not be loaded. Try again in a moment.";
    } finally {
      if (mine === ticket) loading = false;
    }
  }

  $effect(() => {
    if (!isHuman) {
      loading = false;
      return;
    }
    if (draftReady) void load();
  });

  async function save() {
    if (!draft || pending || preparing || !dirty) return;
    preparing = true;
    saveError = "";
    notice = "";
    const scope = get(localWorkScope);
    const epoch = localWorkEpoch(scope);
    try {
      const value = snapshot();
      await saveLocalWork(scope, "draft", target, value, { epoch });
      if (scope !== get(localWorkScope) || epoch !== localWorkEpoch(scope))
        return;
      await enqueueNotificationSettings(value, {
        href: window.location.pathname,
      });
    } catch (error) {
      saveError =
        error?.message ||
        "Your choices are saved on this device. Copy them before closing this tab.";
    } finally {
      preparing = false;
    }
  }

  async function discard() {
    if (pending || preparing || !settings) return;
    const scope = get(localWorkScope);
    const epoch = localWorkEpoch(scope);
    const previous = snapshot();
    draft = draftFromSettings(settings);
    saveError = "";
    try {
      await saveLocalWork(scope, "draft", target, null, { epoch });
    } catch {
      if (scope !== get(localWorkScope) || epoch !== localWorkEpoch(scope))
        return;
      draft = previous;
      saveError =
        "The saved draft could not be discarded. Your choices are still here.";
    }
  }

  onMount(() => {
    const confirmed = (event) => {
      if (
        event.detail?.scope !== get(localWorkScope) ||
        event.detail.operation !== NOTIFICATION_SETTINGS_OPERATION
      )
        return;
      const result = event.detail.result;
      if (!Array.isArray(result?.channels)) return;
      settings = result;
      // Keep newer typing; only follow the server when nothing changed since.
      if (
        !draft ||
        JSON.stringify(draft) === JSON.stringify(event.detail.draft)
      )
        draft = draftFromSettings(result);
      notice = "Notification settings saved.";
    };
    window.addEventListener("anx:mutation-confirmed", confirmed);
    return () => {
      alive = false;
      ++ticket;
      window.removeEventListener("anx:mutation-confirmed", confirmed);
    };
  });

  usePageTitle(() => "Notifications · Agent Nexus");
</script>

<div class="mx-auto max-w-3xl sm:px-4 sm:py-6">
  <h1 class="mb-1 text-title text-fg">Notifications</h1>
  <p class="mb-4 text-micro text-fg-muted">
    Choose where this workspace reaches you when something needs your decision.
  </p>

  {#if !isHuman}
    <InlineAlert variant="warn">
      Notification settings belong to people. Sign in as a person to change
      them.
    </InlineAlert>
  {:else}
    <div
      use:durableForm={{
        target,
        value: snapshot(),
        read: snapshot,
        onRestore: restore,
        onReady: (ready) => {
          draftReady = ready;
        },
        checkpointClicks: false,
      }}
      onlocal-save-error={() => {
        saveError =
          "Local saving is unavailable. Your choices are still visible; copy them before closing this page.";
      }}
    >
      <MutationSendStatus {target} />

      {#if loadError}
        <InlineAlert class="mb-3">
          {loadError}
          <button
            type="button"
            class="ml-2 underline"
            onclick={() => void load()}>Try again</button
          >
        </InlineAlert>
      {/if}
      {#if saveError}
        <InlineAlert class="mb-3">{saveError}</InlineAlert>
      {/if}
      {#if notice && !pending}
        <p class="mb-3 text-meta text-fg-muted" role="status">{notice}</p>
      {/if}

      {#if loading && !settings}
        <p class="text-meta text-fg-muted">Loading…</p>
      {:else if settings && available.length === 0}
        <div
          class="rounded-md border border-line bg-panel p-4 text-meta text-fg"
        >
          <p class="font-medium">No notification channels are installed.</p>
          <p class="mt-1 text-fg-muted">
            An operator needs to install a channel adapter for email or Telegram
            first. Once it is running, the channel appears here.
          </p>
        </div>
      {:else if settings && draft}
        <form
          class="space-y-4"
          onsubmit={(event) => {
            event.preventDefault();
            void save();
          }}
        >
          {#each draft.rows as row (row.channel)}
            {#if isPrincipalRow(row)}
              <fieldset class="rounded-md border border-line bg-panel p-4">
                <legend class="px-1 text-meta font-medium text-fg"
                  >{channelLabel(row.channel, row.label)}</legend
                >
                <label class="flex items-start gap-2 text-meta text-fg">
                  <input
                    type="checkbox"
                    class="mt-0.5"
                    data-testid="notify-{row.channel}-enabled"
                    disabled={Boolean(pending) || preparing}
                    checked={rowEnabled(row)}
                    onchange={(event) =>
                      setRowEnabled(row, event.currentTarget.checked)}
                  />
                  <span>
                    Send notifications from this workspace
                    <span class="mt-0.5 block text-micro text-fg-muted">
                      On by default. Turn it off and this workspace stops
                      notifying you here; your other workspaces are unaffected.
                    </span>
                  </span>
                </label>
                {#if row.manageUrl}
                  <a
                    class="mt-3 inline-block text-micro text-accent-text hover:underline"
                    href={row.manageUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    >Change where notifications reach you</a
                  >
                {/if}
              </fieldset>
            {:else}
              <fieldset class="rounded-md border border-line bg-panel p-4">
                <legend class="px-1 text-meta font-medium text-fg"
                  >{channelLabel(row.channel, row.label)}</legend
                >
                <label
                  class="mb-1 block text-micro font-medium text-fg-muted"
                  for="notify-{row.channel}-address"
                  >{addressLabel(row.channel)}</label
                >
                <input
                  id="notify-{row.channel}-address"
                  type={row.channel === "email" ? "email" : "text"}
                  autocomplete="off"
                  disabled={Boolean(pending) || preparing}
                  bind:value={row.address}
                  class="w-full rounded-md border border-line bg-bg px-3 py-1.5 text-meta text-fg placeholder:text-fg-subtle focus:border-accent focus:outline-none"
                />
                {#if problems[row.channel]}
                  <p class="mt-1 text-micro text-danger-text" role="alert">
                    {problems[row.channel]}
                  </p>
                {:else}
                  <p class="mt-1 text-micro text-fg-subtle">
                    Leave blank to stop notifications on this channel.
                  </p>
                {/if}
                <label class="mt-3 flex items-center gap-2 text-meta text-fg">
                  <input
                    type="checkbox"
                    disabled={Boolean(pending) || preparing}
                    bind:checked={row.push_enabled}
                  />
                  Immediate alerts when a decision is overdue
                </label>
                <label class="mt-2 flex items-center gap-2 text-meta text-fg">
                  <input
                    type="checkbox"
                    disabled={Boolean(pending) || preparing}
                    bind:checked={row.digest_enabled}
                  />
                  Daily digest
                </label>
              </fieldset>
            {/if}
          {/each}
          <div class="flex justify-end gap-2">
            <Button
              variant="ghost"
              type="button"
              disabled={!dirty || Boolean(pending) || preparing}
              onclick={() => void discard()}>Discard changes</Button
            >
            <Button
              variant="primary"
              type="submit"
              disabled={!dirty ||
                Boolean(pending) ||
                preparing ||
                Object.keys(problems).length > 0}
              >{preparing ? "Saving…" : "Save"}</Button
            >
          </div>
        </form>
      {/if}
    </div>
  {/if}
</div>
