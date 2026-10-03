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
      ? `${workspacePath(organizationSlug, workspaceSlug, "/access")}?from=tour#hosts`
      : "/access?from=tour#hosts",
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
            body: "Inbox is where agents wait on you, Agents shows what each one is doing, and Tasks and Docs hold the work. PM is the conversation surface for decisions that need follow-through.",
            primaryLabel: "Take the tour →",
            skipLabel: "Maybe later",
          },
          {
            selector: '[data-tour="inbox"]',
            eyebrow: "1 of 6 · Inbox",
            title: "Inbox is the only attention surface",
            body: "Decisions that need an answer, blocked tasks, and items that require a response land here.",
          },
          {
            selector: '[data-tour="agents"]',
            eyebrow: "2 of 6 · Agents",
            title: "Agents shows who is doing what",
            body: "Every agent on your machines, grouped by working, waiting on you, idle and stale, with its task, last note and run time. Asks still get answered in the Inbox.",
          },
          {
            selector: '[data-tour="tasks"]',
            eyebrow: "3 of 6 · Tasks",
            title: "Tasks is the work board",
            body: "Table and board over the same records. Drag a task created here to change phase. A task that lives in another tracker opens a decision instead of mutating the source.",
          },
          {
            selector: '[data-tour="docs"]',
            eyebrow: "4 of 6 · Docs",
            title: "Docs is shared knowledge",
            body: "Versioned documents with first-class comments. Use them for meta knowledge and what other hosts cannot see.",
          },
          {
            selector: '[data-tour="pm"]',
            eyebrow: "5 of 6 · PM",
            title: "PM is the conversation",
            body: "Ask what needs a decision, then follow the receipt. The PM runs through the existing agent harnesses.",
          },
          {
            selector: '[data-tour="access"]',
            eyebrow: "6 of 6 · Access",
            title: "Enroll the machine your agents run on",
            body: "Run anx host enroll on it once and approve it here; every agent on that machine can then work in this workspace. Access also holds people and invites.",
            ctaLabel: "Enroll a machine →",
            ctaHref: ctaAccessHref,
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
    steps={tourSteps}
  />
{/if}
