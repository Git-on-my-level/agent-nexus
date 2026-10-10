<script>
  import { onMount, tick, untrack } from "svelte";
  import { page } from "$app/stores";
  import { beforeNavigate, goto } from "$app/navigation";
  import { coreClient } from "$lib/coreClient";
  import { initializeAuthSession } from "$lib/authSession";
  import { restartSession } from "$lib/workspaceBootstrap";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import Time from "$lib/time/Time.svelte";
  import { resolveRefLink } from "$lib/refLinkModel.js";
  import {
    decisionTitle,
    errorMessage,
    isSessionExpired,
    receiptSignal,
  } from "$lib/pm/presentation.js";
  import { decisionIdsFromTurn } from "$lib/pm/turnDecisions.js";
  import {
    candidateDecisionIdsFromTurn,
    clockTime,
    conversationHeading,
    evidenceRefsForTurn,
    hasPendingTurn,
    isNearBottom,
    linkifyDecisionIds,
    proposedDecisionIds,
    startsTimeGroup,
    turnState,
    EXPECTED_WAIT_LABEL,
    QUEUED_LABEL,
    UNCLAIMED_LABEL,
    WORKING_LABEL,
  } from "$lib/pm/chatModel.js";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import SignalBadge from "$lib/components/pm/SignalBadge.svelte";
  import ReceiptSignal from "$lib/components/pm/ReceiptSignal.svelte";
  import AnxRefChip from "$lib/components/AnxRefChip.svelte";
  import {
    collectPageRefs,
    resolveRefsInBatches,
    keepReadableRefs,
  } from "$lib/refResolve.js";
  import { pinnedRefs, activityLabel } from "$lib/pm/context.js";
  import RefChip from "$lib/components/RefChip.svelte";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import PmStatusBadge from "$lib/components/pm/PmStatusBadge.svelte";
  import {
    PM_STATES,
    isPmNotOnboardedRefusal,
    pmOffline,
    pmSetupOffered,
  } from "$lib/pm/onboardingState.js";
  import {
    pmPresence,
    publishPmPresence,
    refreshPmPresence,
  } from "$lib/pm/presence.js";

  let conversations = $state([]),
    conversation = $state(null),
    turns = $state([]),
    loading = $state(true),
    sending = $state(false),
    ready = $state(false),
    error = $state(""),
    // The last error came from a send, so "try again" means send again.
    errorFromSend = $state(false),
    draft = $state(""),
    partial = $state(false),
    historyOpen = $state(false);
  let decisionRecords = $state({});
  let decisionError = $state("");
  let conversationsCursor = $state("");
  let turnsCursor = $state("");
  let loadingOlder = $state(false);
  let loadingConversations = $state(false);
  let now = $state(Date.now());
  let reducedMotion = $state(false);
  let atBottom = $state(true);
  /** Stricter than `atBottom`; see `isParkedAtBottom`. */
  let pinnedToBottom = $state(false);
  let threadElement = $state(null);
  let threadInnerElement = $state(null);
  let composerElement = $state(null);
  let olderLoaded = false;
  let creationKey, requestKey, requestText, createdConversationId;
  // Conversations this tab created whose first message was refused: they
  // exist at core, but nothing was said in them, so History skips them until
  // a message lands (the retry still uses the same conversation).
  let unsentConversations = $state(new Set());
  let requestId = 0;
  let decisionFetch = 0;
  let pollInFlight = false;
  let anchored = false;
  const pendingDecisions = new Set();
  const retryKeys = new Map();
  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  // The resolved slug the shell keys PM state by, not the URL segment.
  let workspaceSlug = $derived($page.data?.workspace?.slug ?? "");
  let pmState = $derived(
    $pmPresence.workspace === workspaceSlug ? $pmPresence : null,
  );
  let pmIsOffline = $derived(pmOffline(pmState));
  let selectedId = $derived($page.url.searchParams.get("conversation") || "");
  let workRef = $derived($page.url.searchParams.get("work_ref") || "");
  let selectedKey = $derived(`${selectedId}\n${workRef}`);
  let contextRefs = $derived(pinnedRefs(conversation, $page.url.searchParams));
  let resolvedRefs = $state(new Map());
  let refRequest = 0;
  let refsError = $state("");
  let pageRefs = $derived(
    collectPageRefs(
      turns.flatMap((turn) => [turn.response, turn.partial_response]),
      {
        extraRefs: [
          ...contextRefs,
          ...turns.flatMap((turn) => evidenceRefsForTurn(turn)),
          ...Object.values(decisionRecords).map((record) => record.work_ref),
          ...conversations.flatMap((item) => pinnedRefs(item)),
        ],
      },
    ),
  );
  $effect(() => {
    const refs = pageRefs;
    if (!ready) return;
    const id = ++refRequest;
    refsError = "";
    if (!refs.length) {
      resolvedRefs = new Map();
      return;
    }
    resolveRefsInBatches(refs, (batch) => coreClient.resolveRefs(batch))
      .then((result) => {
        if (id === refRequest) {
          resolvedRefs = keepReadableRefs(resolvedRefs, result);
          if ([...result.values()].some((row) => row.unreadable))
            refsError = "Context titles could not be loaded.";
        }
      })
      .catch(() => {
        if (id === refRequest)
          refsError = "Context titles could not be loaded.";
      });
  });
  let turnDecisionIds = $derived(
    turns.flatMap((turn) => decisionIdsFromTurn(turn)),
  );
  // Ids the PM names in prose without a `decision:` prefix. They are guesses
  // until core answers for them, so a failed lookup is silent.
  let turnCandidateDecisionIds = $derived(
    turns.flatMap((turn) => candidateDecisionIdsFromTurn(turn)),
  );
  let waiting = $derived(hasPendingTurn(turns, now));
  // "Your draft stays in the composer" holds across navigation too: the
  // draft is kept per conversation for this tab until it is sent.
  function draftStorageKey(scope) {
    return `anx.pm.draft:${$page.params.workspace}:${String(scope ?? "").split("\n")[0] || "new"}`;
  }
  function readDraft(scope) {
    try {
      return sessionStorage.getItem(draftStorageKey(scope)) || "";
    } catch {
      return "";
    }
  }
  $effect(() => {
    const key = draftStorageKey(conversationScope);
    const text = draft;
    try {
      if (text.trim()) sessionStorage.setItem(key, text);
      else sessionStorage.removeItem(key);
    } catch {
      // Storage may be unavailable; the draft still lives in the composer.
    }
  });
  let sessionExpired = $state(false);
  function signInAgain() {
    restartSession({
      organizationSlug: $page.params.organization,
      workspaceSlug: $page.params.workspace,
      hostedMode: $page.data?.shellCapabilities?.mode === "hosted",
      workspaceId: $page.data?.workspace?.workspaceId,
      currentAppPath: "/pm",
      search: $page.url.search,
    });
  }
  // Task titles for the refs the PM names; a raw card ref is not a label.
  function workTitle(ref) {
    return resolvedRefs.get(String(ref ?? ""))?.title || "";
  }
  let showJump = $derived(Boolean(turns.length) && !atBottom);
  /*
   * The heading. Core names a conversation from its first question, so for
   * most conversations the "title" is that question cut at 100 characters —
   * printed under the header it read as stray repeated text. `chatModel`
   * decides which titles are real; the rest leave the page named "Ask PM"
   * and let the first bubble be the question.
   */
  let heading = $derived(
    conversationHeading(conversation, turns, {
      // A non-empty cursor means older turns are unloaded, so `turns[0]` is
      // not the first question and the echo cannot be detected.
      hasOlderTurns: Boolean(turnsCursor),
    }),
  );
  let headingTitle = $derived(heading || "Ask PM");
  /** Nothing asked yet: the one screen with room for the explainer. */
  let isNewConversation = $derived(!loading && !turns.length);
  /**
   * How many source chips a reply shows before the rest go behind a `+N`.
   * Three fits one line at 390px, which is what keeps the row from wrapping
   * into the composer.
   */
  const SOURCE_PREVIEW = 3;
  // Reassigned rather than mutated, the way `unsentConversations` is: a
  // plain `$state` Set does not notify on `add`.
  let expandedSources = $state(new Set());
  function showAllSources(turnId) {
    expandedSources = new Set([...expandedSources, turnId]);
  }

  beforeNavigate(({ cancel, type }) => {
    if (sending) {
      cancel();
      return;
    }
    // Full-page unloads are covered by onbeforeunload.
    if (type === "leave") return;
    if (
      draft.trim() &&
      !sending &&
      !window.confirm("Leave with an unsent PM message?")
    )
      cancel();
  });

  let conversationScope;
  $effect(() => {
    const key = selectedKey;
    if (!ready) return;
    // Only the conversation key may re-run this: reading the draft here made
    // every restored draft fetch the conversation a second time.
    untrack(() => {
      const firstRun = conversationScope === undefined;
      const switched = !firstRun && conversationScope !== key;
      conversationScope = key;
      historyOpen = false;
      // A reload or deep link lands here once with an empty composer; the
      // draft promised to the reader is in this tab's storage.
      if (firstRun && !draft.trim()) draft = readDraft(key);
      if (switched) {
        createdConversationId = "";
        creationKey = "";
        requestKey = "";
        requestText = "";
        draft = readDraft(key);
        anchored = false;
      }
      void loadConversation(key.split("\n")[0]);
    });
  });
  $effect(() => {
    void loadDecisionRecords(turnDecisionIds);
  });
  $effect(() => {
    void loadDecisionRecords(turnCandidateDecisionIds, true);
  });
  // A pending turn shows its own elapsed wait; nothing else needs a clock.
  $effect(() => {
    if (!waiting) return;
    const timer = setInterval(() => {
      now = Date.now();
    }, 1000);
    return () => clearInterval(timer);
  });
  /*
   * An offline PM's "last seen" also ages on screen, far more slowly, and
   * outlives any pending turn — without this it froze at whatever it read
   * when the page opened.
   */
  $effect(() => {
    if (waiting || !pmIsOffline) return;
    const timer = setInterval(() => {
      now = Date.now();
    }, 30_000);
    return () => clearInterval(timer);
  });
  // Auto-grow: reset then measure, capped so the thread keeps most of the height.
  $effect(() => {
    const value = draft;
    const element = composerElement;
    if (!element) return;
    void value;
    element.style.height = "auto";
    const measured = Number(element.scrollHeight) || 0;
    if (measured > 0) element.style.height = `${Math.min(measured, 200)}px`;
  });
  // Stick to the newest turn while the reader is already at the bottom. Only a
  // new turn (an answer arriving on one, or its proposals resolving underneath
  // it) may move the view — the reader's own scroll position is read untracked
  // so scrolling never re-pins them.
  $effect(() => {
    const signature = turns
      .map(
        (turn) =>
          `${turn.id}:${turn.response ? 1 : 0}:${
            proposedDecisionIds(turn, decisionRecords).length
          }`,
      )
      .join("|");
    const element = threadElement;
    if (!element || !signature) return;
    untrack(() => {
      if (loadingOlder) return;
      if (!anchored) {
        anchored = true;
        void tick().then(() => scrollToLatest("auto"));
        return;
      }
      if (!atBottom) return;
      void tick().then(() => scrollToLatest(reducedMotion ? "auto" : "smooth"));
    });
  });

  /*
   * Re-pin when the thread grows under the reader.
   *
   * The effect above keys on turn ids, answers and proposal counts, so it
   * does not fire when a turn that is already on screen gets *taller* — and
   * the last turn does get taller: its source chips are `RefChip`s whose
   * labels arrive with `resolveRefs`, a round trip after the turn rendered.
   * A row that wraps at that moment lands under the composer, and because
   * the page still believes it is at the bottom it does not even offer
   * "Jump to latest". This is the growth nobody asked for, taken care of.
   */
  $effect(() => {
    const inner = threadInnerElement;
    if (!inner || typeof ResizeObserver === "undefined") return;
    let last = inner.getBoundingClientRect().height;
    const observer = new ResizeObserver(() => {
      const height = inner.getBoundingClientRect().height;
      const grew = height > last + 1;
      last = height;
      /*
       * Only downward, only for a reader genuinely parked at the end of a
       * thread that already scrolled, and never mid-prepend — `olderTurns`
       * restores its own position.
       *
       * `atBottom` is too loose to use here: `isNearBottom` tolerates 64px
       * and calls a thread that does not scroll at all "at the bottom". On
       * both of those, opening a run log on a short conversation would have
       * scrolled the thing the reader just opened off the top.
       */
      if (!grew) return;
      untrack(() => {
        if (!pinnedToBottom || loadingOlder) return;
        scrollToLatest("auto");
      });
    });
    observer.observe(inner);
    return () => observer.disconnect();
  });

  function scrollToLatest(behavior = "auto") {
    const element = threadElement;
    if (!element) return;
    if (typeof element.scrollTo === "function")
      element.scrollTo({ top: element.scrollHeight, behavior });
    else element.scrollTop = element.scrollHeight;
    atBottom = true;
    // `scrollTo` with `behavior: "smooth"` lands later, so read the state
    // the caller just asked for rather than the one on screen.
    pinnedToBottom = true;
  }
  function onThreadScroll() {
    atBottom = isNearBottom(threadElement);
    pinnedToBottom = isParkedAtBottom(threadElement);
  }
  /**
   * Parked at the very end of a thread that actually scrolls — the only
   * state in which growing the thread should move the reader.
   */
  function isParkedAtBottom(element) {
    if (!element) return false;
    const slack = element.scrollHeight - element.clientHeight;
    if (slack <= 1) return false;
    return slack - element.scrollTop <= 2;
  }

  async function loadList(append = false) {
    loadingConversations = true;
    try {
      const result = await coreClient.listPmConversations({
        limit: 50,
        cursor: append ? conversationsCursor : undefined,
      });
      conversations = [
        ...new Map(
          [...(append ? conversations : []), ...(result.items || [])].map(
            (item) => [item.id, item],
          ),
        ).values(),
      ];
      conversationsCursor = result.next_cursor || "";
      partial = Boolean(result.has_more);
    } finally {
      loadingConversations = false;
    }
  }
  async function moreConversations() {
    try {
      await loadList(true);
    } catch (err) {
      error = errorMessage(err);
    }
  }
  async function olderTurns() {
    if (!selectedId || !turnsCursor || loadingOlder) return;
    const ticket = requestId;
    const element = threadElement;
    const previousHeight = element?.scrollHeight ?? 0;
    const previousTop = element?.scrollTop ?? 0;
    loadingOlder = true;
    try {
      const result = await coreClient.getPmConversation(selectedId, {
        limit: 100,
        cursor: turnsCursor,
      });
      if (ticket !== requestId) return;
      turns = [
        ...new Map(
          [...(result.turns || []), ...turns].map((turn) => [turn.id, turn]),
        ).values(),
      ];
      turnsCursor = result.next_cursor || "";
      olderLoaded = true;
      // Prepending must not move the reader: restore by the height the
      // prepended turns added.
      await tick();
      if (element)
        element.scrollTop =
          previousTop + (element.scrollHeight - previousHeight);
    } catch (err) {
      if (ticket === requestId) error = errorMessage(err);
    } finally {
      loadingOlder = false;
    }
  }
  async function loadDecisionRecords(ids, speculative = false) {
    const ticket = decisionFetch;
    const missing = ids.filter(
      (id) => id && !(id in decisionRecords) && !pendingDecisions.has(id),
    );
    if (!missing.length) return;
    for (const id of missing) pendingDecisions.add(id);
    await Promise.all(
      missing.map(async (id) => {
        try {
          const record = await coreClient.getPmDecision(id);
          if (ticket === decisionFetch) decisionRecords[id] = record;
        } catch (err) {
          if (ticket === decisionFetch) {
            decisionRecords[id] = null;
            // A bare id in prose need not be a decision this actor can read;
            // that lookup failing is expected and says nothing to the reader.
            if (!speculative) decisionError = errorMessage(err);
          }
        } finally {
          pendingDecisions.delete(id);
        }
      }),
    );
  }
  async function loadConversation(id = selectedId, quiet = false) {
    const ticket = ++requestId;
    if (!quiet) {
      loading = true;
      conversation = null;
      turns = [];
      turnsCursor = "";
      olderLoaded = false;
      anchored = false;
    }
    // A quiet poll refreshes turns; it must not erase an error the reader
    // has not yet seen (a "busy" refusal, for one).
    if (!quiet) {
      error = "";
      errorFromSend = false;
    }
    try {
      if (id) {
        const result = await coreClient.getPmConversation(id, { limit: 100 });
        if (ticket !== requestId) return;
        conversation = result.conversation;
        turns = quiet
          ? [
              ...new Map(
                [...turns, ...(result.turns || [])].map((turn) => [
                  turn.id,
                  turn,
                ]),
              ).values(),
            ]
          : result.turns || [];
        if (!quiet || !olderLoaded) turnsCursor = result.next_cursor || "";
      }
    } catch (err) {
      if (ticket === requestId) {
        // The PM was uninstalled while this thread was open: core refuses
        // every call from here on, so leave for setup rather than re-raising
        // the same alert on each poll.
        if (await redirectIfPmGone(err)) return;
        error = errorMessage(err);
        // An expired session will not fix itself; stop polling and offer
        // sign-in instead of re-raising the same alert every few seconds.
        if (isSessionExpired(err)) sessionExpired = true;
      }
    } finally {
      if (ticket === requestId) loading = false;
    }
  }
  async function initialize() {
    loading = true;
    error = "";
    try {
      await initializeAuthSession({
        fetchFn: globalThis.fetch.bind(globalThis),
        workspaceSlug: $page.params.workspace,
        authDriver: "pm-conversation",
      });
      /*
       * A PM agent runs on the reader's computer, so this surface can be
       * reached (a bookmark, a stale tab) with no PM at all. Core would
       * refuse every call; send them to setup instead of to an error.
       */
      const { presence } = await refreshPmPresence(workspaceSlug, {
        force: true,
      });
      if (pmSetupOffered(presence)) {
        await goToSetup();
        return;
      }
      await loadList();

      ready = true;
      // No conversation in the URL means an empty thread; History lists prior
      // threads without implying one is open. Send creates or continues from
      // the URL (?conversation=, ?work_ref=, or ?new=1).
      if (!selectedId) loading = false;
    } catch (err) {
      if (await redirectIfPmGone(err)) return;
      error = errorMessage(err);
      loading = false;
    }
  }

  /** Leave for setup, without keeping a dead PM page in the back history. */
  async function goToSetup() {
    loading = false;
    await goto(workspaceHref("/pm/setup"), { replaceState: true });
  }

  /**
   * Core refused because there is no PM to serve this. Record that and move
   * the reader to setup; retrying the same call cannot repair it.
   */
  async function redirectIfPmGone(err) {
    if (!isPmNotOnboardedRefusal(err)) return false;
    publishPmPresence(workspaceSlug, PM_STATES.NOT_ONBOARDED);
    await goToSetup();
    return true;
  }

  async function send(event) {
    event?.preventDefault?.();
    const pointerSend =
      event?.type === "submit" && event.submitter instanceof HTMLElement;
    const text = draft.trim();
    if (!text || sending || !ready) return;
    sending = true;
    error = "";
    errorFromSend = false;
    if (!requestKey || text !== requestText) {
      requestKey = crypto.randomUUID();
      requestText = text;
    }
    try {
      let id = selectedId || createdConversationId;
      if (!id) {
        creationKey ||= crypto.randomUUID();
        const result = await coreClient.createPmConversation({
          request_key: creationKey,
          title: text.slice(0, 100),
          ...(workRef ? { work_ref: workRef } : {}),
          ...(contextRefs.length ? { context_refs: contextRefs } : {}),
        });
        if (!result.id)
          throw new Error(
            "Conversation creation did not return an identifier. Retry with the same request.",
          );
        id = result.id;
        createdConversationId = id;
        unsentConversations = new Set([...unsentConversations, id]);
      }
      const turn = await coreClient.sendPmMessage(id, {
        request_key: requestKey,
        text,
      });
      if (!turn.id)
        throw new Error(
          "The message receipt is incomplete. Your draft is retained; retry uses the same request key.",
        );
      draft = "";
      requestKey = "";
      requestText = "";
      if (unsentConversations.has(id)) {
        const next = new Set(unsentConversations);
        next.delete(id);
        unsentConversations = next;
      }
      await loadList();
      if (selectedId !== id) {
        sending = false;
        await goto(workspaceHref(`/pm?conversation=${encodeURIComponent(id)}`));
      } else await loadConversation(id, true);
    } catch (err) {
      if (await redirectIfPmGone(err)) return;
      error = errorMessage(err);
      errorFromSend = true;
    } finally {
      sending = false;
      // Keyboard send keeps the caret in the composer; pointer send should not
      // yank focus back after the operator clicked Send.
      if (!pointerSend) {
        void tick().then(() => composerElement?.focus());
      }
    }
  }
  /**
   * Resend a turn the PM never answered. The key is stable per turn, so
   * repeated retries of the same message stay one intent at core.
   */
  async function retryTurn(turn) {
    const text = String(turn?.text ?? "").trim();
    if (!text || sending || !ready) return;
    if (!retryKeys.has(turn.id)) retryKeys.set(turn.id, crypto.randomUUID());
    requestKey = retryKeys.get(turn.id);
    requestText = text;
    draft = text;
    await send();
  }
  function onComposerKeydown(event) {
    if (event.key !== "Enter" || event.isComposing || event.shiftKey) return;
    event.preventDefault();
    void send();
  }
  function useSuggestion(prompt) {
    draft = prompt;
    document.getElementById("pm-message")?.focus();
  }
  /**
   * Evidence chips reuse the shared ref model for labels and hrefs; `card:` and
   * `work:` keep the task route this page has always used.
   */
  function evidenceLink(ref) {
    const resolved = resolveRefLink(ref, {
      organizationSlug: $page.params.organization,
      workspaceSlug: $page.params.workspace,
    });
    if (ref.startsWith("card:") || ref.startsWith("work:"))
      return {
        ...resolved,
        label: workTitle(ref) || resolved.label,
        href: workspaceHref(`/tasks/${encodeURIComponent(ref)}`),
        isExternal: false,
      };
    return resolved;
  }
  function decisionInboxHref(id) {
    return workspaceHref(`/inbox?item=decision:${encodeURIComponent(id)}`);
  }
  // The PM names its proposals as bare ids mid-sentence. Every id that resolved
  // to a decision becomes a short link to the Inbox item that answers it; an id
  // that did not resolve stays as written.
  function answerBody(response, proposed) {
    const answerable = new Set(proposed);
    void answerable;
    return linkifyDecisionIds(response, (id) => decisionInboxHref(id));
  }
  const prompts = [
    "What needs my decision?",
    "What changed since I last checked?",
    "Which commitments are blocked, and who acts next?",
    "Where is the evidence still uncertain?",
  ];
  const RUNBOOK_HREF =
    "https://github.com/Git-on-my-level/agent-nexus/blob/main/cli/docs/runbook.md#pm-runner-anx-pm-serve";
  onMount(() => {
    void initialize();
    const motion = globalThis.matchMedia?.("(prefers-reduced-motion: reduce)");
    reducedMotion = Boolean(motion?.matches);
    const onMotionChange = (event) => {
      reducedMotion = Boolean(event.matches);
    };
    motion?.addEventListener?.("change", onMotionChange);
    // A pending turn resolves in the background whether or not this tab is in
    // front: keep polling while hidden, and refresh the moment it comes back.
    const pollNow = (force = false) => {
      if (!selectedId || sending || loading || loadingOlder || pollInFlight)
        return;
      // Nothing changes on its own once every turn is answered or failed;
      // only a pending turn earns a poll.
      // A conversation is shared with the CLI and channels: poll fast while a
      // turn is pending, slowly (every sixth tick) when nothing is.
      idleTicks = waiting ? 0 : idleTicks + 1;
      if (!force && !waiting && idleTicks % 6 !== 0) return;
      if (sessionExpired) return;
      pollInFlight = true;
      void loadConversation(selectedId, true).finally(() => {
        pollInFlight = false;
      });
    };
    const onVisibilityChange = () => {
      if (!document.hidden) pollNow(true);
    };
    document.addEventListener("visibilitychange", onVisibilityChange);
    let idleTicks = 0;
    const timer = setInterval(pollNow, 5000);
    return () => {
      requestId++;
      decisionFetch++;
      motion?.removeEventListener?.("change", onMotionChange);
      document.removeEventListener("visibilitychange", onVisibilityChange);
      clearInterval(timer);
    };
  });
