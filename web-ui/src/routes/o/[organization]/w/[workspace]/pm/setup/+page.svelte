<script>
  import { onDestroy } from "svelte";
  import { page } from "$app/stores";
  import { browser } from "$app/environment";

  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import CopyButton from "$lib/components/CopyButton.svelte";
  import PmSetupPanel from "$lib/components/pm/PmSetupPanel.svelte";
  import { initializeAuthSession } from "$lib/authSession";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { formatAbsoluteDateTime } from "$lib/formatDate";
  import { formatWait } from "$lib/inboxMailbox.js";
  import {
    pmConnected,
    pmLastSeenLabel,
    pmOffline,
    pmInstallCommand,
    pmSetupOffered,
    pmStatusCommand,
    pmUninstallCommand,
  } from "$lib/pm/onboardingState.js";
  import { pmPresence, startPmPresencePoll } from "$lib/pm/presence.js";

  /**
   * One surface for the PM's whole lifecycle: set it up when there is none,
   * and manage it once there is. Both answers are the same three facts —
   * where the PM runs, what state it is in, and the command that changes
   * that — so they do not deserve two pages.
   */
  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  let workspaceSlug = $derived($page.data?.workspace?.slug ?? "");
  let cliBaseUrl = $derived($page.data?.workspace?.cliBaseUrl ?? "");
  let cliInstallCommand = $derived(
    $page.data?.workspace?.cliInstallCommand ?? "",
  );
  let workspaceLabel = $derived(
    $page.data?.workspace?.label || $page.params.workspace || "",
  );
  let presence = $derived(
    $pmPresence.workspace === workspaceSlug ? $pmPresence : null,
  );
  let setupOffered = $derived(pmSetupOffered(presence));
  let connected = $derived(pmConnected(presence));
  /*
   * The reader who arrived here with no PM is watching for one word. Keep the
   * setup flow on screen when it arrives so the answer lands where they are
   * looking, instead of replacing the page with a status table the moment the
   * first heartbeat lands. Manage is a navigation away, and the PM status in
   * the shell links back to it.
   *
   * Recorded against the workspace it was true for: this component is reused
   * across workspaces, and another workspace's connected PM must not be shown
   * an install flow.
   */
  let watchedWorkspace = $state("");
  /** The watch window closed with no PM: the page says what to check. */
  let gaveUpWaiting = $state(false);
  $effect(() => {
    if (setupOffered) watchedWorkspace = workspaceSlug;
  });
  /** The setup flow: before a PM exists, and while its arrival is awaited. */
  let showSetup = $derived(
    setupOffered ||
      (Boolean(workspaceSlug) && watchedWorkspace === workspaceSlug) ||
      !presence?.loaded,
  );
  /** Status and the commands that change it, for a PM that already exists. */
  let showManage = $derived(
    !showSetup && (pmConnected(presence) || pmOffline(presence)),
  );
  let now = $state(Date.now());
  let lastSeen = $derived(pmLastSeenLabel(presence, now, formatWait));
  let runsAs = $derived(
    [presence?.runner, presence?.host].filter(Boolean).join(" · "),
  );
  let installCommand = $derived(pmInstallCommand({ cliBaseUrl }));
  let statusCommand = $derived(pmStatusCommand({ cliBaseUrl }));
  let uninstallCommand = $derived(pmUninstallCommand({ cliBaseUrl }));

  let stopPoll = null;

  /*
   * While no PM has ever connected, watch for the first heartbeat so the
   * reader sees "Connected" without reloading. The poll stops the moment a
   * PM is onboarded; one bounded state read per tick.
   */
  $effect(() => {
    if (!browser || !workspaceSlug) return;
    const slug = workspaceSlug;
    stopPoll?.();
    stopPoll = null;
    let cancelled = false;
    gaveUpWaiting = false;
    /*
     * The session has to exist before core will answer. Without this the
     * first read races the shell's own bootstrap and comes back 401, which
     * used to be indistinguishable from "this core has no PM state".
     */
    void initializeAuthSession({
      fetchFn: globalThis.fetch.bind(globalThis),
      workspaceSlug: $page.params.workspace,
      authDriver: "pm-setup",
    })
      .catch(() => {})
      .then(() => {
        if (cancelled) return;
        stopPoll = startPmPresencePoll(slug, {
          onGaveUp: () => {
            if (!cancelled) gaveUpWaiting = true;
          },
        });
      });
    return () => {
      cancelled = true;
      stopPoll?.();
      stopPoll = null;
    };
  });

  // Only "last seen" ages on screen, and only while a PM is offline.
  let agesOnScreen = $derived(
    Boolean(presence?.lastSeen) && !connected && showManage,
  );
  $effect(() => {
    if (!browser || !agesOnScreen) return;
    const timer = setInterval(() => (now = Date.now()), 30_000);
    return () => clearInterval(timer);
  });

  onDestroy(() => stopPoll?.());
