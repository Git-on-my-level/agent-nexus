<script>
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  import { onMount, untrack } from "svelte";
  import { get } from "svelte/store";
  import { page } from "$app/stores";
  import { coreClient } from "$lib/coreClient";
  import {
    liveWorkspaceEvents,
    TASK_LIST_EVENT_TYPES,
  } from "$lib/liveWorkspaceEvents.js";
  import {
    authSessionReady,
    authenticatedAgent,
    initializeAuthSession,
    isAuthenticated,
  } from "$lib/authSession";
  import { cacheWorkRecord, readWorkSnapshot } from "$lib/workCache.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { formatAbsoluteDateTime, formatTimestamp } from "$lib/formatDate";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import StateEmpty from "$lib/components/state/StateEmpty.svelte";
  import Skeleton from "$lib/components/state/Skeleton.svelte";
  import ActorLabel from "$lib/components/ActorLabel.svelte";
  import {
    actorDisplayLabel,
    actorRegistry,
    agentRegistry,
    isKnownActor,
    principalRegistry,
  } from "$lib/actorSession";
  import SignalBadge from "$lib/components/pm/SignalBadge.svelte";
  import AnxRefPreview from "$lib/components/AnxRefPreview.svelte";
  import AnxRefChip from "$lib/components/AnxRefChip.svelte";
  import InfoTip from "$lib/components/InfoTip.svelte";
  import DecisionCard from "$lib/components/decide/DecisionCard.svelte";
  import InboxUndoToast from "$lib/components/inbox/InboxUndoToast.svelte";
  import { undoInboxResponse } from "$lib/inboxResponseQueue.js";
  import { briefStore, mountBrief } from "$lib/briefStore.js";
  import {
    decisionsForInitiative,
    initiativeLine,
    WATCH_STATE_LABELS,
  } from "$lib/brief.js";
  import MarkdownRenderer from "$lib/components/MarkdownRenderer.svelte";
  import PanelToggle from "$lib/components/layout/PanelToggle.svelte";
  import PlanView from "$lib/components/PlanView.svelte";
  import ProvenanceChip from "$lib/components/ProvenanceChip.svelte";
  import EvidenceHandoff from "$lib/components/participation/EvidenceHandoff.svelte";
  import TaskParticipation from "$lib/components/participation/TaskParticipation.svelte";
  import {
    collectPageRefs,
    isChipRef,
    indexResolvedRefs,
    resolveRefsInBatches,
  } from "$lib/refResolve.js";
  import {
    safeSourceHref,
    sourceLabel,
    isNexusOwned,
    workKey,
    errorMessage,
    readErrorExplanation,
    workSourceKey,
    humanizeInstants,
    connectionName,
  } from "$lib/pm/presentation.js";
  import { workProse, workSummaryModel } from "$lib/workSummary.js";
  import { nextActionOvertaken, planNextSteps } from "$lib/planNext.js";
  import { authoredProvenance, liveProvenance } from "$lib/reportProvenance.js";
  import {
    adoptPanelPreference,
    panelAutoCollapsed,
    panelCollapsed,
    SHELL_PANELS,
    writePanelPreference,
  } from "$lib/shellPanels.js";
  import {
    evidenceSources,
    observationHistory,
    observationErrorText,
    observationStatusLabel,
    observationStatusTone,
  } from "$lib/pm/evidence.js";
  import { pmFeaturesVisible } from "$lib/pm/onboardingState.js";
  import { pmPresence } from "$lib/pm/presence.js";
  import { usePageTitle } from "$lib/pageTitle.svelte.js";
  let work = $state(null),
    /**
     * True while `work` is the row the list already had rather than the card
     * read. The page renders it immediately — a title and a status beat a
     * spinner — and says so where it would otherwise claim a fact it has not
     * read yet.
     */
    workPartial = $state(false),
    observations = $state([]),
    nextCursor = $state(""),
    loading = $state(true),
    error = $state(""),
    notFound = $state(false),
    evidenceError = $state(""),
    notice = $state(""),
    refreshing = $state(false),
    ready = $state(false),
    plan = $state(null),
    planState = $state(null),
    planRefs = $state(new Map()),
    /**
     * When this page last read the computed plan.
     *
     * Core recomputes progress, health and the critical path on every read, so
     * the plan is live data — and the honest age to show a reader is the age of
     * the read, which only the client knows.
     */
    planReadAt = $state(""),
    /**
     * The summary the plan read returned.
     *
     * Core recomputes status, progress and the next step on every plan read,
     * so this is fresher than the one the card row carried; the header shows
     * whichever it has.
     */
    planSummary = $state(null),
    /**
     * Has a plan read answered for this card yet?
     *
     * `plan === null` cannot tell "this card has no plan" from "nothing has
     * come back yet", and the header read it as the second: after a plan was
     * cleared it fell back to the `plan_state` the card row happened to carry
     * and put the steps of a plan that is gone back on screen. A successful
     * read sets this, empty or not; a failed one leaves it, because a dropped
     * request is not news about the plan.
     */
    planLoaded = $state(false);
  /** Ticks so a relative age on screen does not go quietly out of date. */
  let now = $state(Date.now());
  let requestId = 0;
  /**
   * The newest read of this card's record, its plan, and the chips they name.
   *
   * `requestId` says which card is open, and a live event re-reading the card
   * already on screen never changes it — so two refreshes that overlapped both
   * passed it, and whichever *landed* last won. That is routinely the older
   * one, and with no Reload button nothing puts it right.
   *
   * One ticket covers all three writes, because they are one answer about one
   * card. Guarding the plan alone was not enough: an older `work.get` could
   * still land after a newer one and bring back the record — and with it the
   * `plan_state` and summary — of a plan that had since been cleared.
   */
  let readId = 0;
  /**
   * A ticket for one read of this card. It stops being live when the page
   * opens another card, or when a newer read of this one is issued.
   *
   * @returns {{live: () => boolean}}
   */
  function beginRead() {
    const card = requestId;
    const read = ++readId;
    return { live: () => card === requestId && read === readId };
  }
  let refPreview = $state();

  /**
   * This card's summary, as the one renderer every other surface uses.
   *
   * The Tasks table, the board, the Overview and this header all render
   * `WorkSummary` over this model, which is what keeps them from disagreeing
   * about the same card. The plan read's own summary wins when there is one,
   * because core recomputed it more recently than the card row did; the plan
   * state is merged in either way so the progress bar is drawn from the steps
   * this page has actually read.
   */
  let cardSummary = $derived.by(() => {
    if (!work) return null;
    return workSummaryModel(
      { ...work, plan_state: effectivePlanState },
      { now, summary: planSummary },
    );
  });
  /**
   * The card body, as prose.
   *
   * `summary=1` puts the computed object at `summary` and moves the prose to
   * `summary_text`. Reading `summary` directly rendered `[object Object]`
   * where the body should be.
   */
  let cardProse = $derived(workProse(work));
  /**
   * The computed plan state this page is reading.
   *
   * The plan read is the fresher of the two — core recomputes status, the
   * critical path and the next steps on every one — and the card row's copy
   * is what the page has until that lands. Once a read has answered, its
   * answer stands, "there is no plan" included: the row's copy is from
   * whenever the record was read, and falling back to it after a successful
   * empty read is what put a cleared plan's steps back on screen.
   */
  let effectivePlanState = $derived(
    planLoaded ? planState : (work?.plan_state ?? null),
  );
  /**
   * What happens next, from the plan rather than from an annotation.
   *
   * Core recomputes `next_steps` on every plan read — the steps whose
   * dependencies are all done and which are neither done nor blocked — and
   * `planNextSteps` only names and orders the ids it sent, with core's own
   * `next` first so this page and the Tasks row cannot pick different ones.
   * The page used to print `next_action`, a field somebody typed once: on a
   * card whose first phase had since finished, that was the page's most
   * confident wrong sentence.
   */
  let nextSteps = $derived(
    planNextSteps(plan, effectivePlanState, {
      preferred: cardSummary?.next?.id ?? "",
    }),
  );
  /**
   * Every card this plan points at.
   *
   * A step's status is computed from the card behind it, and `card_moved` for
   * that card carries only *its* ref — so a page filtering the stream on its
   * own ref alone sat there with a step reading "In progress" minutes after
   * the card behind it was marked done. The set comes from the plan already in
   * memory, so matching an event against it costs nothing per event and reads
   * nothing extra.
   */
  let planCardRefs = $derived(
    new Set(
      (Array.isArray(plan?.steps) ? plan.steps : [])
        .map((step) => String(step?.ref ?? "").trim())
        .filter(Boolean),
    ),
  );
  /** How many ready steps fit before the rest become a count. */
  const NEXT_PREVIEW = 3;
  let shownNextSteps = $derived(nextSteps.slice(0, NEXT_PREVIEW));
  /** Computed step status by id, for the one word the header needs. */
  let stepStatus = $derived(
    new Map(
      (Array.isArray(effectivePlanState?.steps)
        ? effectivePlanState.steps
        : []
      ).map((step) => [String(step?.id ?? ""), String(step?.status ?? "")]),
    ),
  );
  /**
   * Who acts next.
   *
   * `next_actor` is a free-text annotation, so it holds an actor on a card an
   * agent filed properly and the word "agent" on one where somebody wrote
   * down a role — which the avatar then rendered as "AG agent", initials for
   * nobody. So it is used when a registry can actually name it, and the card's
   * owner answers otherwise. A role nobody can name is still shown, as the
   * word it is: it says a person is not holding this, which is a fact worth
   * having. Nothing here invents a name.
   */
  let nextActor = $derived.by(() => {
    for (const candidate of [
      work?.next_actor,
      work?.owner,
      cardSummary?.owner,
    ]) {
      const value = String(candidate ?? "").trim();
      if (
        value &&
        isKnownActor(value, $actorRegistry, $principalRegistry, $agentRegistry)
      ) {
        return {
          id: value,
          label: actorDisplayLabel(value, $actorRegistry, $principalRegistry),
          named: true,
        };
      }
    }
    const role = String(work?.next_actor ?? "").trim();
    if (!role) return null;
    /*
     * Still through `actorDisplayLabel`: it is what turns a synthetic handle
     * — `agent_ext_0123…`, which is a machine identifier and not a name — into
     * "Workspace member". A role somebody typed comes back as the word they
     * typed, which is the honest answer for it.
     */
    return {
      id: "",
      label: actorDisplayLabel(role, $actorRegistry, $principalRegistry),
      named: false,
    };
  });
  /**
   * How the card stands with whoever acts next.
   *
   * The verb follows the lead step's computed status: a step somebody is
   * already working is not one anybody is waiting on. The status line and the
   * Next section both use it, so the page says one thing in two places.
   */
  let waitingVerb = $derived(
    shownNextSteps.length && stepStatus.get(shownNextSteps[0].id) === "active"
      ? "with"
      : "waiting on",
  );
  /**
   * The hand-written next action, where the plan has not already passed it.
   *
   * A note that names a finished step and no open one has been overtaken, and
   * showing it beside a computed next step would be the page contradicting
   * itself. There is no honest age to date it with either — `updated_at` is
   * the card's concurrency token, which a phase move bumps — so a note that
   * still stands is marked as hand-written rather than stamped with a time
   * nobody wrote it at.
   */
  let nextNote = $derived.by(() => {
    const note = String(work?.next_action ?? "").trim();
    if (!note) return "";
    return nextActionOvertaken(note, plan, effectivePlanState) ? "" : note;
  });

  /**
   * The right rail collapses, remembered per viewer. It stacks below the
   * content under `xl` anyway, so the toggle only has work to do on a wide
   * window — `shellPanels` disables it rather than offering a no-op.
   */
  let railPreference = $state(null);
  let viewportWidth = $state(0);
  let railViewerId = $derived($authenticatedAgent?.agent_id || "");
  // Read once per viewer; see `adoptPanelPreference` for why re-reading on
  // every identity tick would undo a collapse the viewer just made.
  let railPreferenceViewerId = $state(/** @type {string | null} */ (null));
  $effect(() => {
    const viewerId = railViewerId;
    const previousViewerId = untrack(() => railPreferenceViewerId);
    if (previousViewerId === viewerId) return;
    railPreference = adoptPanelPreference(SHELL_PANELS.RAIL, {
      fromViewerId: previousViewerId,
      toViewerId: viewerId,
    });
    railPreferenceViewerId = viewerId;
  });
  let railCollapsed = $derived(
    panelCollapsed({
      panel: SHELL_PANELS.RAIL,
      preference: railPreference,
      viewportWidth,
      defaultCollapsed: !railWorthShowing,
    }),
  );
  let railAutoCollapsed = $derived(
    panelAutoCollapsed({ panel: SHELL_PANELS.RAIL, viewportWidth }),
  );
  function toggleRail(collapsed) {
    railPreference = collapsed;
    writePanelPreference(SHELL_PANELS.RAIL, collapsed, railViewerId);
  }

  /**
   * The plan and every ref the page is about to chip, in two reads: one plan,
   * one batch resolve. Chips never fetch for themselves, so a twenty-step plan
   * whose body also names six tasks is still two requests rather than
   * twenty-seven.
   *
   * Prose counts: the card body, the acceptance criteria and the blockers all
   * render through the shared markdown renderer now, and a `card:` written in
   * one of those is the same chip a plan step is.
   */
  /**
   * The plan read, issued at once rather than after `work.get`.
   *
   * It needs the card id, which the route already has, so waiting for the card
   * record only bought a second round trip. The plan is applied as soon as it
   * lands; the refs it names are resolved by `filePlan`, which does need the
   * card body to know what else to chip.
   */
  function loadPlanRequest(read, id) {
    return Promise.resolve()
      .then(() => coreClient.getCardPlan(id))
      .then((result) => {
        if (!read.live()) return null;
        const steps = Array.isArray(result?.plan?.steps)
          ? result.plan.steps
          : [];
        /*
         * Whatever came back is what is on screen, an empty plan included. A
         * plan whose steps were cleared, and a card that has none, both answer
         * successfully with no steps; keeping the previous ones left the page
         * drawing work the card no longer has. A read that *failed* is a
         * different thing and stays in the catch below.
         */
        plan = steps.length ? result.plan : null;
        planState = steps.length ? (result.plan_state ?? null) : null;
        planSummary =
          steps.length && result.summary && typeof result.summary === "object"
            ? result.summary
            : null;
        planReadAt = steps.length ? new Date().toISOString() : "";
        /*
         * Read, and answered — even when the answer is that there is no plan.
         * Everything downstream reads this rather than testing `plan` for
         * null, which cannot tell "this card has none" from "nothing has come
         * back yet" and so fell back to the card row's own stale copy.
         */
        planLoaded = true;
        return steps;
      })
      .catch(() => {
        /*
         * A read that failed is not a plan that is gone. Core answers a card
         * with no plan with a 200 and no steps, so this path is a real
         * failure — a dropped connection, a refused read — and blanking a
         * plan the page has already read and still believes would be the
         * wrong answer to it. What is on screen stays, and the steps it is
         * drawing are handed on so their chips are resolved with the rest of
         * the page rather than turning into "not found" because one request
         * did not land.
         */
        if (!read.live()) return null;
        return Array.isArray(plan?.steps) ? plan.steps : [];
      });
  }

  /**
   * Every ref the page is about to chip, in one batch resolve: the plan's
   * steps plus the prose. Chips never fetch for themselves, so a twenty-step
   * plan whose body also names six tasks is one request rather than
   * twenty-six.
   */
  async function filePlan(read, stepsPromise) {
    const steps = await stepsPromise;
    // `null` is a read another one has already overtaken: its steps are not
    // on screen, so resolving their refs would file chips for a plan nobody
    // is looking at over the ones that are.
    if (steps === null || !read.live()) return;
    const refs = collectPageRefs(
      [
        workProse(work),
        work?.next_action,
        work?.wake_condition,
        ...(work?.definition_of_done ?? []),
        ...(work?.blockers ?? []),
      ],
      {
        extraRefs: [
          ...steps.map((step) => step?.ref),
          ...(work?.relations ?? []).map((relation) => relation?.ref),
        ].filter(Boolean),
      },
    );
    if (!refs.length) return;
    try {
      const resolved = await resolveRefsInBatches(refs, (batch) =>
        coreClient.resolveRefs(batch),
      );
      if (read.live()) planRefs = resolved;
    } catch {
      // Chips fall back to "not found" rather than the plan failing to render.
      if (read.live()) planRefs = indexResolvedRefs({}, refs);
    }
  }
  const briefState = briefStore();
  let initiativeRef = $derived(`card:${workId}`);
  let briefRow = $derived(
    $briefState.data?.initiatives?.find((row) => row.ref === initiativeRef) ??
      null,
  );
  let briefParts = $derived(briefRow ? initiativeLine(briefRow, now) : []);
  let decisions = $derived(
    decisionsForInitiative($briefState.data, initiativeRef),
  );
  /**
   * Evidence refs arrive with the observations, after the plan's batch. They
   * are resolved here once, so a chip shows a title rather than an id.
   */
  $effect(() => {
    const refs = [];
    for (const observation of observations)
      for (const entry of observation?.evidence ?? [])
        if (entry?.ref && isChipRef(entry.ref) && !planRefs.has(entry.ref))
          refs.push(entry.ref);
    const wanted = [...new Set(refs)];
    if (!wanted.length) return;
    const card = requestId;
    resolveRefsInBatches(wanted, (batch) => coreClient.resolveRefs(batch))
      .then((resolved) => {
        if (card === requestId)
          planRefs = new Map([...planRefs, ...resolved]);
      })
      .catch(() => {});
  });
  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  let workId = $derived($page.params.workId);
  let pmState = $derived(
    $pmPresence.workspace === ($page.data?.workspace?.slug ?? "")
      ? $pmPresence
      : null,
  );
  // Gates the PM affordances and wording only, never a read: a proposal
  // filed earlier still waits for a yes whether or not a PM is running now.
  let pmVisible = $derived(pmFeaturesVisible(pmState));
  let pmHref = $derived(
    workspaceHref(`/pm?work_ref=${encodeURIComponent(work?.ref || workId)}`),
  );
  let sourceName = $derived(sourceLabel(work?.source));
  let nexusOwned = $derived(work ? isNexusOwned(work) : false);
  let lastCheckedAt = $derived(work?.freshness?.last_observed_at || "");
  let refreshPending = $derived(
    ["queued", "running"].includes(work?.refresh?.state),
  );
  let refreshError = $derived(readErrorExplanation(work?.refresh?.last_error));
  let lastAttemptAt = $derived(work?.refresh?.last_attempt_at || "");
  let failedAttempts = $derived(Number(work?.refresh?.failures) || 0);
  let sources = $derived(evidenceSources(observations, work));
  /**
   * Is the Source block saying anything? A task created here with nobody on
   * it answers "Agent Nexus" and "Not assigned", which is two rows of nothing.
   */
  let sourceDetailsWorthShowing = $derived(
    Boolean(
      work?.owner ||
      (work?.source?.authority && work.source.authority !== "nexus") ||
      work?.source?.native_id ||
      work?.source?.url,
    ),
  );
  // Evidence links are distinct already; a long comment thread still gets a
  // short list first.
  const LINK_PREVIEW = 6;
  let showAllLinks = $state(false);
  // Other tasks that mirror this same source item through another connection.
  // The Tasks table keeps one row per source item; the rest live here.
  let mirrors = $state([]);
  let hasNext = $derived(
    Boolean(
      nextSteps.length ||
      nextActor ||
      nextNote ||
      work?.blockers?.length ||
      work?.wake_condition,
    ),
  );
  /**
   * Is the Evidence section saying anything, or offering anything to do?
   *
   * A task created here with no reader attached answers "No activity" and
   * nothing else — a heading, a tip and an absence. A task with an outside
   * source always has something to offer, because the reader can be asked to
   * check it now.
   */
  let evidenceWorthShowing = $derived(
    Boolean(
      !nexusOwned ||
      sources.length ||
      observations.length ||
      evidenceError ||
      refreshError ||
      notice,
    ),
  );
  /**
   * Does the rail have anything in it?
   *
   * With no outside source and nothing waiting on the reader, the rail is a
   * heading, a tip about an absence and a link to an empty Inbox view — 18rem
   * of the window spent saying nothing. It starts collapsed there; the
   * viewer's own choice still wins, because `panelCollapsed` reads this only
   * as the default.
   */
  let railWorthShowing = $derived(
    Boolean(
      sourceDetailsWorthShowing ||
      work?.project_ref ||
      work?.start_at ||
      work?.due_at,
    ),
  );
  let hasDetails = $derived(
    Boolean(work?.executions?.length),
  );
  $effect(() => {
    const id = workId;
    if (ready) void load(id);
  });
  async function load(id = workId) {
    const ticket = ++requestId;
    const read = beginRead();
    loading = true;
    error = "";
    notFound = false;
    evidenceError = "";
    refreshing = false;
    notice = "";
    observations = [];
    mirrors = [];
    showAllLinks = false;
    nextCursor = "";
    plan = null;
    planState = null;
    planSummary = null;
    planRefs = new Map();
    planReadAt = "";
    planLoaded = false;

    /*
     * The row the list already had, painted before anything is requested. The
     * reader clicked a card whose title, status and owner were on screen a
     * moment ago; making them watch a spinner to see them again is the slowest
     * part of opening a card that is otherwise one request away.
     */
    const snapshot = readWorkSnapshot(id);
    work = snapshot?.work ?? null;
    workPartial = Boolean(snapshot && !snapshot.full);

    /*
     * Everything this page needs, started at once. The plan used to wait for
     * `work.get` to come back first, which turned one round trip into two in
     * sequence — it does not need the card record to be issued, only filed.
     */
    const planRead = loadPlanRequest(read, id);
    const results = await Promise.allSettled([
      // `summary=1`: core computes the shared summary only when asked.
      coreClient.getWork(id, { summary: 1 }),
      coreClient.listWorkObservations(id, { limit: 30 }),
    ]);
    if (ticket !== requestId) return;
    if (results[0].status === "fulfilled") {
      const record = results[0].value.work;
      /*
       * A live refresh issued while this load was in flight has already
       * answered with a newer record; this one is history and must not be
       * written over it. The reads it feeds still run — they are filed
       * against the same ticket and drop out on their own if it has moved.
       */
      if (read.live()) {
        work = record;
        workPartial = false;
      }
      if (!record) error = "The workspace did not return this initiative.";
      else {
        cacheWorkRecord(record);
        void loadMirrors(ticket, record);
        void filePlan(read, planRead);
      }
    } else if (read.live()) {
      error = errorMessage(results[0].reason);
      notFound = results[0].reason?.status === 404;
      // A failed read must not leave a list row on screen looking like one.
      work = null;
      workPartial = false;
    }
    if (results[1].status === "fulfilled") {
      observations = results[1].value.observations || [];
      nextCursor = results[1].value.next_cursor || "";
    } else evidenceError = errorMessage(results[1].reason);
    loading = false;
  }
  /**
   * The record and the plan, in place: a live change must not blank the page
   * the operator is reading.
   *
   * The plan is re-read here and not only on a full load because this page has
   * no Reload button any more. The plan is the page's lead content and the
   * thing an agent changes most — a step going from active to done is exactly
   * the event that just fired — so a live page that re-read only the card
   * record would keep drawing a plan it knew was stale.
   */
  async function refreshWorkRecord() {
    const read = beginRead();
    try {
      const result = await coreClient.getWork(workId, { summary: 1 });
      if (read.live() && result?.work) work = result.work;
    } catch {
      // The next change tries again.
    }
    if (!read.live()) return;
    void filePlan(read, loadPlanRequest(read, workId));
  }
  async function loadMirrors(ticket, loadedWork) {
    const key = workSourceKey(loadedWork);
    if (!key) return;
    try {
      const result = await coreClient.listWork({
        source: loadedWork.source.authority,
        limit: 200,
      });
      if (ticket !== requestId) return;
      mirrors = (Array.isArray(result?.work) ? result.work : []).filter(
        (other) =>
          workSourceKey(other) === key &&
          workKey(other) !== workKey(loadedWork),
      );
    } catch {
      // Fail soft: the disclosure is context, not the page.
    }
  }
  async function loadMore() {
    if (loading) return;
    const ticket = requestId;
    loading = true;
    evidenceError = "";
    try {
      const result = await coreClient.listWorkObservations(workId, {
        limit: 30,
        cursor: nextCursor,
      });
      if (ticket === requestId) {
        observations = [...observations, ...(result.observations || [])];
        nextCursor = result.next_cursor || "";
      }
    } catch (err) {
      if (ticket === requestId) evidenceError = errorMessage(err);
    } finally {
      if (ticket === requestId) loading = false;
    }
  }
  async function refresh() {
    refreshing = true;
    notice = "";
    const ticket = requestId;
    try {
      const result = await coreClient.requestWorkRefresh(workId);
      if (ticket !== requestId) return;
      work = { ...work, refresh: result.refresh };
      notice = `Refresh ${result.refresh?.state || "requested"}. Evidence changes only after a reader reports back.`;
    } catch (err) {
      if (ticket === requestId)
        evidenceError = `The check could not be requested: ${errorMessage(err)}`;
    } finally {
      if (ticket === requestId) refreshing = false;
    }
  }
  onMount(() => {
    let disposed = false;
    const stopBrief = mountBrief();
    // A move or assignment made elsewhere (the ⌘K palette, an agent, the
    // board) re-reads this task's record and its plan. Reader reports are
    // refreshed by "Check <source> now" and by re-opening the card.
    const stopLive = liveWorkspaceEvents({
      client: coreClient,
      types: TASK_LIST_EVENT_TYPES,
      filter: (event) => {
        if (!Array.isArray(event.refs)) return false;
        const own = work?.ref || workId;
        return event.refs.some((ref) => ref === own || planCardRefs.has(ref));
      },
      onChange: () => {
        if (ready && work) void refreshWorkRecord();
      },
    });
    /*
     * `initializeAuthSession` always re-reads `/auth/session`, so gating the
     * first card read on it put a whole round trip in front of every card
     * open — including one opened from a list the same session had just
     * loaded. When the session is already hydrated the page reads the card
     * now and lets the session refresh land behind it; only a cold page waits.
     */
    const sessionWarm = get(authSessionReady) && isAuthenticated();
    if (sessionWarm) ready = true;
    initializeAuthSession({
      fetchFn: globalThis.fetch.bind(globalThis),
      workspaceSlug: $page.params.workspace,
      authDriver: "work-detail",
    })
      .then(() => {
        if (!disposed) ready = true;
      })
      .catch((err) => {
        if (sessionWarm) return;
        error = errorMessage(err);
        loading = false;
      });
    const clock = window.setInterval(() => {
      now = Date.now();
    }, 60_000);
    return () => {
      disposed = true;
      requestId++;
      stopLive();
      stopBrief();
      window.clearInterval(clock);
    };
  });

  usePageTitle(() => `${work?.title || "Initiative"} · Agent Nexus`);
