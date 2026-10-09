<script>
  import Button from "$lib/components/Button.svelte";
  import SetupPrompt from "$lib/components/setup/SetupPrompt.svelte";
  import {
    pmConnected,
    pmSetupOffered,
    pmStatusCommand,
  } from "$lib/pm/onboardingState.js";
  import { DEFAULT_PM_RUNNER_KEY } from "$lib/setup/pmRunners.js";

  /**
   * Onboarding a PM agent: one explanation, one prompt, one live answer.
   *
   * The PM runs on the reader's own computer, so the only thing this surface
   * can do is hand over something to run and then watch for the first
   * heartbeat. It flips to "Connected" in place, without a reload.
   *
   * The prompt is one-shot on purpose: it joins the machine to the workspace
   * first if it is not in it yet, so a reader who has never run anx is not sent
   * away to Access and back.
   */
  let {
    presence = null,
    cliBaseUrl = "",
    cliInstallCommand = "",
    workspaceLabel = "",
    /** Where Ask PM lives, offered once a PM is connected. */
    pmHref = "",
    /** The live watch gave up: say what to check instead of waiting on. */
    gaveUpWaiting = false,
  } = $props();

  let runnerKey = $state(DEFAULT_PM_RUNNER_KEY);

  let statusCommand = $derived(pmStatusCommand({ cliBaseUrl }));
  let waiting = $derived(pmSetupOffered(presence));
  let connected = $derived(pmConnected(presence));
</script>

<!-- The page heading already says "Set up your PM"; this does not repeat it. -->
<section class="space-y-4" data-pm-setup>
  <p class="text-meta text-fg-muted">
    The PM runs on your own computer, through the agent you already use (Hermes,
    Claude Code, or any command you name). Nothing about it runs on the server,
    so it stops when your machine does.
  </p>

  <SetupPrompt
    kind="pm"
    {cliBaseUrl}
    {cliInstallCommand}
    {workspaceLabel}
    bind:runnerKey
  >
    {#snippet status()}
      <div
        class="rounded-md border px-3 py-2.5 {connected
          ? 'border-ok bg-ok-soft'
          : 'border-line-subtle bg-bg-soft'}"
        role="status"
        aria-live="polite"
        data-pm-setup-state={connected ? "connected" : "waiting"}
      >
        {#if connected}
          <div class="flex flex-wrap items-center justify-between gap-3">
            <p class="text-meta font-medium text-ok-text">
              Connected. Your PM is running.
            </p>
            {#if pmHref}
              <Button variant="primary" size="compact" href={pmHref}
                >Ask PM</Button
              >
            {/if}
          </div>
        {:else if waiting && gaveUpWaiting}
          <p class="text-meta text-fg-muted" data-pm-setup-gave-up>
            Still no PM. Run <code class="font-mono text-micro text-fg"
              >{statusCommand}</code
            >
            on your computer to see what the service is doing, then reload this page.
          </p>
        {:else if waiting}
          <p class="flex items-center gap-2 text-meta text-fg-muted">
            <span
              class="h-1.5 w-1.5 shrink-0 animate-pulse rounded-full bg-accent-solid"
              aria-hidden="true"
            ></span>
            Waiting for your PM to connect…
          </p>
          <p class="mt-1 text-micro text-fg-subtle">
            This page notices the first heartbeat on its own, for the next few
            minutes.
          </p>
        {:else}
          <p class="text-meta text-fg-muted">
            Checking whether a PM is connected…
          </p>
        {/if}
      </div>
    {/snippet}
  </SetupPrompt>

  <p class="text-micro text-fg-subtle">
    Anything this workspace's PM was asked before is still here, and readable
    again once one is connected.
  </p>
</section>