</script>

{#snippet runningLine(view, turn)}
  <!--
    Inline, so a `<summary>` keeps its disclosure marker: the steps are
    behind this line, and a reader has to be able to see that they are.
  -->
  <span class="pm-status-line">
    {#if reducedMotion}
      <span>{view.claimed ? WORKING_LABEL : `${QUEUED_LABEL}…`}</span>
    {:else}
      <span class="pm-dots" aria-hidden="true"><i></i><i></i><i></i></span>
      <!--
        "Working", not the last step's own label. The steps are listed inside
        this fold, so borrowing one of their names for the summary printed
        the same word twice on one answer.
      -->
      <span>{view.claimed ? WORKING_LABEL : QUEUED_LABEL}</span>
      {#if view.elapsed}<span aria-hidden="true">· {view.elapsed}</span>{/if}
    {/if}
    {#if turn.activity?.length}<span class="sr-only"
        >{turn.activity.length} steps</span
      >{/if}
  </span>
{/snippet}

{#snippet runSteps(turn)}
  <ol class="pm-run-steps" aria-label="Turn activity">
    {#each turn.activity as event (event.sequence)}
      <li>
        {event.label}{#if event.target}
          · {event.target}{/if}
      </li>
    {/each}
  </ol>
{/snippet}

<svelte:window
  onbeforeunload={(event) => {
    if (draft.trim()) {
      event.preventDefault();
      event.returnValue = "";
    }
  }}
/>
<svelte:head><title>PM · Agent Nexus</title></svelte:head>
<WorkspacePageShell class="pm-page">
  <div class="pm-head">
    <!--
      Three lines of meta became one. The heading is the conversation when
      the conversation has a name of its own, with "Ask PM" as the eyebrow
      above it; the status is a dot and a word on the heading's baseline,
      with the runner and host in its tooltip; and the sentence about where
      a proposal lands is kept for the one screen that has room for it and
      a reader who has not seen it — a new, empty conversation.
    -->
    <WorkspacePageHeader title={headingTitle} clamp={Boolean(heading)}>
      {#snippet eyebrow()}{#if heading}Ask PM{/if}{/snippet}
      {#snippet meta()}<PmStatusBadge
          presence={pmState}
          {now}
          manageHref={workspaceHref("/pm/setup")}
        />{/snippet}
      {#snippet subtitle()}{#if isNewConversation}Ask about your tasks. If the
          PM proposes a change, you approve it in Inbox.{/if}{/snippet}
      {#snippet actions()}
        <details
          class="pm-history"
          bind:open={historyOpen}
          aria-label="PM conversations"
        >
          <summary class="ui-btn-secondary list-none"
            >History{#if conversations.length && !partial}<span
                class="ml-1 text-fg-muted">{conversations.length}</span
              >{/if}</summary
          >
          <!--
            Only while it is open. A closed `<details>` still lays its
            contents out — every conversation row, every live clock in them —
            and they are measured by anything that walks the page, which is
            how a popover nobody opened started reporting itself as clipped
            off the side of the screen.
          -->
          {#if historyOpen}
            <div class="pm-history-panel" role="presentation">
              <nav aria-label="Conversation history">
                {#each conversations.filter((item) => item.id === selectedId || !unsentConversations.has(item.id)) as item (item.id)}
                  <a
                    class="pm-history-item {item.id === selectedId
                      ? 'pm-history-item--active'
                      : ''}"
                    href={workspaceHref(
                      `/pm?conversation=${encodeURIComponent(item.id)}`,
                    )}
                    aria-current={item.id === selectedId ? "page" : undefined}
                  >
                    <span class="line-clamp-1 break-words">{item.title}</span>
                    <span class="text-micro text-fg-subtle">
                      {#each pinnedRefs(item) as ref (ref)}
                        <span class="block"
                          >{workTitle(ref) || ref}
                          {#if resolvedRefs.get(ref)?.status}
                            · {resolvedRefs
                              .get(ref)
                              .status.replaceAll("_", " ")}{/if}
                          {#if resolvedRefs.get(ref)?.lastMovedAt}
                            · <Time
                              value={resolvedRefs.get(ref).lastMovedAt}
                              {now}
                            />{/if}
                        </span>
                      {/each}
                      <Time value={item.created_at} {now} /></span
                    >
                  </a>
                {:else}
                  <p class="px-3 py-3 text-micro text-fg-muted">
                    No conversations yet.
                  </p>
                {/each}
              </nav>
              {#if partial || conversationsCursor}
                <div class="border-t border-line px-3 py-2 text-micro">
                  {#if conversationsCursor}
                    <button
                      class="ui-prose-link"
                      onclick={moreConversations}
                      disabled={loadingConversations}
                      type="button"
                      >{loadingConversations
                        ? "Loading…"
                        : "More conversations"}</button
                    >
                  {:else if partial}
                    <span class="text-warn-text">Partial history</span>
                  {/if}
                </div>
              {/if}
            </div>
          {/if}
        </details>
        <a class="ui-btn-secondary" href={workspaceHref("/pm?new=1")}>New</a>
      {/snippet}
    </WorkspacePageHeader>

    {#if pmIsOffline}
      <!--
        The PM is installed but not running: the reader can still ask, and the
        answer arrives when their machine does. Say that once, quietly.
      -->
      <p class="text-micro text-fg-muted" data-pm-offline-note>
        Your PM is not running right now, so answers wait until it is back.
        Check it with <code class="font-mono text-micro text-fg"
          >anx pm status</code
        > on your computer.
      </p>
    {/if}
    {#if contextRefs.length}
      <!-- TODO(SCA-694): use WorkSummary when the shared component lands. -->
      <div class="pm-context" aria-label="Conversation context">
        {#each contextRefs as ref (ref)}
          <div class="flex max-w-full min-w-0 flex-wrap items-center gap-2">
            <AnxRefChip
              refValue={ref}
              resolved={resolvedRefs}
              organizationSlug={$page.params.organization}
              workspaceSlug={$page.params.workspace}
            />
            {#if resolvedRefs.get(ref)?.status}<span
                class="text-micro text-fg-muted"
                >{resolvedRefs.get(ref).status.replaceAll("_", " ")}</span
              >{/if}
            {#if resolvedRefs.get(ref)?.lastMovedAt}<span
                class="text-micro text-fg-subtle"
                ><Time value={resolvedRefs.get(ref).lastMovedAt} {now} /></span
              >{/if}
          </div>
        {/each}
      </div>
    {/if}
    {#if refsError}<p class="text-micro text-warn-text">{refsError}</p>{/if}
  </div>

  <!-- A scrollable region must be reachable by keyboard (axe scrollable-region-focusable). -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div
    class="pm-thread"
    bind:this={threadElement}
    onscroll={onThreadScroll}
    aria-label="PM conversation"
    role="region"
    tabindex="0"
  >
    <div class="pm-thread-inner" bind:this={threadInnerElement}>
      {#if loading}
        <p class="text-meta text-fg-muted" role="status">Loading…</p>
      {:else if !turns.length}
        <p class="pm-empty">
          Nothing asked yet. Pick a starter below, or type a question.
        </p>
      {/if}

      {#if turnsCursor}
        <button
          class="ui-prose-link w-fit text-micro"
          type="button"
          onclick={olderTurns}
          disabled={loadingOlder}
          >{loadingOlder ? "Loading older messages…" : "Older messages"}</button
        >
      {/if}

      <ol
        class="pm-turns"
        aria-label="Conversation messages"
        role="log"
        aria-live="polite"
        aria-relevant="additions text"
      >
        {#each turns as turn, index (turn.id)}
          {@const view = turnState(turn, now)}
          {@const proposed = proposedDecisionIds(turn, decisionRecords)}
          {@const evidence = evidenceRefsForTurn(turn)}
          {@const clock = clockTime(turn.created_at)}
          {@const grouped = startsTimeGroup(turn, turns[index - 1]) && clock}
          <li class="pm-pair">
            {#if grouped}
              <p class="pm-group-head">
                <Time value={turn.created_at} style="clock" {now} />
              </p>
            {/if}
            <!--
              One time per group, in the header above it. The bubble carried
              a second copy of the same "4:11 PM" directly under the first.
            -->
            <div class="pm-you">
              <p class="pm-bubble">{turn.text}</p>
            </div>
            <div class="pm-answer">
              {#if view.kind === "answered"}
                <MarkdownRenderer
                  source={answerBody(turn.response, proposed)}
                  resolved={resolvedRefs}
                  organizationSlug={$page.params.organization}
                  workspaceSlug={$page.params.workspace}
                  class="pm-response text-meta text-fg"
                />
              {:else if view.kind === "pending"}
                {#if turn.partial_response}
                  <p class="text-micro text-fg-muted">Draft answer</p>
                  <MarkdownRenderer
                    source={turn.partial_response}
                    resolved={resolvedRefs}
                    organizationSlug={$page.params.organization}
                    workspaceSlug={$page.params.workspace}
                    class="pm-response text-meta text-fg"
                  />
                {/if}
                <!--
                  One line. This was three: a "Running · 3 steps" fold, then
                  "••• Running · 46s", then a sentence about the PM being a
                  separate agent — all saying the turn is running. The line
                  is now the fold's own summary, so the steps are one click
                  under the words they belong to.
                -->
                {#if turn.activity?.length}
                  <details class="pm-run" data-pm-run="pending">
                    <summary class="pm-status">
                      {@render runningLine(view, turn)}
                    </summary>
                    {@render runSteps(turn)}
                  </details>
                {:else}
                  <p class="pm-status" role="status" data-pm-run="pending">
                    {@render runningLine(view, turn)}
                  </p>
                {/if}
                {#if !view.claimed}
                  <p class="pm-status-note">{UNCLAIMED_LABEL}</p>
                {:else if view.longWait}
                  <p class="pm-status-note">{EXPECTED_WAIT_LABEL}</p>
                {/if}
                {#if view.stalled}
                  <p class="pm-status-note">
                    No reply yet — the PM runner may be off. <a
                      class="ui-prose-link"
                      href={RUNBOOK_HREF}>How to start it</a
                    >
                  </p>
                {/if}
              {:else}
                <p
                  class="text-meta {view.kind === 'failed'
                    ? 'text-danger-text'
                    : 'text-fg-muted'}"
                >
                  {view.kind === "failed"
                    ? view.detail || "The PM did not answer."
                    : "Delivery uncertain"}
                  <button
                    class="ui-prose-link ml-2 text-micro"
                    type="button"
                    disabled={sending || !ready}
                    onclick={() => retryTurn(turn)}>Retry</button
                  >
                </p>
              {/if}
              <!--
                The run log, under the answer rather than over it. Collapsed
                it is one quiet line; above the answer it was the first thing
                the eye landed on, which is not what a reader opened the
                conversation for.
              -->
              {#if view.kind !== "pending" && turn.activity?.length}
                <details class="pm-run pm-run--done" data-pm-run="done">
                  <summary
                    >{activityLabel(turn)} · {turn.activity.length} steps</summary
                  >
                  {@render runSteps(turn)}
                </details>
              {/if}
              {#if evidence.length}
                {@const shown = expandedSources.has(turn.id)
                  ? evidence
                  : evidence.slice(0, SOURCE_PREVIEW)}
                {@const rest = evidence.length - shown.length}
                <!--
                  A quiet labelled row, bounded. Five refs under a reply wrapped
                  to three lines of chips, and the last one sat under the
                  composer where nobody could read it.
                -->
                <div class="pm-sources">
                  <span class="pm-sources-label">Sources</span>
                  <ul class="pm-evidence" aria-label="Evidence for this reply">
                    {#each shown as ref (ref)}
                      {@const link = evidenceLink(ref)}
                      <li class="flex min-w-0">
                        <RefChip
                          href={link.href}
                          external={link.isExternal}
                          title={link.raw}
                          ><span class="min-w-0 truncate">{link.label}</span
                          ></RefChip
                        >
                      </li>
                    {/each}
                    {#if rest > 0}
                      <li class="flex min-w-0">
                        <button
                          class="pm-sources-more"
                          type="button"
                          onclick={() => showAllSources(turn.id)}
                          aria-label={`Show ${rest} more source${rest === 1 ? "" : "s"}`}
                          >+{rest}</button
                        >
                      </li>
                    {/if}
                  </ul>
                </div>
              {/if}
              {#if proposed.length}
                <div class="mt-3 border-t border-line-subtle pt-2">
                  <p
                    class="text-micro font-semibold uppercase tracking-wide text-fg-muted"
                  >
                    Proposed decisions
                  </p>
                  <ul
                    class="mt-1 divide-y divide-line-subtle"
                    aria-label="Decisions proposed in this reply"
                  >
                    {#each proposed as id (id)}
                      {@const record = decisionRecords[id]}
                      <li
                        class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 py-1.5"
                      >
                        <span
                          class="line-clamp-2 min-w-0 flex-1 basis-80 break-words text-meta text-fg"
                          title={record
                            ? decisionTitle(record)
                            : `decision:${id}`}
                          >{record
                            ? decisionTitle(record)
                            : `decision:${id}`}</span
                        >
                        {#if record?.work_ref}
                          <a
                            class="min-w-0 truncate text-micro text-fg-muted hover:text-accent-text {workTitle(
                              record.work_ref,
                            )
                              ? ''
                              : 'font-mono'}"
                            href={workspaceHref(
                              `/tasks/${encodeURIComponent(record.work_ref)}`,
                            )}
                            title={record.work_ref}
                            >{workTitle(record.work_ref) || record.work_ref}</a
                          >
                        {/if}
                        {#if record}
                          {@const signal = receiptSignal(record.status)}
                          <ReceiptSignal {signal} />
                          {#if record.status === "awaiting_answer"}
                            <a
                              class="ui-prose-link text-micro"
                              href={decisionInboxHref(id)}>Answer</a
                            >
                          {/if}
                        {:else if record === null}
                          <SignalBadge tone="neutral">Unavailable</SignalBadge>
                        {:else}
                          <span class="text-micro text-fg-subtle">Loading…</span
                          >
                        {/if}
                      </li>
                    {/each}
                  </ul>
                  {#if decisionError}
                    <p class="mt-1 text-micro text-warn-text">
                      {decisionError}
                    </p>
                  {/if}
                </div>
              {/if}
            </div>
          </li>
        {/each}
      </ol>
      {#if error}
        <div
          role="alert"
          class="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
        >
          <span class="min-w-0 flex-1 break-words">{error}</span>
          {#if sessionExpired}
            <button
              class="ui-prose-link text-micro"
              type="button"
              onclick={signInAgain}>Sign in again</button
            >
          {:else if errorFromSend}
            <button
              class="ui-prose-link text-micro"
              type="button"
              disabled={sending || !draft.trim()}
              onclick={() => void send()}>Send again</button
            >
          {:else}
            <button
              class="ui-prose-link text-micro"
              type="button"
              onclick={() =>
                ready ? loadConversation(selectedId, true) : initialize()}
              >Retry</button
            >
          {/if}
        </div>
      {/if}
    </div>
  </div>

  <div class="pm-foot">
    {#if showJump}
      <button
        class="pm-jump"
        type="button"
        onclick={() => scrollToLatest(reducedMotion ? "auto" : "smooth")}
        >Jump to latest ↓</button
      >
    {/if}
    <form class="pm-composer" onsubmit={send}>
      {#if !turns.length && !loading}
        <div class="pm-suggestions" role="group" aria-label="Starter questions">
          {#each prompts as prompt}
            <button
              class="pm-chip"
              type="button"
              onclick={() => useSuggestion(prompt)}>{prompt}</button
            >
          {/each}
        </div>
      {/if}
      <label for="pm-message" class="sr-only">Message PM</label>
      <textarea
        id="pm-message"
        class="pm-composer-input"
        rows="1"
        bind:this={composerElement}
        bind:value={draft}
        required
        maxlength="16000"
        placeholder="Ask the PM…"
        disabled={sending}
        onkeydown={onComposerKeydown}
      ></textarea>
      <div class="pm-composer-foot">
        {#if !ready && error}
          <p class="pm-composer-note">
            Sending is disabled until PM is reachable.
          </p>
        {/if}
        <kbd
          class="pm-kbd"
          title="Enter sends. Shift+Enter adds a line."
          aria-label="Enter sends. Shift+Enter adds a line.">↵</kbd
        >
        <button
          class="pm-send"
          type="submit"
          aria-label="Send message"
          disabled={sending || !draft.trim() || !ready}
        >
          <svg viewBox="0 0 16 16" aria-hidden="true" focusable="false">
            <path
              d="M8 13V3.5M8 3.5 4 7.5M8 3.5l4 4"
              fill="none"
              stroke="currentColor"
              stroke-width="1.6"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </button>
      </div>
    </form>
  </div>
</WorkspacePageShell>

<style>
  /* The thread scrolls, not the page: the shell must give up its own scroll. */
  :global(.shell-main-scroll:has(.pm-page)) {
    overflow: hidden;
    padding-bottom: 0;
    display: flex;
    flex-direction: column;
  }
  :global(.shell-main-scroll:has(.pm-page) > .shell-content) {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  /* The compact shell's bottom nav is fixed; the composer must clear it. */
  @media (max-width: 1023px) {
    :global(.shell-main-scroll:has(.pm-page)) {
      padding-bottom: calc(3.25rem + env(safe-area-inset-bottom, 0px));
    }
  }
  :global(.pm-page) {
    flex: 1 1 auto;
    display: grid;
    grid-template-rows: auto 1fr auto;
    /* An auto column grows to its widest row's min-content, so one unbreakable
       id in the header or a turn would push the whole page past the viewport
       (the shell clips it, so it cannot even be scrolled to). */
    grid-template-columns: minmax(0, 1fr);
    height: 100%;
    min-height: 0;
  }
  /* The shell wrapper's `space-y-5` would fight the grid rows. */
  :global(.pm-page > * + *) {
    margin-block-start: 0;
  }

  .pm-head,
  .pm-thread-inner,
  .pm-composer,
  .pm-foot {
    width: min(46rem, 100%);
    margin-inline: auto;
  }
  .pm-context {
    display: flex;
    flex-wrap: wrap;
    min-width: 0;
    overflow-wrap: anywhere;
    align-items: baseline;
    gap: 0.5rem;
    margin-top: 0.375rem;
    font-size: 11px;
    color: var(--fg-subtle);
  }

  .pm-thread {
    min-height: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
    /*
     * The last line of an answer needs air between it and the composer.
     * With 24px a wrapped row of source chips finished flush against the
     * composer's top edge and read as cut off even when it was scrollable.
     */
    padding: 16px 0 40px;
  }
  .pm-thread-inner {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }
  .pm-empty {
    font-size: 13px;
    color: var(--fg-muted);
  }
  .pm-turns {
    display: flex;
    flex-direction: column;
    gap: 24px;
    min-width: 0;
  }
  .pm-pair {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }
  .pm-group-head {
    font-size: 11px;
    color: var(--fg-subtle);
    font-variant-numeric: tabular-nums;
  }

  .pm-you {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 2px;
    min-width: 0;
  }
  .pm-bubble {
    max-width: 80%;
    padding: 8px 12px;
    border: 1px solid var(--line);
    border-radius: 10px 10px 4px 10px;
    background: var(--panel);
    color: var(--fg);
    font-size: 13px;
    line-height: 18px;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .pm-answer {
    min-width: 0;
    font-size: 13px;
    line-height: 22px;
    color: var(--fg);
  }
  :global(.pm-response) {
    overflow-wrap: anywhere;
    line-height: 22px;
  }
  :global(.pm-response p),
  :global(.pm-response ul),
  :global(.pm-response ol) {
    margin: 0 0 12px;
  }
  :global(.pm-response ul),
  :global(.pm-response ol) {
    padding-left: 20px;
  }
  :global(.pm-response > :last-child) {
    margin-bottom: 0;
  }
  :global(.pm-response hr) {
    border: 0;
    border-top: 1px solid var(--line);
    margin: 12px 0;
  }
  :global(.pm-response code) {
    font-family: var(--font-mono);
    font-size: 0.85em;
    background: var(--bg-soft);
    padding: 0.05em 0.3em;
    border-radius: 3px;
  }
  /* Inline chip for a decision id the PM named mid-sentence. Markdown carries
     no class, so the Inbox href is the hook. */
  :global(.pm-response a[href*="?item=decision:"]) {
    display: inline-block;
    padding: 0 5px;
    border: 1px solid var(--line);
    border-radius: 4px;
    background: var(--bg);
    color: var(--accent-text);
    font-size: 11px;
    line-height: 16px;
    white-space: nowrap;
    text-decoration: none;
    vertical-align: baseline;
  }
  :global(.pm-response a[href*="?item=decision:"]:hover) {
    border-color: var(--line-strong);
    text-decoration: none;
  }

  .pm-status {
    min-height: 18px;
    font-size: 11px;
    color: var(--fg-subtle);
    font-variant-numeric: tabular-nums;
  }
  /* Inline, so a `<summary>` keeps its disclosure marker. */
  .pm-status-line {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .pm-status-note {
    margin-top: 4px;
    font-size: 11px;
    color: var(--fg-muted);
  }
  .pm-dots {
    display: inline-flex;
    gap: 3px;
  }
  .pm-dots i {
    width: 4px;
    height: 4px;
    border-radius: 999px;
    background: var(--fg-subtle);
    animation: pm-dot 1.2s infinite ease-in-out;
  }
  .pm-dots i:nth-child(2) {
    animation-delay: 0.2s;
  }
  .pm-dots i:nth-child(3) {
    animation-delay: 0.4s;
  }
  @keyframes pm-dot {
    0%,
    100% {
      opacity: 0.3;
    }
    50% {
      opacity: 1;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .pm-dots i {
      animation: none;
      opacity: 0.6;
    }
  }

  /*
   * The run log. Quieter than the answer in both states: the answer is 13px
   * `--fg`, this is 11px `--fg-subtle`, so the eye reaches the answer first
   * whether the turn is still working or finished.
   */
  .pm-run {
    font-size: 11px;
    color: var(--fg-subtle);
  }
  .pm-run--done {
    margin-top: 6px;
  }
  .pm-run > summary {
    cursor: pointer;
    width: fit-content;
    font-variant-numeric: tabular-nums;
  }
  .pm-run > summary:hover {
    color: var(--fg-muted);
  }
  .pm-run-steps {
    margin-top: 4px;
    padding-left: 14px;
    color: var(--fg-muted);
    list-style: disc;
  }

  /*
   * Sources: a labelled, bounded row. Five refs wrapped to three lines of
   * chips under a reply, and the last line sat behind the composer.
   */
  .pm-sources {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    margin-top: 8px;
    min-width: 0;
  }
  .pm-sources-label {
    flex: none;
    font-size: 11px;
    color: var(--fg-subtle);
  }
  .pm-evidence {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    min-width: 0;
  }
  .pm-sources-more {
    flex: none;
    height: 22px;
    padding: 0 7px;
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--fg-muted);
    font-size: 11px;
    font-variant-numeric: tabular-nums;
    cursor: pointer;
  }
  .pm-sources-more:hover {
    color: var(--fg);
    border-color: var(--line-strong);
  }

  .pm-foot {
    position: relative;
    padding-bottom: 8px;
  }
  .pm-jump {
    position: absolute;
    left: 50%;
    bottom: calc(100% + 12px);
    transform: translateX(-50%);
    height: 28px;
    padding: 0 12px;
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--panel);
    color: var(--fg-muted);
    font-size: 11px;
    white-space: nowrap;
    cursor: pointer;
  }
  .pm-jump:hover {
    color: var(--fg);
    border-color: var(--line-strong);
  }

  .pm-composer {
    border: 1px solid var(--line);
    border-radius: var(--radius-md);
    background: var(--panel);
    padding: 0;
  }
  .pm-composer:focus-within {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .pm-suggestions {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    padding: 8px 12px 0;
  }
  .pm-suggestions::-webkit-scrollbar {
    display: none;
  }
  .pm-chip {
    flex: 0 0 auto;
    height: 24px;
    padding: 0 8px;
    border: 1px solid var(--line-subtle);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--fg-muted);
    font-size: 11px;
    white-space: nowrap;
    cursor: pointer;
    transition: all var(--motion-fast);
  }
  .pm-chip:hover {
    color: var(--fg);
    border-color: var(--line);
    background: var(--bg-soft);
  }
  .pm-composer-input {
    display: block;
    width: 100%;
    padding: 10px 12px;
    border: 0;
    background: transparent;
    color: var(--fg);
    font-size: 13px;
    line-height: 20px;
    min-height: 40px;
    max-height: 200px;
    resize: none;
    overflow-y: auto;
  }
  .pm-composer-input:focus {
    outline: none;
    box-shadow: none;
  }
  .pm-composer-input::placeholder {
    color: var(--fg-subtle);
  }
  .pm-composer-foot {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    height: 32px;
    padding: 0 12px 8px;
  }
  .pm-composer-note {
    margin-right: auto;
    min-width: 0;
    font-size: 11px;
    color: var(--fg-subtle);
  }
  @media (hover: none) {
    .pm-kbd {
      display: none;
    }
  }

  .pm-kbd {
    font-family: inherit;
    font-size: 11px;
    color: var(--fg-subtle);
  }
  .pm-send {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    flex: 0 0 auto;
    border: 0;
    border-radius: var(--radius);
    background: var(--accent-solid);
    color: var(--fg);
    cursor: pointer;
  }
  .pm-send svg {
    width: 16px;
    height: 16px;
  }
  .pm-send:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .pm-history {
    position: relative;
  }
  .pm-history summary {
    cursor: pointer;
  }
  .pm-history summary::-webkit-details-marker {
    display: none;
  }
  .pm-history-panel {
    position: absolute;
    right: 0;
    top: calc(100% + 0.375rem);
    z-index: 30;
    width: min(22rem, 90vw);
    max-height: 24rem;
    overflow-y: auto;
    border: 1px solid var(--line);
    border-radius: var(--radius-md);
    background: var(--panel);
    box-shadow: var(--shadow-menu);
    padding: 0.25rem;
  }
  /* Narrow screens wrap the header actions to the left edge, where a panel
     anchored to the trigger's right edge hangs off-screen. Span the page
     gutter instead; the trigger is at the gutter too, so the panel still
     opens under it. Above this width the actions stay on the title's row —
     see the flex basis in `WorkspacePageHeader` — so the panel stays
     anchored to the trigger, which is where a reader looks for it. */
  @media (max-width: 639px) {
    .pm-history-panel {
      position: fixed;
      top: auto;
      left: 0.75rem;
      right: auto;
      width: min(22rem, calc(100vw - 1.5rem));
      margin-top: 0.375rem;
      max-height: min(24rem, 60vh);
    }
  }
  .pm-history-item {
    display: grid;
    gap: 0.125rem;
    padding: 0.5rem 0.625rem;
    border-radius: var(--radius);
    color: var(--fg-muted);
    font-size: 13px;
    text-decoration: none;
  }
  .pm-history-item:hover {
    background: var(--panel-hover);
    color: var(--fg);
  }
  .pm-history-item--active {
    background: var(--bg-soft);
    color: var(--fg);
  }
</style>
