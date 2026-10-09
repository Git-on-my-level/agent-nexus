<script>
  import { onDestroy } from "svelte";

  import Button from "$lib/components/Button.svelte";
  import CopyButton from "$lib/components/CopyButton.svelte";
  import { isAdministrationRefusal } from "$lib/coreAuthErrors.js";
  import { coreClient } from "$lib/coreClient";
  import {
    PM_RUNNERS,
    DEFAULT_PM_RUNNER_KEY,
    pmRunnerFor,
  } from "$lib/setup/pmRunners.js";
  import {
    SETUP_TOKEN_LABEL,
    SETUP_TOKEN_LIFETIME_SECONDS,
    buildMachinePrompt,
    buildPmPrompt,
    formatCountdown,
    hostEnrollCommand,
    resolveCliInstallCommand,
    setupPromptBlockedReason,
  } from "$lib/setup/setupPrompt.js";

  /**
   * One way to set a machine up, wherever the reader runs out of road.
   *
   * Access → Hosts, the empty Agents roster and PM setup all need the same
   * answer — "install anx on the computer your agents use, join it to this
   * workspace, prove it worked" — so they get the same component rather than
   * three sets of instructions that drift. The only differences are which
   * prompt it builds and which live signal the page watches; the page owns the
   * watching, and passes the result in.
   *
   * The enrollment token is issued when this panel opens, not when Copy is
   * clicked: the clipboard write has to be synchronous inside the click or
   * Safari drops it.
   */
  let {
    /** `"machine"` connects a host; `"pm"` also installs the PM service. */
    kind = "machine",
    cliBaseUrl = "",
    cliInstallCommand = "",
    workspaceLabel = "",
    /** Chosen PM harness; bindable so the page can remember it. */
    runnerKey = $bindable(DEFAULT_PM_RUNNER_KEY),
    heading = "",
    lede = "",
    /** Rendered under the prompt: the page's live "has it arrived yet" line. */
    status,
  } = $props();

  /**
   * Which path is on screen. Defaults to the prompt, and to the commands when
   * this deployment cannot produce a prompt worth copying — landing a reader
   * on an explanation of what is missing would hide the path that still works.
   */
  let tab = $state("");
  let activeTab = $derived(
    tab || (blockedReason || refused ? "manual" : "agent"),
  );
  /**
   * Core says this reader may not issue enrollment tokens.
   *
   * Issuing one is an administration write, and this panel also appears on the
   * Agents roster, which any reader can open. "You are not an administrator"
   * is an answer, not a fault: the panel keeps the commands and stops offering
   * a prompt it cannot build.
   */
  let refused = $state(false);
  let token = $state(
    /** @type {{ secret: string, id: string, expiresAt: string } | null} */ (
      null
    ),
  );
  let issuing = $state(false);
  let issueError = $state("");
  let now = $state(Date.now());
  /**
   * What the current token was issued for. Held in a plain object rather than
   * `$state` because it records what the effect has already done; making it
   * reactive would re-run the effect that writes it.
   */
  const issued = { key: "" };
  /** Set once this panel is gone, so a late response strands nothing. */
  let destroyed = false;
  /**
   * The reader has the current token somewhere this page cannot see.
   *
   * A token's whole purpose is to leave the browser — copied here, pasted into
   * an agent over there — so once it has been handed over this panel must not
   * take it back. Only a token nobody ever received is retired on the way out.
   */
  let handedOver = false;

  let installCommand = $derived(resolveCliInstallCommand(cliInstallCommand));
  let blockedReason = $derived(setupPromptBlockedReason({ cliBaseUrl }));
  let manualEnrollCommand = $derived(hostEnrollCommand({ cliBaseUrl }));
  let runner = $derived(pmRunnerFor(runnerKey));

  /** Core's expiry, or NaN when it did not report one (an older core). */
  let expiresAtMs = $derived(Date.parse(token?.expiresAt ?? ""));
  let expiryKnown = $derived(Number.isFinite(expiresAtMs));
  let remainingMs = $derived(expiryKnown ? expiresAtMs - now : 0);
  let countdown = $derived(formatCountdown(remainingMs));
  /*
   * Only a known expiry can be in the past. A core that reports no expiry is
   * not reporting an expired token, and calling it one would tell the reader
   * to replace a token that works.
   */
  let expired = $derived(expiryKnown && remainingMs <= 0);

  let prompt = $derived(
    !token
      ? ""
      : kind === "pm"
        ? buildPmPrompt({
            workspaceLabel,
            cliBaseUrl,
            installCommand,
            token: token.secret,
            expiresAt: token.expiresAt,
            runnerKey,
          })
        : buildMachinePrompt({
            workspaceLabel,
            cliBaseUrl,
            installCommand,
            token: token.secret,
            expiresAt: token.expiresAt,
          }),
  );

  /**
   * Hand a token back when it will not be used.
   *
   * Best effort by design: the reader has already moved on, and an unused
   * token expires on its own. Failing here must not surface as a failure of
   * whatever they did next.
   *
   * @param {string} id
   */
  function retire(id) {
    if (!id) return;
    void coreClient.revokeHostEnrollmentToken(id).catch(() => {});
  }

  function describe(error, fallback) {
    return (
      error?.details ||
      (error instanceof Error ? error.message : "") ||
      fallback
    );
  }

  /**
   * Issue the single-use token this prompt carries.
   *
   * `replacing` revokes the token this panel issued a moment ago, so asking
   * for a fresh one does not leave a live secret behind in a clipboard the
   * reader has already given up on.
   */
  async function issueToken({ replacing = false } = {}) {
    if (issuing) return;
    issuing = true;
    issueError = "";
    refused = false;
    const retiring = replacing ? token?.id : "";
    try {
      const result = await coreClient.createHostEnrollmentToken({
        label: SETUP_TOKEN_LABEL,
        expires_in_seconds: SETUP_TOKEN_LIFETIME_SECONDS,
      });
      const secret = String(result?.token ?? "");
      const record = result?.enrollment_token ?? {};
      if (!secret) {
        // Core made a token and did not hand back its secret: unusable here,
        // and live until it expires unless it goes back now.
        retire(String(record.id ?? ""));
        throw new Error("Core returned no token.");
      }
      const issuedToken = {
        secret,
        id: String(record.id ?? ""),
        expiresAt: String(record.expires_at ?? ""),
      };
      /*
       * The panel was torn down while this was in flight. Keeping the secret
       * would strand a live workspace-joining credential nothing can reach,
       * so retire it instead of assigning it to state no one is reading.
       */
      if (destroyed) {
        retire(issuedToken.id);
        return;
      }
      token = issuedToken;
      handedOver = false;
      now = Date.now();
      if (retiring) {
        retire(retiring);
      }
    } catch (error) {
      if (isAdministrationRefusal(error)) {
        refused = true;
      } else {
        issueError = describe(error, "A setup token could not be issued.");
      }
    } finally {
      issuing = false;
    }
  }

  /*
   * One token per panel, per reader. Keyed on what the prompt is built from,
   * so switching workspaces re-issues and a re-render does not.
   */
  $effect(() => {
    if (blockedReason) return;
    const key = `${kind}|${cliBaseUrl}`;
    if (issued.key === key) return;
    issued.key = key;
    void issueToken();
  });

  /** Only the countdown ages on screen, and only while a token is live. */
  $effect(() => {
    if (!token || expired) return;
    const timer = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(timer);
  });

  /*
   * Hand the token back on the way out. This panel issues one on open, and
   * the onboarding path crosses three surfaces plus the walkthrough: without
   * this, one pass through it would leave four live credentials behind, each
   * good for joining any machine to the workspace for half an hour.
   */
  onDestroy(() => {
    destroyed = true;
    /*
     * Not retired once it has been copied: the panel unmounts on an ordinary
     * navigation, and on a failed read that hides this section for one poll
     * tick, and taking the token back there would kill the paste the reader is
     * in the middle of. An uncopied token is the one nobody is waiting on.
     *
     * This is best effort either way — `onDestroy` does not run on a tab
     * close — which is why the 30-minute lifetime, not this, is the bound that
     * matters.
     */
    if (!handedOver) retire(token?.id ?? "");
    token = null;
  });
