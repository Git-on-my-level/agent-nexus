<script>
  import SetupPrompt from "$lib/components/setup/SetupPrompt.svelte";
  import { CONNECT_HARNESSES } from "$lib/firstRun.js";

  /**
   * The whole first screen of a workspace nothing has used yet.
   *
   * A cold Overview used to be five empty sections whose only call to action
   * was pinning a visual report — which no workspace can do before an agent
   * has written one. This replaces them with the single move that makes every
   * other surface fill up: connect the computer the reader's agents run on.
   *
   * It owns no state. Whether the workspace is still cold is the Overview's
   * question, answered from the snapshot it already loads, so this panel
   * appears and is replaced by the same live refresh that fills the sections
   * behind it — no polling of its own, no second read.
   */
  let {
    /** Switches the panel to the arrival confirmation. */
    connected = false,
    /** Display name of the agent that arrived, when one is known. */
    connectedLabel = "",
    cliBaseUrl = "",
    cliInstallCommand = "",
    workspaceLabel = "",
    /** `bindWorkspaceHref` from the page: links stay workspace-scoped. */
    hrefFor = (/** @type {string} */ path) => path,
  } = $props();
</script>

{#if connected}
  <!--
    The flip. The reader pasted a prompt into a terminal on another machine
    and came back to a page that cannot acknowledge it; this says the thing
    they were waiting to hear, once, and then the workspace speaks for itself.
  -->
  <section
    class="rounded-md border border-ok bg-ok-soft px-4 py-3"
    role="status"
    data-first-agent-connected
  >
    <p class="text-meta font-medium text-ok-text">
      {connectedLabel
        ? `${connectedLabel} is connected.`
        : "Your first agent is connected."}
    </p>
    <p class="mt-1 max-w-prose text-micro text-fg-muted">
      Its work, its questions and what it finishes now show up here. Anything it
      needs an answer for lands in <a
        class="text-accent-text hover:underline"
        href={hrefFor("/decide")}>Decide</a
      >.
    </p>
  </section>
{:else}
  <section
    class="rounded-md border border-line bg-panel"
    aria-labelledby="first-agent-heading"
    data-overview-section="first-agent"
    data-first-agent-panel
  >
    <header class="border-b border-line px-4 py-3">
      <h2 id="first-agent-heading" class="text-subtitle text-fg">
        Connect your first agent
      </h2>
      <p class="mt-1 max-w-prose text-meta text-fg-muted">
        This workspace is empty because nothing has reported into it yet. Agents
        reach it through the computer they run on: set that computer up once,
        and every agent on it shows its work, its questions and its progress
        here.
      </p>
    </header>

    <div class="px-4 py-4">
      <SetupPrompt
        kind="machine"
        {cliBaseUrl}
        {cliInstallCommand}
        {workspaceLabel}
        lede={`Copy the prompt and paste it into ${CONNECT_HARNESSES} on that computer. It installs the anx CLI, joins the machine to this workspace and reports back — no further setup from you.`}
      >
        {#snippet status()}
          <!--
            The live half of the panel. The Overview already refreshes this
            snapshot off the workspace event stream, so this line is replaced
            by the arrival confirmation the moment the first agent registers
            or posts; the reader does not have to reload to find out.
          -->
          <p
            class="flex items-center gap-2 border-t border-line-subtle pt-3 text-micro text-fg-muted"
            role="status"
            data-first-agent-waiting
          >
            <span
              class="h-1.5 w-1.5 shrink-0 animate-pulse rounded-full bg-accent"
              aria-hidden="true"
            ></span>
            Waiting for your first agent… this page notices it on its own.
          </p>
        {/snippet}
      </SetupPrompt>
    </div>

    <!--
      What the reader gets for doing it, in the words of the surfaces it
      fills. The example is marked as one and is not a row in Decide: a
      workspace's first real item should be the reader's, not ours.
    -->
    <div class="border-t border-line px-4 py-4">
      <h3 class="text-meta font-semibold text-fg">Then what</h3>
      <ul class="mt-2 max-w-prose space-y-1.5 text-micro text-fg-muted">
        <li>
          <a class="text-accent-text hover:underline" href={hrefFor("/agents")}
            >Agents</a
          > lists every agent on that machine and what each one is doing.
        </li>
        <li>
          <a
            class="text-accent-text hover:underline"
            href={hrefFor("/initiatives")}>Initiatives</a
          > holds the work they pick up, and what you file for them.
        </li>
        <li>
          <a class="text-accent-text hover:underline" href={hrefFor("/decide")}
            >Decide</a
          > is the only place that needs you: an agent that is stuck or needs a decision
          asks here and waits for your answer.
        </li>
      </ul>

      <figure class="mt-3 max-w-prose" data-first-agent-example>
        <figcaption class="text-micro text-fg-subtle">
          Example — what an ask looks like in Decide
        </figcaption>
        <div
          class="mt-1.5 rounded-md border border-dashed border-line bg-bg-soft px-3 py-2"
          aria-hidden="true"
        >
          <p class="text-meta text-fg">
            Deploy the migration to production tonight?
          </p>
          <p class="mt-0.5 text-micro text-fg-muted">
            claude · asked 4 minutes ago · waiting on you
          </p>
        </div>
      </figure>
    </div>
  </section>
{/if}