</script>

<svelte:window bind:innerWidth={viewportWidth} />
<WorkspacePageShell>
  <a
    class="w-fit text-micro text-fg-muted hover:text-fg"
    href={workspaceHref("/initiatives")}>← Initiatives</a
  >
  {#if error && notFound}
    <!-- A missing initiative is not an outage: retrying a 404 cannot help. -->
    <StateEmpty
      title="Initiative not found"
      helper="It may have been removed, or this link points somewhere else."
      actionLabel="Back to initiatives"
      actionHref={workspaceHref("/initiatives")}
    />
  {:else if error}<StateError
      title="Initiative unavailable"
      message={error}
      onretry={() => load()}
      retrying={loading}
    />{/if}
  {#if loading && !work}
    <!-- The header and first section in the places they will land. -->
    <div class="ui-skeleton-reveal space-y-3" aria-busy="true">
      <p class="sr-only" role="status">Loading…</p>
      <div class="h-6 w-2/5 animate-pulse rounded bg-panel-hover"></div>
      <div class="flex gap-1.5">
        <div class="h-5 w-20 animate-pulse rounded bg-panel-hover"></div>
        <div class="h-5 w-24 animate-pulse rounded bg-panel-hover"></div>
      </div>
      <Skeleton rows={4} class="max-w-2xl pt-4" />
    </div>
  {/if}
  {#if work}
    <WorkspacePageHeader title={work.title || "Untitled initiative"}>
      {#snippet subtitle()}
        <!-- Baseline, not centre: the summary can run to a second line, and
             the badges belong on its first. -->
        <span class="flex flex-wrap items-baseline gap-x-2 gap-y-1">
          <!-- State and its reason come from the brief, the one computed
               read. Nothing on this line is stored or hand-written. -->
          {#if briefRow}
            <span
              class="text-meta font-medium text-fg"
              data-source="computed:initiatives.state"
              >{WATCH_STATE_LABELS[briefRow.state]}</span
            >
            {#each briefParts as part (part.field)}
              <span
                class="text-meta text-fg-muted"
                data-source={`computed:${part.field}`}>{part.text}</span
              >
            {/each}
            {#if briefRow.progress}
              <span
                class="text-meta text-fg-muted"
                data-source="computed:initiatives.progress"
                >{briefRow.progress.done} of {briefRow.progress.total} done</span
              >
            {/if}
          {:else if !$briefState.data}
            <span class="text-micro text-fg-subtle" role="status"
              >Reading state…</span
            >
          {/if}
          {#if workPartial}
            <!-- The list's own row is on screen while the card is read. Say
                 so rather than letting a partial view pass for the whole. -->
            <span class="text-micro text-fg-subtle" role="status"
              >Reading the initiative…</span
            >
          {/if}
        </span>
      {/snippet}
      {#snippet actions()}<!--
          Actions on the right, metadata under the title. "Copy ref" used to
          float in the middle of the badge row, between a warning badge and
          nothing, which made the row read as neither state nor controls.
        --><button
          class="ui-btn-secondary"
          type="button"
          title={work.ref}
          onclick={() => navigator.clipboard?.writeText(work.ref)}
          >Copy ref</button
        >{#if pmVisible}<!--
            Named for what it does, and it carries this card: the sidebar's
            "Ask PM" opens an empty thread, so two buttons a thumb apart
            reading the same three words looked like one of them was a
            mistake. No Reload beside them — the card, its plan and its chips
            re-read themselves off the workspace event stream.
          --><a
            class="ui-btn-primary"
            href={pmHref}>Ask PM about this</a
          >{/if}{/snippet}
    </WorkspacePageHeader>
    <MutationSendStatus target={`card:${workId}:assignment`} />
    <MutationSendStatus target={`task:${workId}:move`} />
    <div
      class="grid gap-6 {railCollapsed
        ? ''
        : 'xl:grid-cols-[minmax(0,1fr)_18rem]'}"
      data-task-rail={railCollapsed ? "collapsed" : "expanded"}
    >
      <div class="min-w-0 space-y-7">
        {#if cardProse}
          <!-- The description, once, collapsed. The plan below is the
               working view; this is only what the initiative was filed as. -->
          <details class="text-meta" data-initiative-description>
            <summary class="w-fit cursor-pointer text-micro text-fg-muted"
              >Description</summary
            >
            <MarkdownRenderer
              source={cardProse}
              class="mt-2 text-meta text-fg [overflow-wrap:anywhere]"
              resolved={planRefs}
              organizationSlug={$page.params.organization}
              workspaceSlug={$page.params.workspace}
              onpreview={(model, anchor) => refPreview?.open(model, anchor)}
              onpreviewclose={() => refPreview?.requestClose()}
            />
          </details>
        {/if}
        {#if plan}
          <section data-initiative-plan>
            <div class="flex flex-wrap items-baseline justify-between gap-2">
              <h2 class="ui-label">Plan</h2>
              <ProvenanceChip
                model={liveProvenance(
                  planReadAt,
                  { status: planReadAt ? "ok" : "loading" },
                  now,
                )}
              />
            </div>
            <div class="mt-3">
              <PlanView
                {plan}
                {planState}
                resolved={planRefs}
                organizationSlug={$page.params.organization}
                workspaceSlug={$page.params.workspace}
                onpreview={(model, anchor) => refPreview?.open(model, anchor)}
                onpreviewclose={() => refPreview?.requestClose()}
              />
            </div>
          </section>
        {/if}
        {#if work.definition_of_done?.length}
          <section>
            <h2 class="ui-label">Done when</h2>
            <ul class="list-disc space-y-1 break-words pl-5 text-meta text-fg">
              {#each work.definition_of_done as criterion}<li>
                  <MarkdownRenderer
                    inline
                    source={criterion}
                    resolved={planRefs}
                    organizationSlug={$page.params.organization}
                    workspaceSlug={$page.params.workspace}
                    onpreview={(model, anchor) =>
                      refPreview?.open(model, anchor)}
                    onpreviewclose={() => refPreview?.requestClose()}
                  />
                </li>{/each}
            </ul>
          </section>
        {:else if !workPartial}
          <!-- Only once the card itself has been read: a list row does not
               carry acceptance criteria. Start-aligned with its section. -->
          <section class="flex justify-start" data-no-acceptance-criteria>
            <SignalBadge tone="warn">No acceptance criteria</SignalBadge>
          </section>
        {/if}
        {#if hasNext}
          <section>
            <h2 class="ui-label">Who is next</h2>
            <!--
              Who acts next, as a name a reader recognises. A `next_actor` no
              registry can name is a role, not a principal, so it is written
              as the word it is rather than given initials and an avatar.
            -->
            <p class="flex flex-wrap items-center gap-x-2 gap-y-1 text-meta">
              {#if nextActor?.named}<ActorLabel
                  label={nextActor.label}
                  seed={nextActor.id}
                  size="xs"
                  prefix={nextSteps.length ? waitingVerb : "owner"}
                />{:else if nextActor}<span
                  class="min-w-0 break-words text-fg [overflow-wrap:anywhere]"
                  data-next-actor-role
                  >Next actor: {nextActor.label} — no principal named</span
                >{:else}<span class="text-fg-muted">Nobody assigned</span>{/if}
            </p>
            {#if nextNote}
              <div
                class="mt-2 flex flex-wrap items-baseline gap-x-2 gap-y-1"
                data-next-note
              >
                <p class="min-w-0 break-words text-meta text-fg-muted">
                  {nextNote}
                </p>
                <!-- Hand-written, beside steps that are computed. No date: a
                     card's `updated_at` is its concurrency token, so stamping
                     this note with it would claim a rewrite nobody made. -->
                <ProvenanceChip
                  model={authoredProvenance({ reviewable: false })}
                />
              </div>
            {/if}
            {#if work.blockers?.length}
              <ul
                class="mt-2 list-disc space-y-1 break-words pl-5 text-meta text-warn-text"
              >
                {#each work.blockers as blocker}<li>{blocker}</li>{/each}
              </ul>
            {/if}
            {#if work.wake_condition}<p class="mt-2 text-micro text-fg-muted">
                Wakes when: {work.wake_condition}
              </p>{/if}
          </section>
        {/if}
        <section data-initiative-decisions>
          <h2 class="ui-label">Open decisions</h2>
          {#if decisions.length}
            <ul class="mt-3 space-y-3">
              {#each decisions as decision (decision.ref)}
                <li><DecisionCard {decision} variant="row" /></li>
              {/each}
            </ul>
          {:else if $briefState.error && !$briefState.data}
            <p class="mt-2 text-meta text-fg-muted" role="status">
              Decisions could not be read.
              <button
                class="ui-prose-link"
                type="button"
                onclick={() => briefState.refresh({ force: true })}
                >Retry</button
              >
            </p>
          {:else if !$briefState.data}
            <p class="mt-2 text-meta text-fg-muted" role="status">
              Loading decisions…
            </p>
          {:else}
            <p class="mt-2 text-meta text-fg-muted">Nothing waiting on you.</p>
          {/if}
        </section>
        <!-- Nothing to report is not worth a line. An empty Participation
             section on a card nobody has run against said "No activity ·
             Check again", directly above an Evidence section saying "No
             activity" — two headings for one absence. -->
        <TaskParticipation
          tasks={[{ ref: work.ref || workId, title: work.title }]}
          {workspaceHref}
          hideWhenEmpty
        />
        <section
          class={evidenceWorthShowing ? "" : "hidden"}
          data-task-evidence
        >
          <h2 class="ui-label">Evidence, newest first</h2>
          <EvidenceHandoff
            {observations}
            unavailable={Boolean(evidenceError) && !observations.length}
            note={nexusOwned && !lastCheckedAt && !observations.length
              ? "This initiative was created here, so there is no outside tracker to read back from."
              : ""}
          />
          {#if nexusOwned && !lastCheckedAt && !observations.length}
            <!-- Nothing to say: the heading's tip already explains why a task
                 created here has no outside source to read back from. -->
          {:else}
            <!--
              One line per source, not one block per read. A reader reports
              the item and every comment on it each time it runs, so four
              reads used to print twenty identical links. The line says what
              was read, how often and when; the links follow once; the
              read-by-read history is a disclosure.
            -->
            {#each sources as source (source.key)}
              {@const visibleLinks = showAllLinks
                ? source.links
                : source.links.slice(0, LINK_PREVIEW)}
              <div class="flex flex-wrap items-center gap-x-2 gap-y-1.5">
                <p class="text-meta text-fg" data-evidence-source>
                  <span class="font-medium">{source.name}</span><span
                    class="text-fg-muted"
                    >{` · ${source.reads === 1 ? "1 observation" : `${source.reads} observations`}`}{#if source.lastAt}{" · last "}<time
                        datetime={source.lastAt}
                        title={formatAbsoluteDateTime(source.lastAt)}
                        >{formatTimestamp(source.lastAt)}</time
                      >{/if}</span
                  >
                </p>
                {#if source.latestRead}
                  <SignalBadge tone={observationStatusTone(source.latestRead)}
                    >{observationStatusLabel(source.latestRead)}</SignalBadge
                  >
                {/if}
              </div>
              {#if source.latest && observationErrorText(source.latest) && source.latest.status === "error"}
                <p class="mt-1 break-words text-micro text-warn-text">
                  Last read failed <time
                    datetime={source.latest.observed_at}
                    title={formatAbsoluteDateTime(source.latest.observed_at)}
                    >{formatTimestamp(source.latest.observed_at)}</time
                  >: {humanizeInstants(observationErrorText(source.latest))}
                </p>
              {/if}
              {#if source.latestRead?.uncertainty?.length}
                <ul
                  class="mt-1.5 list-disc break-words pl-5 text-meta text-warn-text"
                >
                  {#each source.latestRead.uncertainty as item}<li>
                      {item}
                    </li>{/each}
                </ul>
              {/if}
              {#if source.links.length}
                <ul
                  class="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-meta"
                  aria-label="Evidence links"
                >
                  {#each visibleLinks as link (link.key)}
                    <li class="min-w-0 break-words">
                      {#if link.ref && isChipRef(link.ref)}<AnxRefChip
                          refValue={link.ref}
                          resolved={planRefs}
                          organizationSlug={$page.params.organization}
                          workspaceSlug={$page.params.workspace}
                          onpreview={(model, anchor) =>
                            refPreview?.open(model, anchor)}
                          onpreviewclose={() => refPreview?.requestClose()}
                        />{:else if link.href}<a
                          class="text-accent-text hover:underline"
                          href={link.href}
                          target="_blank"
                          rel="noreferrer"
                          title={link.href}>{link.label} ↗</a
                        >{:else}<span class="text-fg-muted">{link.label}</span
                        >{/if}
                    </li>
                  {/each}
                  {#if source.links.length > LINK_PREVIEW}
                    <li>
                      <button
                        class="ui-prose-link text-micro"
                        type="button"
                        aria-expanded={showAllLinks}
                        onclick={() => (showAllLinks = !showAllLinks)}
                        >{showAllLinks
                          ? "Show fewer"
                          : `${source.links.length - LINK_PREVIEW} more`}</button
                      >
                    </li>
                  {/if}
                </ul>
              {/if}
            {:else}
              <p class="text-meta text-fg-muted">
                {lastCheckedAt ? "No reads loaded" : `Never read ${sourceName}`}
              </p>
            {/each}
            <div class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-2">
              {#if !nexusOwned}
                <button
                  class="ui-btn-secondary"
                  onclick={refresh}
                  disabled={refreshing || refreshPending}
                  >{refreshing
                    ? "Checking…"
                    : refreshPending
                      ? "Check pending"
                      : `Check ${sourceName} now`}</button
                >
              {/if}
              {#if lastAttemptAt && failedAttempts && !(sources[0]?.latest?.status === "error")}
                <span class="text-micro text-fg-muted"
                  >Last attempt <time
                    datetime={lastAttemptAt}
                    title={formatAbsoluteDateTime(lastAttemptAt)}
                    >{formatTimestamp(lastAttemptAt)}</time
                  >, {failedAttempts} failed</span
                >
              {/if}
            </div>
          {/if}
          {#if refreshError && sources[0]?.latest?.status !== "error"}
            <p class="mt-2 break-words text-micro text-warn-text">
              {refreshError}
            </p>
          {/if}
          {#if notice}<p class="mt-2 text-micro text-fg-muted" role="status">
              {notice}
            </p>{/if}
          {#if evidenceError}<div class="mt-3">
              <StateError
                title="Evidence history unavailable"
                message={evidenceError}
                onretry={() => load()}
              />
            </div>{/if}
          {#if observations.length}
            <details open class="mt-3 text-micro text-fg-muted">
              <summary class="w-fit cursor-pointer"
                >Timeline{nextCursor
                  ? ""
                  : ` (${observations.length})`}</summary
              >
              <ol
                class="mt-2 divide-y divide-line-subtle border-t border-line-subtle"
              >
                {#each observationHistory(observations) as row, index (row.observation.id || index)}
                  {@const observation = row.observation}
                  <li class="flex flex-wrap items-center gap-x-2 gap-y-1 py-2">
                    <SignalBadge tone={observationStatusTone(observation)}
                      >{observationStatusLabel(
                        observation,
                      )}{#if row.count > 1}{" "}× {row.count}{/if}</SignalBadge
                    >
                    <span class="text-micro text-fg-muted">
                      {#if row.count > 1}between <time
                          datetime={row.oldest}
                          title={formatAbsoluteDateTime(row.oldest)}
                          >{formatTimestamp(row.oldest)}</time
                        > and{/if}
                      <time
                        datetime={observation.observed_at}
                        title={formatAbsoluteDateTime(observation.observed_at)}
                        >{formatTimestamp(observation.observed_at) ||
                          "time unknown"}</time
                      >
                    </span>
                    {#if row.group}
                      {#if row.message}<span
                          class="basis-full break-words text-micro text-danger-text"
                          >{humanizeInstants(row.message)}</span
                        >{/if}
                    {:else if observation.evidence?.length}
                      {#each observation.evidence.filter((entry) => entry?.ref && isChipRef(entry.ref)) as entry (entry.ref)}
                        <AnxRefChip
                          refValue={entry.ref}
                          resolved={planRefs}
                          organizationSlug={$page.params.organization}
                          workspaceSlug={$page.params.workspace}
                          onpreview={(model, anchor) =>
                            refPreview?.open(model, anchor)}
                          onpreviewclose={() => refPreview?.requestClose()}
                        />
                      {/each}
                      <span class="text-micro text-fg-subtle"
                        >· {observation.evidence.length === 1
                          ? "1 link"
                          : `${observation.evidence.length} links`}</span
                      >
                    {/if}
                  </li>
                {/each}
              </ol>
              {#if nextCursor}<button
                  class="ui-btn-secondary mt-2"
                  disabled={loading}
                  onclick={loadMore}
                  >{loading ? "Loading…" : "Older observations"}</button
                >{/if}
            </details>
          {/if}
          {#if mirrors.length}
            <details class="mt-2 text-micro text-fg-muted">
              <summary class="w-fit cursor-pointer"
                >Also tracked through {mirrors.length === 1
                  ? "another connection"
                  : `${mirrors.length} other connections`}</summary
              >
              <ul class="mt-2 space-y-1">
                {#each mirrors as mirror (workKey(mirror))}
                  <li class="flex flex-wrap items-center gap-2">
                    <a
                      class="text-accent-text hover:underline"
                      href={workspaceHref(
                        `/initiatives/${encodeURIComponent(workKey(mirror))}`,
                      )}
                      >{mirror.source?.connection_id
                        ? connectionName(mirror.source)
                        : "Other connection"}</a
                    >
                  </li>
                {/each}
              </ul>
            </details>
          {/if}
        </section>
        <!--
          One Details disclosure. Relations, run provenance and the raw reader
          reports are the answers to "prove it" — real, occasionally needed,
          and not what the page is for.
        -->
        {#if work.relations?.length}
          <section data-initiative-linked>
            <h2 class="ui-label">Linked docs and refs</h2>
            <ul class="mt-2 space-y-1 text-meta">
              {#each work.relations as relation}<li
                  class="flex flex-wrap items-center gap-x-2 break-words"
                >
                  <span class="text-fg-muted">{relation.kind}</span>
                  {#if relation.ref && isChipRef(relation.ref)}<AnxRefChip
                      refValue={relation.ref}
                      resolved={planRefs}
                      organizationSlug={$page.params.organization}
                      workspaceSlug={$page.params.workspace}
                      onpreview={(model, anchor) =>
                        refPreview?.open(model, anchor)}
                      onpreviewclose={() => refPreview?.requestClose()}
                    />{:else}<span class="text-fg">{relation.ref}</span>{/if}
                </li>{/each}
            </ul>
          </section>
        {/if}
        {#if hasDetails || observations.length}
          <details class="text-micro text-fg-muted">
            <summary class="w-fit cursor-pointer">Details</summary>
            <div class="mt-3 space-y-5">
              {#if work.executions?.length}
                <section>
                  <h2 class="ui-label">Runs</h2>
                  <ul class="divide-y divide-line-subtle">
                    {#each work.executions as execution}<li
                        class="break-words py-2 text-meta text-fg"
                      >
                        {#if safeSourceHref(execution.url)}<a
                            class="text-accent-text hover:underline"
                            href={safeSourceHref(execution.url)}
                            target="_blank"
                            rel="noreferrer"
                            >{execution.authority} · {execution.run_id} ↗</a
                          >{:else}{execution.authority} · {execution.run_id}{/if}
                        <p class="mt-0.5 text-micro text-fg-muted">
                          {[
                            execution.host,
                            execution.harness,
                            execution.agent,
                            execution.model,
                          ]
                            .filter(Boolean)
                            .join(" · ") ||
                            "Context not reported"}{#if execution.result_ref}
                            · <span class="font-mono"
                              >{execution.result_ref}</span
                            >{/if}
                        </p>
                      </li>{/each}
                  </ul>
                </section>
              {/if}
              {#if observations.length}
                <section>
                  <h2 class="ui-label">Reader reports</h2>
                  <ul class="space-y-3">
                    {#each observations as observation, index (observation.id || index)}
                      <li>
                        <p class="break-words text-micro text-fg-subtle">
                          {observation.reader_id ||
                            "unknown reader"}{#if observation.reader_revision}
                            @{observation.reader_revision}{/if}{#if observation.source_revision}
                            · <span class="font-mono"
                              >{observation.source_revision}</span
                            >{/if}
                        </p>
                        <pre
                          class="mt-1 max-h-72 overflow-auto whitespace-pre-wrap break-words rounded bg-bg-soft p-3 font-mono text-micro">{JSON.stringify(
                            {
                              facts: observation.facts,
                              coverage: observation.coverage,
                              actor_id: observation.actor_id,
                              received_at: observation.received_at,
                              status: observation.status,
                              verification: observation.verification,
                            },
                            null,
                            2,
                          )}</pre>
                      </li>
                    {/each}
                  </ul>
                </section>
              {/if}
            </div>
          </details>
        {/if}
      </div>
      <!-- `break-words` wraps text but does not shrink the column's intrinsic
           minimum, so a long project ref or source id used to widen the whole
           grid past the viewport. -->
      <aside
        class="min-w-0 space-y-6 text-meta"
        aria-label="Source and follow-through"
      >
        <div class="flex items-center justify-between gap-2">
          <h2 class="ui-label mb-0 flex items-center gap-1.5">
            Source{#if !sourceDetailsWorthShowing}<InfoTip
                label="Where this initiative came from"
                text="This initiative was created here and nobody is assigned, so there is no outside tracker or owner to name."
              />{/if}
          </h2>
          <PanelToggle
            label="details"
            side="right"
            collapsed={railCollapsed}
            auto={railAutoCollapsed}
            onToggle={toggleRail}
          />
        </div>
        {#if !railCollapsed}
          <section>
            <dl class="mt-2 space-y-2">
              <!--
              Authority and Owner are the rail's point when a task comes from
              somewhere else. When neither is set — a task created here with
              nobody on it — they are two rows of "Agent Nexus" and "Not
              assigned", which is why they fold away instead.
            -->
              {#if sourceDetailsWorthShowing}
                <div>
                  <dt class="text-micro text-fg-subtle">Authority</dt>
                  <dd class="break-words text-fg">
                    {sourceLabel(work.source)}{#if work.source?.native_id}
                      <span class="text-fg-subtle"> · </span><span
                        class="font-mono text-fg-muted"
                        >{work.source.native_id}</span
                      >{/if}
                  </dd>
                  {#if safeSourceHref(work.source?.url)}<dd>
                      <a
                        class="text-accent-text hover:underline"
                        href={safeSourceHref(work.source.url)}
                        target="_blank"
                        rel="noreferrer"
                        data-task-source-link
                        aria-keyshortcuts="O"
                        title="Open source record (O)">Open source record ↗</a
                      >
                    </dd>{/if}
                </div>
              {/if}
              {#if work.owner}
                <div>
                  <dt class="text-micro text-fg-subtle">Owner</dt>
                  <dd>
                    <ActorLabel
                      label={actorDisplayLabel(
                        work.owner,
                        $actorRegistry,
                        $principalRegistry,
                      )}
                      seed={work.owner}
                      size="xs"
                    />
                  </dd>
                </div>
              {/if}
              {#if work.project_ref}
                <div>
                  <dt class="text-micro text-fg-subtle">Project</dt>
                  <dd class="break-words text-fg">{work.project_ref}</dd>
                </div>
              {/if}
              {#each [["start_at", "Start"], ["due_at", "Due"]] as [field, title]}{#if work[field]}<div
                  >
                    <dt class="text-micro text-fg-subtle">{title}</dt>
                    <dd class="text-fg">
                      {formatAbsoluteDateTime(work[field])}
                    </dd>
                  </div>{/if}{/each}
            </dl>
            {#if work.source?.authority !== "nexus"}
              <p class="mt-2 text-micro text-fg-subtle">
                Status and workflow are owned by the source.
                {#if pmVisible}
                  Ask PM to request a change.
                {:else}
                  A change there has to be requested and approved here first.
                {/if}
              </p>
            {/if}
          </section>
        {/if}
      </aside>
    </div>
  {/if}
</WorkspacePageShell>
<!-- One preview layer for every chip on the page. -->
<AnxRefPreview bind:this={refPreview} />
<!-- Answers from Open decisions wait here for Undo before they send. -->
<InboxUndoToast onUndo={() => undoInboxResponse()} />
