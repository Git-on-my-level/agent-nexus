<script>
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  import SpotlightTour from "$lib/components/onboarding/SpotlightTour.svelte";
  import { coreClient } from "$lib/coreClient";
  import {
    isWorkspaceTourSeen,
    markWorkspaceTourSeen,
    replayTourSignal,
  } from "$lib/tourState";
  import { stripWorkspacePath, workspacePath } from "$lib/workspacePaths";

  let {
    organizationSlug = "",
    workspaceSlug = "",
    devActorModeReady = false,
    /** Optional first-name / display label used to personalize the welcome */
    userLabel = "",
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
      ? `${workspacePath(organizationSlug, workspaceSlug, "/access")}?invite=agent&from=tour`
      : "/access?invite=agent&from=tour",
  );

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
            body: "This is your control plane for delegating work to AI agents. We'll show you the map in 6 stops — then connect your first agent so the workspace comes alive.",
            primaryLabel: "Take the tour →",
            skipLabel: "Maybe later",
          },
          {
            selector: '[data-tour="home"]',
            eyebrow: "1 of 6 · Home",
            title: "Home is your situation room",
            body: "A live snapshot of what your agents are doing, what's blocked, and what just shipped — so you walk in and know where to look.",
          },
          {
            selector: '[data-tour="inbox"]',
            eyebrow: "2 of 6 · Inbox",
            title: "Inbox is where agents tap you in",
            body: "When an agent gets stuck, needs a decision, or finishes work, it lands here. Think of it as the one place to triage agent output.",
          },
          {
            selector: '[data-tour="topics"]',
            eyebrow: "3 of 6 · Topics",
            title: "Topics keep work on rails",
            body: "Every long-running thread of work — a feature, a customer, an investigation — gets its own topic. Agents and humans collaborate against shared context, not loose chats.",
          },
          {
            selector: '[data-tour="boards"]',
            eyebrow: "4 of 6 · Boards",
            title: "Boards are the views you'll live in",
            body: 'Group related topics into the dashboards you actually open every day — "Customer X", "This week", "Stuck on me". You build them, agents respect them.',
          },
          {
            selector: '[data-tour="docs"]',
            eyebrow: "5 of 6 · Docs",
            title: "Docs your agents can read and write",
            body: "Briefs, specs, runbooks — the durable knowledge your agents reach for. Edit them like a wiki; agents will cite them in their work.",
          },
          {
            selector: '[data-tour="access"]',
            eyebrow: "6 of 6 · Access",
            title: "This is where the workspace wakes up",
            body: "Right now you're the only principal here. Invite an agent (or a teammate) and the rest of the workspace lights up — Inbox starts catching real work, Topics fill in, Boards earn their keep.",
            ctaLabel: "Connect your first agent →",
            ctaHref: ctaAccessHref,
          },
        ],
  );

  function shouldOfferTourPath(/** @type {string} */ path) {
    return path === "/";
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
      const dest = workspacePath(organizationSlug, workspaceSlug, "/");
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
  // workspaceTourSeen flag. The tour is anchored to landmarks that only
  // render on Home, so navigate there first if needed, then open once the
  // path settles.
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
      const dest = workspacePath(organizationSlug, workspaceSlug, "/");
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
    steps={tourSteps}
  />
{/if}
