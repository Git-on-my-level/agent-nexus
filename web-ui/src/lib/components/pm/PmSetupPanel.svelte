<script>
  import CopyButton from "$lib/components/CopyButton.svelte";
  import Button from "$lib/components/Button.svelte";
  import {
    pmConnected,
    pmInstallCommand,
    pmSetupOffered,
    pmStatusCommand,
  } from "$lib/pm/onboardingState.js";

  /**
   * Onboarding a PM agent: one explanation, one command, one live answer.
   *
   * The PM runs on the reader's own computer, so the only thing this surface
   * can do is tell them what to run and then watch for the first heartbeat.
   * It flips to "Connected" in place, without a reload.
   */
  let {
    presence = null,
    cliBaseUrl = "",
    /** Where Ask PM lives, offered once a PM is connected. */
    pmHref = "",
    /** The live watch gave up: say what to check instead of waiting on. */
    gaveUpWaiting = false,
  } = $props();

  let command = $derived(pmInstallCommand({ cliBaseUrl }));
  let statusCommand = $derived(pmStatusCommand({ cliBaseUrl }));
  let waiting = $derived(pmSetupOffered(presence));
  let connected = $derived(pmConnected(presence));
</script>

<!-- The page heading already says "Set up your PM"; this does not repeat it. -->
<section class="space-y-5" data-pm-setup>
  <p class="max-w-prose text-meta text-fg-muted">
    The PM runs on your own computer, through the agent you already use (Hermes,
    Claude Code, or any command you name). Nothing about it runs on the server,
    so it stops when your machine does.
  </p>

  <div class="space-y-2">
    <h2 class="text-micro font-medium text-fg">
      Run this in a terminal on your computer
    </h2>
    <div
      class="flex items-center gap-1 rounded-md border border-line bg-bg px-2 py-1.5"
    >
      <code
        class="min-w-0 flex-1 break-all font-mono text-micro text-fg"
        data-pm-install-command>{command}</code
      >
      <CopyButton value={command} label="Copy command" />
    </div>
    <p class="text-micro text-fg-subtle">
      It asks which agent should run the PM, starts it in the background, and
      keeps it running after you log out. Anything this workspace's PM was asked
      before is still here, and readable again once one is connected.
    </p>
  </div>

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
          <Button variant="primary" size="compact" href={pmHref}>Ask PM</Button>
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
      <p class="text-meta text-fg-muted">Checking whether a PM is connected…</p>
    {/if}
  </div>
</section>
