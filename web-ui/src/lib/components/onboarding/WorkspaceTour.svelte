<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import SpotlightTour from "$lib/components/onboarding/SpotlightTour.svelte";
  import { copyText } from "$lib/clipboard.js";
  import { coreClient } from "$lib/coreClient";
  import {
    isWorkspaceTourSeen,
    markWorkspaceTourSeen,
    replayTourSignal,
  } from "$lib/tourState";
  import { stripWorkspacePath, workspacePath } from "$lib/workspacePaths";
  import { pmSetupOffered, pmStateKnown } from "$lib/pm/onboardingState.js";
  import { pmPresence } from "$lib/pm/presence.js";
  import {
    SETUP_TOKEN_LABEL,
    SETUP_TOKEN_LIFETIME_SECONDS,
    buildMachinePrompt,
    setupPromptBlockedReason,
  } from "$lib/setup/setupPrompt.js";

  let {
    organizationSlug = "",
    workspaceSlug = "",
    devActorModeReady = false,
    /** Optional first-name / display label used to personalize the welcome */
    userLabel = "",
    /** anx-core API origin, for the setup prompt the last step hands over. */
    cliBaseUrl = "",
    /** How this deployment installs the CLI. */
    cliInstallCommand = "",
    /** Workspace display name, named in the prompt so a paste is unambiguous. */
    workspaceLabel = "",
  } = $props();

  let tourOpen = $state(false);
  let pathWhenOpened = $state(/** @type {string} */ (""));
  let eligibilityLoading = $state(false);
  let eligible = $state(false);

  let relPath = $derived(
    organizationSlug && workspaceSlug
      ? stripWorkspacePath($page.url.pathname, organizationSlug, workspaceSlug)
      : "",
  );

  let ctaAccessHref = $derived(
    organizationSlug && workspaceSlug
      ? `${workspacePath(organizationSlug, workspaceSlug, "/access")}?from=tour#hosts`
      : "/access?from=tour#hosts",
  );

  /*
   * The last step hands the reader the thing they actually need: a prompt to
   * paste into the agent on the machine they want to connect. The token it
   * carries is fetched when that step appears, so the click itself only writes
   * to the clipboard and navigates — a write behind an await is dropped by
   * Safari, and a CTA that silently copies nothing is worse than one that
   * only navigates.
   *
   * Everything here is best effort. When no prompt could be prepared (no
   * reachable API address, a refused token, an older core) the step falls back
   * to the plain "Enroll a machine →" link, which still works.
   */
  let setupPrompt = $state("");
  let preparingPrompt = false;
  /**
   * What the cached prompt is good for: which workspace, and until when.
   *
   * The tour can be replayed hours later from Overview, and it has no
   * countdown and no "New token" of its own. Without this, a replay would
   * copy a dead token, or one minted for a different workspace, and say
   * nothing about it.
   */
  const promptValidity = { key: "", expiresAtMs: 0 };

  function promptStillGood() {
    if (!setupPrompt) return false;
    if (promptValidity.key !== `${workspaceSlug}|${cliBaseUrl}`) return false;
    // A token core gave no expiry for is not assumed to live forever.
    if (!promptValidity.expiresAtMs) return false;
    // A minute of headroom: a prompt pasted on the edge is a wasted paste.
    return promptValidity.expiresAtMs - Date.now() > 60_000;
  }

  async function prepareSetupPrompt() {
    if (preparingPrompt || promptStillGood()) return;
    setupPrompt = "";
    if (setupPromptBlockedReason({ cliBaseUrl })) return;
    preparingPrompt = true;
    try {
      const result = await coreClient.createHostEnrollmentToken({
        label: SETUP_TOKEN_LABEL,
        expires_in_seconds: SETUP_TOKEN_LIFETIME_SECONDS,
      });
      const secret = String(result?.token ?? "");
      const expiresAt = String(result?.enrollment_token?.expires_at ?? "");
      const expiresAtMs = Date.parse(expiresAt);
      if (!secret || !Number.isFinite(expiresAtMs)) return;
      setupPrompt = buildMachinePrompt({
        workspaceLabel: workspaceLabel || workspaceSlug,
        cliBaseUrl,
        installCommand: cliInstallCommand,
        token: secret,
        expiresAt,
      });
      promptValidity.key = `${workspaceSlug}|${cliBaseUrl}`;
      promptValidity.expiresAtMs = expiresAtMs;
    } catch {
      // No prompt; the CTA stays a plain link to Access.
    } finally {
      preparingPrompt = false;
    }
  }

  /** @param {number} index */
  function onTourStep(index) {
    if (index === tourSteps.length - 1) void prepareSetupPrompt();
  }

  function copySetupPrompt() {
    // Re-checked at the click: the card may have been on screen for a while.
    if (!promptStillGood()) return;
    void copyText(setupPrompt);
  }

  /*
   * The PM step describes whatever the slot it points at actually is. Three
   * cases, because "no PM yet" and "we have not read the state" are not the
   * same claim: offer setup only when core says there is none, and when it
   * has said nothing, describe the PM without promising either.
   */
  let pmTourState = $derived(
    $pmPresence.workspace === workspaceSlug ? $pmPresence : null,
  );
  let pmNeedsSetup = $derived(pmSetupOffered(pmTourState));
  let pmTourKnown = $derived(pmStateKnown(pmTourState));

  let firstName = $derived(deriveFirstName(userLabel));

  /** @param {string} label */
  function deriveFirstName(label) {
    const trimmed = String(label ?? "").trim();
    if (!trimmed) return "";
    // Prefer first whitespace-separated token; fall back to handle-style.
    const word = trimmed.split(/[\s@]+/)[0] ?? "";
    if (!word) return "";
    // Skip generic personas like "anon", "guest", "user"
    if (/^(anon|guest|user|account|owner|admin)$/i.test(word)) return "";
    // Capitalize first letter of all-lowercase handles
    if (word === word.toLowerCase() && word.length > 1) {
      return word[0].toUpperCase() + word.slice(1);
    }
    return word;
  }

  let welcomeTitle = $derived(
    firstName ? `Welcome, ${firstName} 👋` : "Welcome to your workspace",
  );

  const tourSteps = $derived(
    !organizationSlug || !workspaceSlug
      ? []
      : [
          {
            placement: "center",
            eyebrow: "60-second tour",
            title: welcomeTitle,
            body:
              pmNeedsSetup || !pmTourKnown
                ? "Overview is the workspace home. Inbox is where agents wait on you, Agents shows what each one is doing, and Tasks and Docs hold the work."
                : "Overview is the workspace home. Inbox is where agents wait on you, Agents shows what each one is doing, and Tasks and Docs hold the work. PM is the conversation surface for decisions that need follow-through.",
            primaryLabel: "Take the tour →",
            skipLabel: "Maybe later",
          },
          {
            selector: '[data-tour="overview"]',
            eyebrow: "1 of 7 · Overview",
            title: "Overview is the workspace home",
            body: "What needs you, what is in flight, who is working, and which report is current. It links into those surfaces.",
          },
          {
            selector: '[data-tour="inbox"]',
            eyebrow: "2 of 7 · Inbox",
            title: "Inbox is the only attention surface",
            body: "Decisions that need an answer, blocked tasks, and items that require a response land here.",
          },
          {
            selector: '[data-tour="agents"]',
            eyebrow: "3 of 7 · Agents",
            title: "Agents shows who is doing what",
            body: "Every agent on your machines, grouped by working, waiting on you, idle and stale, with its task, last note and run time. Asks still get answered in the Inbox.",
          },
          {
            selector: '[data-tour="tasks"]',
            eyebrow: "4 of 7 · Tasks",
            title: "Tasks is the work board",
            body: "Table and board over the same records. Drag a task created here to change phase. A task that lives in another tracker opens a decision instead of mutating the source.",
          },
          {
            selector: '[data-tour="docs"]',
            eyebrow: "5 of 7 · Docs",
            title: "Docs is shared knowledge",
            body: "Versioned documents with first-class comments. Use them for meta knowledge and what other hosts cannot see.",
          },
          {
            selector: '[data-tour="pm"]',
            eyebrow: "6 of 7 · PM",
            title: !pmTourKnown
              ? "A PM answers about this workspace"
              : pmNeedsSetup
                ? "A PM is optional"
                : "PM is the conversation",
            body: !pmTourKnown
              ? "A PM agent answers questions about this workspace and proposes changes you approve. It runs on your own computer, through the agent you already use."
              : pmNeedsSetup
                ? "A PM agent answers questions about this workspace and proposes changes you approve. It runs on your own computer, through the agent you already use. Set one up whenever you want it."
                : "Ask what needs a decision, then follow the receipt. The PM runs on your computer with your chosen agent harness.",
          },
          {
            selector: '[data-tour="access"]',
            eyebrow: "7 of 7 · Access",
            title: "Last step: connect a machine",
            body: setupPrompt
              ? "Agents work here through the computer they run on. Copy one prompt, paste it into the agent you already use on that machine, and it installs anx, joins the machine to this workspace and reports back. Access also holds people and invites."
              : "Agents work here through the computer they run on. Set one up in Access → Hosts; every agent on that machine can then work in this workspace. Access also holds people and invites.",
            ctaLabel: setupPrompt
              ? "Copy the setup prompt →"
              : "Connect a machine →",
            ctaHref: ctaAccessHref,
            ctaAction: setupPrompt ? copySetupPrompt : undefined,
          },
        ],
  );

  function shouldOfferTourPath(/** @type {string} */ path) {
    return path === "/overview" || path === "/inbox";
  }

  function finishTour() {
    if (workspaceSlug) {
      markWorkspaceTourSeen(workspaceSlug);
    }
    tourOpen = false;
  }

  function onSpotlightClose() {
    finishTour();
  }

  async function evaluateEligibility() {
    if (!workspaceSlug) {
      return false;
    }
    if (isWorkspaceTourSeen(workspaceSlug)) {
      return false;
    }
    try {
      const res = await coreClient.listPrincipals({ limit: 50 });
      const principals = res?.principals ?? [];
      const active = principals.filter((p) => !p?.revoked);
      const hasAgent = active.some(
        (p) => String(p?.principal_kind ?? "").toLowerCase() === "agent",
      );
      if (active.length > 1) {
        return false;
      }
      if (hasAgent) {
        return false;
      }
      return true;
    } catch (e) {
      // A principal that may not list principals (an agent persona, a
      // read-limited grant) is not a lone new human; no welcome for them.
      const status = Number(e?.status ?? e?.coreHttpStatus ?? 0);
      if (status === 401 || status === 403) {
        return false;
      }
      console.warn(
        "[WorkspaceTour] listPrincipals failed; showing tour (solo-workspace assumption)",
        e,
      );
      return true;
    }
  }

  $effect(() => {
    if (!browser || !workspaceSlug || !devActorModeReady) {
      return;
    }
    if (isWorkspaceTourSeen(workspaceSlug)) {
      return;
    }

    let cancelled = false;
    void (async () => {
      eligibilityLoading = true;
      const ok = await evaluateEligibility();
      if (cancelled) return;
      eligible = ok;
      eligibilityLoading = false;
    })();

    return () => {
      cancelled = true;
    };
  });

  $effect(() => {
    if (
      !browser ||
      !workspaceSlug ||
      !devActorModeReady ||
      !eligible ||
      eligibilityLoading
    ) {
      return;
    }
    if (tourOpen) {
      return;
    }
    if (isWorkspaceTourSeen(workspaceSlug)) {
      return;
    }

    if (!shouldOfferTourPath(relPath)) {
      const dest = workspacePath(organizationSlug, workspaceSlug, "/overview");
      void goto(dest, { replaceState: true, noScroll: false });
      return;
    }

    pathWhenOpened = $page.url.pathname;
    tourOpen = true;
  });

  $effect(() => {
    if (!tourOpen) {
      return;
    }
    const path = $page.url.pathname;
    if (pathWhenOpened && path !== pathWhenOpened) {
      finishTour();
    }
  });

  // Replay-on-demand: the Home page exposes a "Take the tour" button that
  // bumps replayTourSignal. Force the tour open regardless of the
  // workspaceTourSeen flag. Steps anchor to the primary nav, starting
  // at Overview.
  let lastReplaySignal = $state(0);
  let pendingReplay = $state(false);

  $effect(() => {
    if (!browser) return;
    const current = $replayTourSignal;
    if (current === lastReplaySignal) return;
    lastReplaySignal = current;
    if (!workspaceSlug || !organizationSlug) return;
    if (tourOpen) return;
    pendingReplay = true;
    if (!shouldOfferTourPath(relPath)) {
      const dest = workspacePath(organizationSlug, workspaceSlug, "/overview");
      void goto(dest, { replaceState: false, noScroll: false });
    }
  });

  $effect(() => {
    if (!browser) return;
    if (!pendingReplay) return;
    if (!shouldOfferTourPath(relPath)) return;
    if (tourOpen) {
      pendingReplay = false;
      return;
    }
    pendingReplay = false;
    pathWhenOpened = $page.url.pathname;
    tourOpen = true;
  });
</script>

{#if tourOpen && tourSteps.length > 0}
  <SpotlightTour
    bind:open={tourOpen}
    onClose={onSpotlightClose}
    onStep={onTourStep}
    steps={tourSteps}
  />
{/if}