</script>

<svelte:head><title>PM setup · Agent Nexus</title></svelte:head>

<WorkspacePageShell>
  <WorkspacePageHeader title={setupOffered ? "Set up your PM" : "Your PM"}>
    {#snippet subtitle()}
      A PM agent runs on your computer and answers from this workspace.
    {/snippet}
    {#snippet actions()}
      {#if showManage}
        <a class="ui-btn-secondary" href={workspaceHref("/pm")}>Ask PM</a>
      {/if}
    {/snippet}
  </WorkspacePageHeader>

  {#if showManage}
    <section class="space-y-5" data-pm-manage>
      <dl
        class="grid gap-3 rounded-md border border-line bg-bg-soft px-4 py-3 text-meta sm:grid-cols-2 lg:grid-cols-3"
      >
        <div class="min-w-0">
          <dt class="text-micro text-fg-subtle">State</dt>
          <dd
            class="flex items-center gap-1.5 {connected
              ? 'text-ok-text'
              : 'text-fg'}"
            data-pm-manage-state={connected ? "connected" : "offline"}
          >
            <span
              class="h-1.5 w-1.5 shrink-0 rounded-full {connected
                ? 'bg-ok'
                : 'bg-fg-subtle'}"
              aria-hidden="true"
            ></span>
            {connected ? "Connected" : "Offline"}
          </dd>
        </div>
        <div class="min-w-0">
          <dt class="text-micro text-fg-subtle">Last seen</dt>
          <dd
            class="truncate text-fg"
            title={presence?.lastSeen
              ? formatAbsoluteDateTime(presence.lastSeen)
              : undefined}
          >
            {#if connected}
              Now
            {:else if lastSeen}
              {lastSeen} ago
            {:else}
              Not recorded
            {/if}
          </dd>
        </div>
        <!--
          Core's presence contract carries no runner or host label yet, so
          this cell appears only if one arrives rather than standing there
          reading "Not reported" forever.
        -->
        {#if runsAs}
          <div class="min-w-0">
            <dt class="text-micro text-fg-subtle">Runs as</dt>
            <dd class="truncate text-fg">{runsAs}</dd>
          </div>
        {/if}
      </dl>

      {#if !connected}
        <p class="text-meta text-fg-muted">
          Nothing is running the PM right now. Start the machine it is installed
          on, or check the service with the command below — and if you have
          never installed it on this computer, <code
            class="font-mono text-micro text-fg">{installCommand}</code
          >
          does that. Anything you ask in the meantime waits for it to come back.
        </p>
      {/if}

      <div class="space-y-3">
        <h2 class="text-micro font-medium text-fg">
          Manage it from a terminal
        </h2>
        {#each [["Check the service", statusCommand, "pm-status-command"], ["Remove it from this machine", uninstallCommand, "pm-uninstall-command"]] as [label, value, hook] (hook)}
          <div class="space-y-1">
            <p class="text-micro text-fg-subtle">{label}</p>
            <div
              class="flex items-center gap-1 rounded-md border border-line bg-bg px-2 py-1.5"
            >
              <code
                class="min-w-0 flex-1 break-all font-mono text-micro text-fg"
                data-pm-command={hook}>{value}</code
              >
              <CopyButton {value} label={`Copy: ${label.toLowerCase()}`} />
            </div>
          </div>
        {/each}
      </div>
    </section>
  {:else if showSetup}
    <PmSetupPanel
      {presence}
      {cliBaseUrl}
      {cliInstallCommand}
      {workspaceLabel}
      {gaveUpWaiting}
      pmHref={workspaceHref("/pm")}
    />
  {:else}
    <!--
      Core answered without a PM state: it is older than this UI. Saying
      "no PM" would be a guess, and offering setup for a PM that may already
      be running is worse than saying what is actually known.
    -->
    <p class="text-meta text-fg-muted" data-pm-state-unavailable>
      This workspace does not report PM state yet. Run
      <code class="font-mono text-micro text-fg">{statusCommand}</code>
      on your computer to see whether a PM is installed.
    </p>
  {/if}
</WorkspacePageShell>