</script>

<section class="space-y-4" data-setup-prompt={kind}>
  {#if heading || lede}
    <div class="space-y-1">
      {#if heading}
        <h3 class="text-meta font-semibold text-fg">{heading}</h3>
      {/if}
      {#if lede}
        <p class="max-w-prose text-micro text-fg-muted">{lede}</p>
      {/if}
    </div>
  {/if}

  <div class="flex gap-1 border-b border-line-subtle" role="tablist">
    {#each [["agent", "Have your agent do it"], ["manual", "Run it myself"]] as [key, label] (key)}
      <button
        class="-mb-px border-b-2 px-3 py-1.5 text-meta transition-colors {activeTab ===
        key
          ? 'border-accent font-medium text-fg'
          : 'border-transparent text-fg-muted hover:text-fg'}"
        type="button"
        role="tab"
        aria-selected={activeTab === key}
        data-setup-tab={key}
        onclick={() => (tab = key)}>{label}</button
      >
    {/each}
  </div>

  {#if activeTab === "agent" && refused}
    <p
      class="max-w-prose rounded-md border border-line bg-bg-soft px-3 py-2 text-micro text-fg-muted"
      data-setup-prompt-refused
    >
      Only workspace administrators can hand out a setup prompt, because it
      carries a token that joins a machine to this workspace. Ask one of them,
      or run the commands yourself.
    </p>
  {:else if activeTab === "agent" && blockedReason}
    <!--
        A prompt is run unread, so one that cannot work is worse than none.
        The commands beside it still help a reader who can see the address and
        reason about it, so only the prompt is withheld.
      -->
    <p
      class="max-w-prose rounded-md border border-line bg-bg-soft px-3 py-2 text-micro text-fg-muted"
      data-setup-prompt-blocked
    >
      {blockedReason}
    </p>
  {:else if activeTab === "agent"}
    <div class="space-y-3">
      {#if kind === "pm"}
        <label
          class="flex flex-wrap items-center gap-2 text-micro text-fg-muted"
        >
          <span>Which agent runs the PM</span>
          <select
            bind:value={runnerKey}
            class="rounded-md border border-line bg-bg px-2 py-1.5 text-meta text-fg"
            data-setup-runner
          >
            {#each PM_RUNNERS as option (option.key)}
              <option value={option.key}>{option.label}</option>
            {/each}
          </select>
        </label>
      {/if}

      <div class="flex flex-wrap items-center gap-2">
        <!--
          Offered only once there is something to copy. A copy control that
          hands over an empty string and then says "copied" is worse than one
          that is not there yet: the reader pastes nothing into their agent and
          has no reason to suspect it.
        -->
        {#if prompt}
          <CopyButton
            value={prompt}
            label="Copy setup prompt"
            text="Copy setup prompt"
            variant="primary"
            size="md"
            title="Copies the whole prompt, including a single-use token"
            oncopied={() => (handedOver = true)}
          />
        {:else}
          <Button variant="primary" size="default" disabled busy={issuing}>
            {issuing ? "Preparing the prompt…" : "No prompt yet"}
          </Button>
        {/if}
        {#if token && !expired && countdown}
          <span
            class="rounded-full border border-warn px-2 py-0.5 text-micro text-warn-text"
            data-setup-token-countdown>token expires in {countdown}</span
          >
        {/if}
        <Button
          variant="ghost"
          size="compact"
          busy={issuing}
          onclick={() => issueToken({ replacing: true })}>New token</Button
        >
      </div>

      <p class="max-w-prose text-micro text-fg-subtle">
        {#if kind === "pm"}
          Paste it into {runner.label} on the computer that should run the PM. It
          installs anx, joins the machine to this workspace if it is not in it yet,
          installs the PM and reports back.
        {:else}
          Paste it into whatever agent you already use on that computer. It
          installs anx, joins the machine to this workspace, checks the result
          with a real call and reports back.
        {/if}
      </p>

      {#if issueError}
        <p
          class="rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
          data-setup-token-error
        >
          {issueError}
        </p>
      {:else if expired}
        <p class="text-micro text-fg-muted" data-setup-token-expired>
          That token has expired. Choose New token for a fresh one.
        </p>
      {/if}

      {#if prompt}
        <details class="rounded-md border border-line bg-bg">
          <summary
            class="cursor-pointer px-3 py-1.5 text-micro text-fg-muted hover:text-fg"
            >Show the prompt</summary
          >
          <pre
            class="max-h-80 overflow-auto whitespace-pre-wrap break-words border-t border-line-subtle px-3 py-2 font-mono text-micro text-fg"
            data-setup-prompt-text>{prompt}</pre>
        </details>
      {/if}

      <p class="max-w-prose text-micro text-fg-subtle">
        The token is single-use and scoped to joining a machine to this
        workspace. The prompt never asks your agent for a passkey, an invitation
        or administration — those stay with you.
      </p>
    </div>
  {:else}
    <div class="space-y-3" data-setup-manual>
      <div class="space-y-1">
        <p class="text-micro text-fg-muted">
          1. Install the anx CLI on that computer (macOS or Linux, Python 3.8 or
          newer).
        </p>
        <div class="flex items-center gap-1 rounded bg-bg-soft px-2 py-1.5">
          <code
            class="min-w-0 flex-1 break-all font-mono text-micro text-fg"
            data-setup-install-command>{installCommand}</code
          >
          <CopyButton value={installCommand} label="Copy install command" />
        </div>
      </div>
      <div class="space-y-1">
        <p class="text-micro text-fg-muted">
          2. Join it to this workspace. It prints a code and waits; approve it
          here when the codes match.
        </p>
        <div class="flex items-center gap-1 rounded bg-bg-soft px-2 py-1.5">
          <code
            class="min-w-0 flex-1 break-all font-mono text-micro text-fg"
            data-host-enroll-command>{manualEnrollCommand}</code
          >
          <CopyButton value={manualEnrollCommand} label="Copy command" />
        </div>
      </div>
      {#if kind === "pm"}
        <div class="space-y-1">
          <p class="text-micro text-fg-muted">
            3. Install the PM service with the harness that should run it.
          </p>
          <div class="flex items-center gap-1 rounded bg-bg-soft px-2 py-1.5">
            <code
              class="min-w-0 flex-1 break-all font-mono text-micro text-fg"
              data-pm-install-command
              >{`anx ${cliBaseUrl ? `--base-url ${cliBaseUrl} ` : ""}pm install`}</code
            >
            <CopyButton
              value={`anx ${cliBaseUrl ? `--base-url ${cliBaseUrl} ` : ""}pm install`}
              label="Copy command"
            />
          </div>
          <p class="text-micro text-fg-subtle">
            Run that one in a terminal you can type into: with no --runner it
            asks which agent should run the PM.
          </p>
        </div>
      {/if}
    </div>
  {/if}

  {#if status}
    {@render status()}
  {/if}
</section>
