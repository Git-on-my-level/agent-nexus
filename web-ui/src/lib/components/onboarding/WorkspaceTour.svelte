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
            body: "Inbox, Tasks, and Docs are the three places you work. PM is the conversation surface for decisions that need follow-through.",
            primaryLabel: "Take the tour →",
            skipLabel: "Maybe later",
          },
          {
            selector: '[data-tour="inbox"]',
            eyebrow: "1 of 5 · Inbox",
            title: "Inbox is the only attention surface",
            body: "Decisions that need an answer, blocked tasks, and items that require a response land here.",
          },
          {
            selector: '[data-tour="tasks"]',
            eyebrow: "2 of 5 · Tasks",
            title: "Tasks is the work board",
            body: "Table and board over the same records. Drag a Nexus-owned card to change phase. Source-owned cards open a decision instead of mutating the source.",
          },
          {
            selector: '[data-tour="docs"]',
            eyebrow: "3 of 5 · Docs",
            title: "Docs is shared knowledge",
            body: "Versioned documents with first-class comments. Use them for meta knowledge and what other hosts cannot see.",
          },
          {
            selector: '[data-tour="pm"]',
            eyebrow: "4 of 5 · PM",
            title: "PM is the conversation",
            body: "Ask what needs a decision, then follow the receipt. The PM runs through the existing agent harnesses.",
          },
          {
            selector: '[data-tour="access"]',
            eyebrow: "5 of 5 · Access",
            title: "Connect the first agent",
            body: "Invite an agent or teammate. Settings also holds Secrets, Integrations, and Audit.",
            ctaLabel: "Connect your first agent →",
            ctaHref: ctaAccessHref,
          },
        ],
  );

  function shouldOfferTourPath(/** @type {string} */ path) {
    return path === "/inbox";
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
      const dest = workspacePath(organizationSlug, workspaceSlug, "/inbox");
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
  // workspaceTourSeen flag. The tour is anchored to Inbox landmarks.
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
      const dest = workspacePath(organizationSlug, workspaceSlug, "/inbox");
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
