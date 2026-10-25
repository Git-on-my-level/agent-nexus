<script>
  import { onMount, tick, untrack } from "svelte";
  import { page } from "$app/stores";
  import { goto } from "$app/navigation";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";
  import { initializeAuthSession } from "$lib/authSession";
  import { restartSession } from "$lib/workspaceBootstrap";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspaceListFilters from "$lib/components/WorkspaceListFilters.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import SkeletonList from "$lib/components/state/SkeletonList.svelte";
  import WorkViews from "$lib/components/pm/WorkViews.svelte";
  import {
    liveWorkspaceEvents,
    TASK_LIST_EVENT_TYPES,
  } from "$lib/liveWorkspaceEvents.js";
  import {
    CLOSED_PHASES,
    dedupeWorkBySource,
    errorMessage,
    isNexusOwned,
    isSessionExpired,
    label,
    PHASES,
    sourceLabel,
    workFreshness,
    workKey,
  } from "$lib/pm/presentation.js";
  import { pmFeaturesVisible, pmKnownAbsent } from "$lib/pm/onboardingState.js";
  import { pmPresence } from "$lib/pm/presence.js";
  import {
    actorDirectoryIncomplete,
    actorRegistry,
    principalDirectoryIncomplete,
    principalRegistry,
  } from "$lib/actorSession";
  import { startAgentRoster } from "$lib/agentRoster.js";
  import { navIconPath } from "$lib/icons.js";
  import {
    humanActorIdSet,
    humanLiveRefreshDelay,
    isHumanNextActor,
    WORK_PAGE_LIMIT,
    WORK_ROW_CAP,
  } from "$lib/overview.js";
  import { formatShortcut } from "$lib/keyboardHints.js";
  import { openCommandPalette } from "$lib/stores/commandPalette.js";
  import {
    beforeCardRefForInsert,
    boardPhasePeers,
  } from "$lib/workBoardDrag.js";
  import { requestedDecisionMap } from "$lib/taskBoardMove.js";

  import { durableForm } from "$lib/actions/durableForm.js";
  import { localWorkScope } from "$lib/localWorkScope.js";
  import {
    mutationOutbox,
    mutationConfirmations,
  } from "$lib/mutationOutbox.js";
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  import {
    enqueueTaskMove,
    taskMoveDraft,
    taskMoveTarget,
    isTaskMove,
  } from "$lib/taskMoveWork.js";

  import { readerScopeKey } from "$lib/readerScope.js";
  import { createInflightSlot, patchSearchParams } from "$lib/urlState.js";
  import {
    readWorkspaceView,
    writeWorkspaceView,
    onWorkspaceViewsDenied,
  } from "$lib/workspaceViewCache.js";
  import { reliableRead } from "$lib/reliableRead.js";
  import { usePageTitle } from "$lib/pageTitle.svelte.js";
  let listController;
  let pendingLiveRead = null;
  let listCacheKey = $state("");
  let reconnecting = $state(false);
  let records = $state([]),
    loading = $state(true),
    error = $state(""),
    nextCursor = $state(""),
    loaded = $state(false),
    now = $state(Date.now());
  let requested = $state({});
  let boards = $state([]);
  // Two flags, because they answer different questions: `boardsRequested`
  // stops a second request, and `boardsLoaded` says the roles are actually in
  // hand — the Initiatives filter cannot narrow before they are, and "no
  // matching tasks" while they are in flight would be a lie.
  let boardsRequested = false;
  let boardsLoaded = $state(false);
  let decisions = $state([]);
  let decisionsLoaded = $state(false);
  let shortcutsOpen = $state(false);
  let moveError = $state("");
  let sessionExpired = $state(false);
  let moveSessionExpired = $state(false);
  function signInAgain() {
    restartSession({
      organizationSlug: $page.params.organization,
      workspaceSlug: $page.params.workspace,
      hostedMode: $page.data?.shellCapabilities?.mode === "hosted",
      workspaceId: $page.data?.workspace?.workspaceId,
      currentAppPath: "/initiatives",
      search: $page.url.search,
    });
  }
  let moveNotice = $state(null);
  // A pending "done" move waiting for its evidence ref.
  let evidenceFor = $state(null);
  // Cancelling the form must not drop the keyboard on the page body.
  function cancelEvidence() {
    const ref = evidenceFor?.work?.ref;
    evidenceFor = null;
    void tick().then(() => {
      if (!ref) return;
      document
        .querySelector(`[data-work-ref="${CSS.escape(ref)}"][tabindex="0"]`)
        ?.focus();
    });
  }
  let evidenceSuggestions = $state([]);
  let evidenceInput = $state(null);
  let moveNoticeElement = $state(null);
  $effect(() => {
    if (evidenceFor && evidenceInput) evidenceInput.focus();
  });
  async function loadEvidenceSuggestions() {
    try {
      const result = await coreClient.listArtifacts({ limit: 25 });
      const items = Array.isArray(result?.artifacts)
        ? result.artifacts
        : Array.isArray(result?.items)
          ? result.items
          : [];
      const mapped = items
        .map((artifact) => ({
          ref: artifact.ref || (artifact.id ? `artifact:${artifact.id}` : ""),
          id: String(artifact.id ?? ""),
          name:
            artifact.title ||
            artifact.summary ||
            artifact.name ||
            artifact.filename ||
            [artifact.kind, artifact.media_type].filter(Boolean).join(" · ") ||
            "",
          when: artifact.created_at ? formatTimestamp(artifact.created_at) : "",
        }))
        .filter((entry) => entry.ref);
      // Three "Vertical Slice Brief" rows are not a choice; say when each was
      // made, and add the id's head when even that repeats.
      const seen = new Map();
      for (const entry of mapped)
        seen.set(entry.name, (seen.get(entry.name) || 0) + 1);
      evidenceSuggestions = mapped.map((entry) => ({
        ref: entry.ref,
        title: [
          entry.name,
          entry.when,
          seen.get(entry.name) > 1 && entry.id ? entry.id.slice(0, 8) : "",
        ]
          .filter(Boolean)
          .join(" · "),
      }));
    } catch {
      evidenceSuggestions = [];
    }
  }
  let requestId = 0;
  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  // With no PM agent onboarded, moving source-owned work still files a
  // request for the reader to approve; only the PM wording and the PM
  // affordances go away.
  let workspaceSlug = $derived($page.data?.workspace?.slug ?? "");
  let pmState = $derived(
    $pmPresence.workspace === workspaceSlug ? $pmPresence : null,
  );
  // Gates the PM affordances and wording only, never a read.
  let pmVisible = $derived(pmFeaturesVisible(pmState));
  /*
   * Refusing the source-owned move needs the opposite test, not the negation:
   * while presence is still loading neither is true, and refusing then would
   * tell a workspace that has a PM that it has none. Core is the authority.
   */
  let pmAbsent = $derived(pmKnownAbsent(pmState));
  // The swimlane board lives in Watch; `?view=lanes` redirects there
  // (`+page.js`), so this page has one board, by placement.
  let view = $derived(
    $page.url.searchParams.get("view") === "board"
      ? $page.url.searchParams.get("view")
      : "table",
  );
  let filters = $derived(
    Object.fromEntries(
      ["q", "source", "project_ref", "owner", "phase", "freshness"].map(
        (key) => [key, $page.url.searchParams.get(key) || ""],
      ),
    ),
  );
  let filterKey = $derived(JSON.stringify(filters));
  // `human=1` is not a core filter. Overview links here for tasks whose
  // next actor is a person; this page reads enough rows to apply it locally.
  let humanOnly = $derived($page.url.searchParams.get("human") === "1");
  // Neither is a core filter: both narrow the rows this page read.
  let initiativesOnly = $derived(
    $page.url.searchParams.get("initiatives") === "1",
  );
  let humanIds = $derived(humanActorIdSet($actorRegistry, $principalRegistry));
  let peopleIncomplete = $derived(
    $actorDirectoryIncomplete || $principalDirectoryIncomplete,
  );
  // One row per source item: a GitHub issue read through two connections is
  // one task to the operator. The folded rows stay reachable from the kept
  // task's page.
  /** The Initiatives filter is on but the board roles have not landed yet. */
  let initiativesPending = $derived(initiativesOnly && !boardsLoaded);
  let visible = $derived.by(() => {
    if (initiativesPending) return [];
    let rows = dedupeWorkBySource(records).records;
    if (humanOnly)
      rows = rows.filter((work) => isHumanNextActor(work, humanIds));
    if (initiativesOnly) {
      rows = rows.filter((work) =>
        initiativeBoardRefs.has(String(work?.board_ref ?? "").trim()),
      );
    }
    return rows;
  });
  // Done and Cancelled fold under a toggle unless the operator asked for them.
  let showClosed = $derived(
    $page.url.searchParams.get("closed") === "1" ||
      CLOSED_PHASES.has(filters.phase),
  );
  let activeFilters = $derived(
    humanOnly || initiativesOnly || Object.values(filters).some(Boolean),
  );
  /*
   * "N marked blocked", not "N blocked". The link opens `?phase=blocked`,
   * which core filters by the stored phase, so the count has to describe the
   * same rows the filter will return — and the word says which of the two
   * things a row's status line now distinguishes it is counting.
   */
  let blockedCount = $derived(
    visible.filter((work) => work.phase === "blocked").length,
  );
  // Three separate facts, three separate filters. They used to be blended into
  // one "without fresh evidence" number, which merged "we have never looked"
  // with "the source is down" — two problems with different fixes.
  let neverCheckedCount = $derived(
    visible.filter(
      (work) =>
        !isNexusOwned(work) && workFreshness(work, now).key === "unknown",
    ).length,
  );
  // A failed read whose last good read is still within its window has not
  // cost the reader anything yet; it is not "failing" from the table's view.
  let unreachableCount = $derived(
    visible.filter((work) => {
      const read = workFreshness(work, now);
      return read.key === "error" && !read.kept;
    }).length,
  );
  /*
   * The boards Overview draws its initiatives from.
   *
   * `role=initiatives` is core's own selection — the same board role the
   * Overview reads — so the filter reuses it rather than inventing a second
   * rule for what an initiative is. It filters the rows this page already
   * holds; `work.list` has no board parameter, so like the person filter it
   * reads the full page cap and narrows locally.
   */
  let initiativeBoardRefs = $derived(
    new Set(
      boards
        .map((entry) => entry?.board ?? entry)
        .filter(
          (board) =>
            String(board?.role ?? "")
              .trim()
              .toLowerCase() === "initiatives",
        )
        .flatMap((board) =>
          [board.ref, board.handle, board.id].map((value) =>
            String(value ?? "").trim(),
          ),
        )
        .filter(Boolean),
    ),
  );
  let boardTitles = $derived(
    Object.fromEntries(
      boards
        .map((entry) => entry?.board ?? entry)
        .filter((board) => board && (board.ref || board.handle || board.id))
        .map((board) => [
          board.ref || board.handle || board.id,
          board.title || board.name || board.handle || board.id,
        ]),
    ),
  );
  let filterCount = $derived(
    ["source", "phase", "freshness", "project_ref", "owner"].filter(
      (key) => filters[key],
    ).length +
      (humanOnly ? 1 : 0) +
      (initiativesOnly ? 1 : 0),
  );
  let actions = $state([]);
  let requestedDecisions = $derived(
    requestedDecisionMap(decisions, records, actions),
  );
  let search = $state("");
  $effect(() => {
    search = filters.q;
  });
  /*
   * The Initiatives filter needs the board roles whether or not the list read
   * succeeded; without this a failing list left the filter waiting forever on
   * a request nothing was going to make.
   */
  $effect(() => {
    if (initiativesOnly && !boardsRequested) void loadBoards();
  });
  $effect(() => {
    const key = filterKey;
    // Re-read when a local filter is toggled; neither is part of `filters`,
    // and both need more than the first page to narrow honestly.
    const local = humanOnly || initiativesOnly;
    if (loaded)
      void untrack(() => load(false, JSON.parse(key), { scanAll: local }));
  });

  /*
   * Each live control navigates on its own. Leaving a text field to click a
   * select fires two `goto`s before `$page` updates; the second must patch
   * the in-flight query, not the still-stale URL snapshot.
   */
  const inflightSearch = createInflightSlot();
  $effect(() => {
    void $page.url.search;
    inflightSearch.clear();
  });
  function filterParams(changes) {
    return patchSearchParams(
      inflightSearch.get($page.url.searchParams),
      changes,
    );
  }
  function queryHref(changes) {
    return `${workspaceHref("/initiatives")}?${filterParams(changes)}`;
  }
  function navigateFilters(changes) {
    const params = filterParams(changes);
    inflightSearch.set(params);
    void goto(`${workspaceHref("/initiatives")}?${params}`, {
      keepFocus: true,
      noScroll: true,
    });
  }
  function setFilter(key, value) {
    navigateFilters({ [key]: value });
  }
  const PAGE_SIZE = 50;
  /*
   * Core computes `work_summary` only when asked. Without this every row
   * would fall back to reading plan health out of the separate legacy
   * fields, which is exactly the disagreement with the Overview that one
   * shared summary exists to end. The enrichment is bounded per page by
   * core, not by workspace size.
   */
  const SUMMARY_READ = { summary: 1 };
  async function load(
    append = false,
    query = filters,
    { live = false, scanAll = false } = {},
  ) {
    if (live && loading) {
      pendingLiveRead = { query, scanAll };
      return;
    }
    if (!live) pendingLiveRead = null;
    const scope = readerScopeKey();
    const key = `${scope}:tasks:${JSON.stringify(query)}:${scanAll}`;
    if (!append && listCacheKey !== key) {
      const cached = readWorkspaceView(key);
      records = cached?.records || [];
      nextCursor = cached?.nextCursor || "";
      listCacheKey = key;
    }
    listController?.abort();
    listController = new AbortController();
    const signal = listController.signal;
    const read = (params) =>
      reliableRead(() => coreClient.listWork({ ...params, summary: 1 }), {
        signal,
        cacheScope: scope,
        onRetry: () => {
          reconnecting = true;
        },
      });
    const id = ++requestId;
    loading = true;
    if (!live) error = "";
    try {
      if (scanAll && !append) {
        const rows = [];
        let cursor = "";
        while (rows.length < WORK_ROW_CAP) {
          const result = await read({
            ...query,
            ...SUMMARY_READ,
            limit: Math.min(WORK_PAGE_LIMIT, WORK_ROW_CAP - rows.length),
            cursor: cursor || undefined,
          });
          if (id !== requestId || scope !== readerScopeKey()) return;
          if (!Array.isArray(result.work))
            throw new Error(
              "The workspace returned an invalid work list. Reload to try again.",
            );
          rows.push(...result.work);
          cursor = result.next_cursor || "";
          if (!cursor) break;
        }
        if (id !== requestId || scope !== readerScopeKey()) return;
        const capped = rows.slice(0, WORK_ROW_CAP);
        records = [
          ...new Map(capped.map((work) => [workKey(work), work])).values(),
        ];
        nextCursor = cursor && rows.length >= WORK_ROW_CAP ? cursor : "";
        error = "";
        reconnecting = false;
        writeWorkspaceView(key, { records, nextCursor });
        if (!decisionsLoaded) void loadDecisions();
        if (!boardsRequested) void loadBoards();
        return;
      }
      const result = await read({
        ...query,
        ...SUMMARY_READ,
        // A live re-read keeps as many rows as the operator already paged in.
        limit: live
          ? Math.min(200, Math.max(PAGE_SIZE, records.length))
          : PAGE_SIZE,
        cursor: append ? nextCursor : undefined,
      });
      if (id !== requestId || scope !== readerScopeKey()) return;
      if (!Array.isArray(result.work))
        throw new Error(
          "The workspace returned an invalid work list. Reload to try again.",
        );
      const rows = append ? [...records, ...result.work] : result.work;
      records = [
        ...new Map(rows.map((work) => [workKey(work), work])).values(),
      ];
      nextCursor = result.next_cursor || "";
      error = "";
      reconnecting = false;
      writeWorkspaceView(key, { records, nextCursor });
      if (!decisionsLoaded) void loadDecisions();
      if (!boardsRequested) void loadBoards();
    } catch (err) {
      if (id === requestId && scope === readerScopeKey() && !signal.aborted) {
        reconnecting = false;
        error = errorMessage(err);
        sessionExpired = isSessionExpired(err);
      }
    } finally {
      if (id === requestId) {
        loading = false;
        if (pendingLiveRead && !signal.aborted) {
          const pending = pendingLiveRead;
          pendingLiveRead = null;
          void load(false, pending.query, {
            live: true,
            scanAll: pending.scanAll,
          });
        }
      }
    }
  }
  let boardsRead = 0;
  async function loadBoards() {
    const ticket = ++boardsRead;
    const scope = $localWorkScope;
    boardsRequested = true;
    try {
      const result = await coreClient.listBoards({ limit: 200 });
      if (ticket === boardsRead && scope === $localWorkScope)
        boards = Array.isArray(result?.boards) ? result.boards : [];
    } catch {
      // Fail soft: the Board column falls back to the board_ref slug, and
      // the Initiatives filter says it found no initiatives board.
    } finally {
      if (ticket === boardsRead && scope === $localWorkScope)
        boardsLoaded = true;
    }
  }
  const DECISION_PREFETCH_PAGES = 25;
  /*
   * Read the proposals whatever the PM state says. A PM runs on the reader's
   * own computer, so one can be absent while proposals it filed earlier still
   * wait for a yes — and the "Requested" badge is how the reader finds them.
   * Only the PM affordances are gated.
   */
  async function loadDecisions() {
    decisionsLoaded = true;
    try {
      // Follow the cursor so the Requested badge does not miss a request
      // that landed past the first page. Pages are newest first, so if the
      // ceiling is reached only the oldest requests go unbadged; the Inbox
      // still lists them.
      const items = [];
      let cursor;
      for (let page = 0; page < DECISION_PREFETCH_PAGES; page += 1) {
        const result = await coreClient.listPmDecisions({ limit: 200, cursor });
        items.push(...(Array.isArray(result?.items) ? result.items : []));
        cursor = result?.next_cursor || "";
        if (!cursor) break;
      }
      decisions = items;
      if (cursor)
        console.warn(
          `Requested badges cover the newest ${items.length} decisions; older ones are not badged.`,
        );
      const receipts = [];
      let actionCursor;
      for (let page = 0; page < DECISION_PREFETCH_PAGES; page += 1) {
        const result = await coreClient.listPmActions({
          limit: 200,
          cursor: actionCursor,
        });
        receipts.push(...(Array.isArray(result?.items) ? result.items : []));
        actionCursor = result?.next_cursor || "";
        if (!actionCursor) break;
      }
      actions = receipts;
    } catch {
      // Fail soft: the Requested badge link degrades, the page stays usable.
    }
  }
  function setMoveNotice(notice, { pointer = false } = {}) {
    moveNotice = notice;
    // Keyboard moves land the caret on Undo. Pointer drops leave the board
    // alone — focusing the notice was scrolling the page.
    if (notice && !pointer) {
      void tick().then(() => moveNoticeElement?.focus());
    }
  }
  function boardToken(work) {
    return (
      boards.find((board) =>
        [board.id, board.ref, `board:${board.id}`].includes(
          work.board_ref || work.board_id,
        ),
      )?.updated_at || ""
    );
  }
  let savedMoves = $derived([
    ...$mutationOutbox.filter(isTaskMove),
    ...Object.values($mutationConfirmations)
      .filter(isTaskMove)
      .filter(
        (entry) => !$mutationOutbox.some((row) => row.target === entry.target),
      ),
  ]);
  let movePending = $derived(
    evidenceFor &&
      $mutationOutbox.some(
        (row) => row.target === taskMoveTarget(evidenceFor.work),
      ),
  );
  function restoreMove(value, reason) {
    evidenceFor = value;
    if (reason === "privacy") {
      moveError = "";
      moveNotice = null;
    }
  }
  async function rebaseMove() {
    const draft = evidenceFor;
    const scope = $localWorkScope;
    try {
      const result = await coreClient.getWork(
        draft.work.ref || workKey(draft.work),
        { summary: 1 },
      );
      const work = result.work;
      const board = isNexusOwned(work)
        ? await coreClient.getBoard(work.board_ref || work.board_id)
        : null;
      if (scope !== $localWorkScope || evidenceFor !== draft) return;
      evidenceFor = {
        ...draft,
        work,
        boardToken: (board?.board || board)?.updated_at || "",
        approved: false,
      };
      moveError = "";
    } catch (err) {
      if (scope === $localWorkScope && evidenceFor === draft)
        moveError = errorMessage(err);
    }
  }
  async function moveTask(
    work,
    phase,
    { undo = false, index, pointer = false } = {},
  ) {
    moveError = "";
    moveSessionExpired = false;
    const key = workKey(work);
    const samePhase = (work.phase || "unknown") === phase;
    if ($mutationOutbox.some((entry) => entry.target === taskMoveTarget(work)))
      return;
    if (
      samePhase &&
      (!isNexusOwned(work) ||
        (!Number.isInteger(index) &&
          !Object.hasOwn(evidenceFor || {}, "beforeCardId")))
    )
      return;
    if (!isNexusOwned(work) && pmAbsent) {
      noticeNoPm(sourceLabel(work.source), { pointer });
      return;
    }
    if (
      evidenceFor &&
      (evidenceFor.key !== key ||
        (evidenceFor.phase !== phase && evidenceFor.ref?.trim())) &&
      !$mutationOutbox.some(
        (entry) => entry.target === taskMoveTarget(evidenceFor.work),
      )
    ) {
      moveError = `Finish or discard the saved move for “${evidenceFor.work.title}” before choosing another move.`;
      return;
    }
    let draft =
      evidenceFor?.key === key
        ? {
            ...evidenceFor,
            phase,
            approved: evidenceFor.phase === phase && evidenceFor.approved,
          }
        : taskMoveDraft(work, phase, {
            key,
            ref: "",
            undo,
            pointer,
            boardToken: boardToken(work),
          });
    if (Number.isInteger(index))
      draft = {
        ...draft,
        beforeCardId: beforeCardRefForInsert(
          work,
          boardPhasePeers(dedupeWorkBySource(records).records, phase, key),
          index,
        ),
        pointer,
      };
    evidenceFor = draft;
    if (phase === "done" && !draft.ref?.trim()) {
      void loadEvidenceSuggestions();
      setMoveNotice(null);
      return;
    }
    if (!isNexusOwned(work) && !draft.approved) {
      if (
        !window.confirm(
          `Ask the PM to request moving “${work.title}” to ${label(phase)} at ${sourceLabel(work.source)}? You approve the request in Decide.`,
        )
      )
        return;
      draft = { ...draft, approved: true };
    }
    draft = {
      ...draft,
      resolutionRefs: phase === "done" ? [draft.ref.trim()] : [],
    };
    evidenceFor = draft;
    try {
      await enqueueTaskMove(draft, {
        draftTarget: "tasks:move",
        href: workspaceHref("/initiatives?view=board"),
      });
      setMoveNotice(
        { text: `Saved move to ${label(phase)}. Waiting for confirmation.` },
        { pointer },
      );
    } catch (err) {
      moveError = readableMoveError(err);
      moveSessionExpired = isSessionExpired(err);
    }
  }
  /** The one explanation for a source-owned move with no PM to carry it. */
  function noticeNoPm(source, { pointer = false } = {}) {
    setMoveNotice(
      {
        text: `Changing work owned by ${source} is a request a PM carries out, and this workspace has none. Set one up to request the move.`,
        href: workspaceHref("/pm/setup"),
        hrefLabel: "Set up your PM",
      },
      { pointer },
    );
  }
  // Core's validation names the field; the reader needs the fix.
  function readableMoveError(err) {
    const raw = errorMessage(err);
    const missing = raw.match(/missing or trashed resolution ref "([^"]+)"/i);
    if (missing)
      return `Nothing exists at ${missing[1]}. Evidence must be an existing artifact or event: pick one from the list, or paste the ref of the file or event that proves completion.`;
    if (/must include artifact: or event:/i.test(raw))
      return "Evidence must be an artifact or an event ref (artifact:… or event:…); an initiative ref is not proof.";
    return raw;
  }
  function isTextEntryTarget(target) {
    return (
      target instanceof HTMLElement &&
      (target.isContentEditable ||
        ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName))
    );
  }
  function taskFocusTargets() {
    return view === "board"
      ? [...document.querySelectorAll('[data-work-ref][tabindex="0"]')]
      : [...document.querySelectorAll("tr[data-work-ref] a[href]")];
  }
  function moveTaskFocus(delta) {
    const targets = taskFocusTargets();
    if (!targets.length) return;
    const active = document.activeElement;
    const currentIndex = targets.findIndex(
      (target) => target === active || target.contains(active),
    );
    const nextIndex =
      currentIndex < 0
        ? 0
        : Math.min(targets.length - 1, Math.max(0, currentIndex + delta));
    targets[nextIndex].focus();
  }
  function setViewFromKeyboard(next) {
    if (view === next) return;
    void goto(queryHref({ view: next }), { keepFocus: true, noScroll: true });
  }
  function handleShortcutKeydown(event) {
    if (event.key === "Escape") {
      if (shortcutsOpen) {
        event.preventDefault();
        shortcutsOpen = false;
      }
      return;
    }
    if (
      event.defaultPrevented ||
      event.metaKey ||
      event.ctrlKey ||
      event.altKey ||
      isTextEntryTarget(event.target) ||
      (!shortcutsOpen && document.querySelector('[aria-modal="true"]'))
    )
      return;
    if (shortcutsOpen) {
      if (event.key === "?") {
        event.preventDefault();
        shortcutsOpen = false;
      }
      return;
    }
    if (event.key === "?") {
      event.preventDefault();
      shortcutsOpener = document.activeElement;
      shortcutsOpen = true;
    } else if (event.key === "j") {
      event.preventDefault();
      moveTaskFocus(1);
    } else if (event.key === "k") {
      event.preventDefault();
      moveTaskFocus(-1);
    } else if (event.key === "b") {
      setViewFromKeyboard("board");
    } else if (event.key === "t") {
      setViewFromKeyboard("table");
    } else if (event.key === "l") {
      void goto(workspaceHref("/watch?view=lanes"));
    }
  }
  onMount(() => {
    const confirmedMove = (event) => {
      const entry = event.detail;
      if (entry?.scope !== $localWorkScope || !isTaskMove(entry)) return;
      const { work, phase, undo } = entry.draft;
      // The receipt proves delivery; refreshed reads decide current phase and
      // decision state, which may already have advanced since that receipt.
      void load(false, filters, { live: true });
      void loadBoards();
      void loadDecisions();
      setMoveNotice(
        entry.operation === "cards.move"
          ? {
              text: `Moved “${work.title}” to ${label(phase)}.`,
              undo:
                undo || work.phase === phase
                  ? undefined
                  : async () => {
                      try {
                        const result = await coreClient.getWork(
                          work.ref || workKey(work),
                          { summary: 1 },
                        );
                        if (entry.scope !== $localWorkScope) return;
                        await loadBoards();
                        if (entry.scope !== $localWorkScope) return;
                        await moveTask(result.work, work.phase, { undo: true });
                      } catch (err) {
                        if (entry.scope === $localWorkScope)
                          moveError = errorMessage(err);
                      }
                    },
            }
          : {
              text: `Move request confirmed. Review it in Decide.${entry.result.decision.supersedes ? " This replaced an earlier proposal for this initiative." : ""}`,
              href: workspaceHref("/decide"),
            },
        { pointer: entry.draft.pointer },
      );
    };
    window.addEventListener("anx:mutation-confirmed", confirmedMove);
    const scope = readerScopeKey();
    const stopDenied = onWorkspaceViewsDenied(scope, (denial) => {
      if (scope !== readerScopeKey()) return;
      requestId++;
      listController?.abort();
      records = [];
      nextCursor = "";
      pendingLiveRead = null;
      loading = false;
      reconnecting = false;
      error = errorMessage(denial);
      sessionExpired = isSessionExpired(denial);
    });
    let disposed = false;
    initializeAuthSession({
      fetchFn: globalThis.fetch.bind(globalThis),
      workspaceSlug: $page.params.workspace,
      authDriver: "task-portfolio",
    })
      .then(() => {
        if (!disposed) loaded = true;
      })
      .catch((err) => {
        error = errorMessage(err);
        sessionExpired = isSessionExpired(err);
        loading = false;
      });
    const timer = setInterval(() => {
      now = Date.now();
    }, 30_000);
    // Tasks change under the operator (agents move cards, readers report);
    // the list follows the event stream instead of offering a Reload button.
    let lastHumanLiveRefresh = 0;
    let humanLiveTimer = 0;
    /**
     * Filters applied to rows after they arrive, so a re-read must page to
     * the row cap for them to stay honest.
     *
     * Read from the URL rather than from `humanOnly`/`initiativesOnly`: this
     * runs from a live-event callback and a timer, outside the component's
     * reactive scope.
     */
    function localFiltersActive() {
      const params = $page.url.searchParams;
      return params.get("human") === "1" || params.get("initiatives") === "1";
    }
    function refreshFromLiveEvent() {
      // A drag or an evidence prompt in progress keeps its rows still.
      if (!loaded || evidenceFor) return;
      /*
       * A local filter narrows rows this page read rather than rows core
       * selected, so a live re-read has to page as widely as the first read
       * did. Without this the Initiatives filter re-read only the newest 200
       * tasks on every workspace event and silently dropped every initiative
       * further down the list.
       */
      const scanAll = localFiltersActive();
      if (!scanAll) {
        void load(false, filters, { live: true });
        return;
      }
      const delay = humanLiveRefreshDelay(Date.now(), lastHumanLiveRefresh);
      if (delay === 0) {
        if (humanLiveTimer) {
          clearTimeout(humanLiveTimer);
          humanLiveTimer = 0;
        }
        lastHumanLiveRefresh = Date.now();
        void load(false, filters, { live: true, scanAll: true });
        return;
      }
      if (humanLiveTimer) return;
      humanLiveTimer = setTimeout(() => {
        humanLiveTimer = 0;
        lastHumanLiveRefresh = Date.now();
        if (!loaded || evidenceFor) return;
        if (!localFiltersActive()) return;
        void load(false, filters, { live: true, scanAll: true });
      }, delay);
    }
    const stopLive = liveWorkspaceEvents({
      client: coreClient,
      types: TASK_LIST_EVENT_TYPES,
      onChange: refreshFromLiveEvent,
    });
    /*
     * The Owner column names agents from the roster, so this page holds a
     * reference to the shared roster loader instead of waiting on the nav
     * badge's delayed start. One read per workspace either way — the loader
     * is reference-counted and the shell asks for the same roster — and it
     * is what lets an owner cell stop holding a placeholder: a roster read
     * that answers, even with an error, is an answer.
     */
    const releaseRoster = startAgentRoster($page.params.workspace);
    return () => {
      disposed = true;
      window.removeEventListener("anx:mutation-confirmed", confirmedMove);
      stopDenied();
      listController?.abort();
      requestId++;
      clearInterval(timer);
      clearTimeout(humanLiveTimer);
      stopLive();
      releaseRoster();
    };
  });
  const SOURCES = [
    ["nexus", "Nexus"],
    ["github", "GitHub"],
    ["multica", "Multica"],
    ["git", "Git"],
    ["other", "Other"],
  ];
  const SHORTCUTS = [
    ["Next initiative", ["J"]],
    ["Previous initiative", ["K"]],
    ["Open focused initiative", ["Enter"]],
    ["Move focused initiative to the previous or next placement", ["←", "→"]],
    ["Placement view", ["B"]],
    ["Table view", ["T"]],
    ["Lanes, in Watch", ["L"]],
    ["Go to Decide, Watch, Initiatives or Ask", ["G", "D W I A"]],
    ["Commands and search", [formatShortcut("K")]],
    ["Shortcut help", ["?"]],
    ["Close this help", ["Esc"]],
  ];
  let shortcutsDialog = $state(null);
  let shortcutsWereOpen = false;
  // The "?" button is gone — the overlay is a keyboard surface, opened and
  // closed with "?" — so focus returns to whatever the reader was on.
  let shortcutsOpener = null;
  $effect(() => {
    if (shortcutsOpen) {
      shortcutsDialog?.focus();
      shortcutsWereOpen = true;
    } else if (shortcutsWereOpen) {
      shortcutsWereOpen = false;
      shortcutsOpener?.focus?.();
      shortcutsOpener = null;
    }
  });

  usePageTitle(() => "Initiatives · Agent Nexus");
</script>

<svelte:window onkeydown={handleShortcutKeydown} />
<WorkspacePageShell>
  <WorkspacePageHeader title="Initiatives">
    {#snippet subtitle()}
      {#if !visible.length && loading && !records.length}
        <!-- Holds the counts line so the page does not shift when it lands. -->
        <span
          class="ui-skeleton-reveal inline-block h-2.5 w-32 rounded bg-panel-hover align-middle"
          aria-hidden="true"
        ></span>
      {:else if visible.length}
        <span class="text-fg-muted"
          >{visible.length}{nextCursor ? "+" : ""} tracked</span
        >{#if blockedCount}<span class="mx-1 text-fg-subtle">·</span><a
            class="ui-prose-link text-warn-text"
            href={queryHref({ phase: "blocked" })}
            >{blockedCount}{nextCursor ? "+" : ""} marked blocked</a
          >{/if}{#if neverCheckedCount}<span class="mx-1 text-fg-subtle">·</span
          ><a class="ui-prose-link" href={queryHref({ freshness: "unknown" })}
            >{neverCheckedCount}{nextCursor ? "+" : ""} never checked</a
          >{/if}{#if unreachableCount}<span class="mx-1 text-fg-subtle">·</span
          ><a
            class="ui-prose-link text-warn-text"
            href={workspaceHref("/integrations")}
            >{unreachableCount}{nextCursor ? "+" : ""} failing to read</a
          >{/if}
      {/if}
    {/snippet}
    {#snippet actions()}
      <button
        class="ui-icon-btn"
        onclick={openCommandPalette}
        aria-label="Search workspace"
        aria-keyshortcuts="Meta+K"
        title="Search workspace (⌘K)"
        type="button"
      >
        <svg
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="1.5"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d={navIconPath("search")} />
        </svg>
      </button>
      {#if pmVisible}
        <!-- Below lg the bottom bar carries Ask PM; a second copy here
             pushed the header onto two rows. -->
        <a class="ui-btn-secondary max-lg:hidden" href={workspaceHref("/pm")}
          >Ask PM</a
        >
      {/if}
      <a class="ui-btn-primary" href={workspaceHref("/initiatives/new")}
        >New initiative</a
      >
    {/snippet}
  </WorkspacePageHeader>

  <div class="flex flex-wrap items-center gap-2">
    <form
      class="min-w-48 flex-1"
      onsubmit={(event) => {
        event.preventDefault();
        setFilter("q", search);
      }}
    >
      <label class="sr-only" for="task-search">Search initiatives</label>
      <input
        id="task-search"
        class="ui-input"
        type="search"
        bind:value={search}
        placeholder="Search title, source ID or next action…"
      />
    </form>
    <!-- The placement board is a desktop surface: 18rem columns cannot be
         dragged on a 390px screen, so its toggle is hidden below 640px.
         Lanes is Watch's board; the link goes there. -->
    <nav
      class="flex rounded-md border border-line bg-bg-soft p-0.5"
      aria-label="Initiative view"
    >
      <a
        class="rounded px-2.5 py-1 text-micro font-medium {view === 'table'
          ? 'bg-panel text-fg'
          : 'text-fg-muted'}"
        href={queryHref({ view: "table" })}
        aria-current={view === "table" ? "page" : undefined}>Table</a
      >
      <a
        class="hidden rounded px-2.5 py-1 text-micro font-medium sm:inline {view ===
        'board'
          ? 'bg-panel text-fg'
          : 'text-fg-muted'}"
        href={queryHref({ view: "board" })}
        aria-current={view === "board" ? "page" : undefined}>By placement</a
      >
      <a
        class="rounded px-2.5 py-1 text-micro font-medium text-fg-muted"
        href={workspaceHref("/watch?view=lanes")}
        data-view-lanes>Lanes</a
      >
    </nav>
  </div>

  {#if initiativesOnly && !initiativesPending}
    <p class="text-micro text-fg-muted" data-task-initiatives-filter>
      {initiativeBoardRefs.size
        ? "Showing initiatives only."
        : "No initiatives are marked yet, so there is nothing to show. Mark one to use this filter."}
      <a class="ui-prose-link" href={queryHref({ initiatives: "" })}>Clear</a>
    </p>
  {/if}

  {#if humanOnly}
    <p class="text-micro text-fg-muted" data-task-human-filter>
      Showing tasks whose next actor is a person.
      {#if peopleIncomplete}
        <span class="text-warn-text" data-task-human-partial
          >Next-actor count may be incomplete.</span
        >
      {/if}
      <a class="ui-prose-link" href={queryHref({ human: "" })}>Clear</a>
    </p>
  {/if}

  <WorkspaceListFilters
    activeCount={filterCount}
    toggleTestId="tasks-filters-toggle"
    panelTestId="tasks-filter-panel"
    openWhenFiltered={activeFilters}
    onclear={() => void goto(workspaceHref(`/initiatives?view=${view}`))}
  >
    {#snippet children()}
      <div class="flex items-end gap-2">
        <label class="ui-label mb-0" for="task-filter-source">Source</label>
        <select
          id="task-filter-source"
          class="ui-input w-auto"
          value={filters.source}
          onchange={(event) => setFilter("source", event.currentTarget.value)}
        >
          <option value="">All sources</option>
          {#each SOURCES as [value, title]}<option {value}>{title}</option
            >{/each}
        </select>
      </div>
      <div class="flex items-end gap-2">
        <label class="ui-label mb-0" for="task-filter-phase">Status</label>
        <select
          id="task-filter-phase"
          class="ui-input w-auto"
          value={filters.phase}
          onchange={(event) => setFilter("phase", event.currentTarget.value)}
        >
          <option value="">All statuses</option>
          {#each PHASES as phase}<option value={phase}>{label(phase)}</option
            >{/each}
        </select>
      </div>
      <!--
          Initiatives, as a filter rather than a second view. Overview selects
          its cards by the `initiatives` board role; this reuses that selection,
          so "the initiatives" means one thing in the product.
        -->
      <div class="flex items-end gap-2">
        <label class="ui-label mb-0" for="task-filter-initiatives"
          >Initiatives</label
        >
        <select
          id="task-filter-initiatives"
          class="ui-input w-auto"
          value={initiativesOnly ? "1" : ""}
          onchange={(event) =>
            setFilter("initiatives", event.currentTarget.value)}
        >
          <option value="">All initiatives</option>
          <option value="1">Initiatives only</option>
        </select>
      </div>
      <div class="flex items-end gap-2">
        <label class="ui-label mb-0" for="task-filter-freshness"
          >Freshness</label
        >
        <select
          id="task-filter-freshness"
          class="ui-input w-auto"
          value={filters.freshness}
          onchange={(event) =>
            setFilter("freshness", event.currentTarget.value)}
        >
          <option value="">Any freshness</option>
          <option value="fresh">Checked recently</option>
          <option value="stale">Not checked lately</option>
          <option value="error">Read failing</option>
          <option value="unknown">Never checked</option>
        </select>
      </div>
      <!-- The two free-text filters apply on Enter or on leaving the field,
             like every other control here. They used to need a third click on
             an Apply button that narrowed nothing the selects had not already
             narrowed. A form so Enter still means "that one". -->
      <form
        class="flex flex-wrap items-end gap-2"
        onsubmit={(event) => {
          event.preventDefault();
          const data = new FormData(event.currentTarget);
          navigateFilters({
            project_ref: String(data.get("project_ref") || "").trim(),
            owner: String(data.get("owner") || "").trim(),
          });
        }}
      >
        <div class="flex items-end gap-2">
          <label class="ui-label mb-0" for="task-filter-project-ref"
            >Project reference</label
          >
          <input
            id="task-filter-project-ref"
            name="project_ref"
            class="ui-input w-auto"
            value={filters.project_ref}
            placeholder="project:name"
            onchange={(event) =>
              setFilter("project_ref", event.currentTarget.value.trim())}
          />
        </div>
        <div class="flex items-end gap-2">
          <label class="ui-label mb-0" for="task-filter-owner">Owner</label>
          <input
            id="task-filter-owner"
            name="owner"
            class="ui-input w-auto"
            value={filters.owner}
            placeholder="Owner identifier"
            onchange={(event) =>
              setFilter("owner", event.currentTarget.value.trim())}
          />
        </div>
      </form>
    {/snippet}
  </WorkspaceListFilters>

  <div
    use:durableForm={{
      target: "tasks:move",
      value: evidenceFor,
      read: () => evidenceFor,
      onRestore: restoreMove,
    }}
  >
    {#each savedMoves as entry (entry.key)}
      <MutationSendStatus {entry} />
    {/each}
    {#if evidenceFor}
      <form
        class="flex flex-wrap items-end gap-2 rounded-md bg-bg-soft px-3 py-2 text-meta"
        role="status"
        onsubmit={(event) => {
          event.preventDefault();
          void moveTask(evidenceFor.work, evidenceFor.phase);
        }}
      >
        <!-- A wide enough minimum that the buttons wrap below on a phone instead
             of squeezing the prompt and the field down to a few characters; the
             task title comes from a source, so it can be one long token. -->
        {#if evidenceFor.phase === "done"}
          <label
            class="min-w-56 flex-1 text-micro text-fg-muted [overflow-wrap:anywhere]"
            >Marking “{evidenceFor.work.title}” done needs evidence. Ref of the
            artifact or event that proves it<input
              class="ui-input mt-1"
              bind:value={evidenceFor.ref}
              bind:this={evidenceInput}
              list="done-evidence-suggestions"
              placeholder="artifact:… or event:…"
            /></label
          >
          <datalist id="done-evidence-suggestions">
            {#each evidenceSuggestions as entry (entry.ref)}
              <option value={entry.ref}>{entry.title}</option>
            {/each}
          </datalist>
        {:else}
          <p>
            Saved move: “{evidenceFor.work.title}” to {label(
              evidenceFor.phase,
            )}.
          </p>
        {/if}
        <button
          class="ui-btn-primary"
          type="submit"
          disabled={movePending ||
            (evidenceFor.phase === "done" && !evidenceFor.ref?.trim())}
          >{evidenceFor.phase === "done" ? "Mark done" : "Send move"}</button
        >
        {#if pmVisible}
          <a
            class="ui-prose-link text-micro"
            href={`${workspaceHref("/pm")}?work_ref=${encodeURIComponent(evidenceFor.work.ref || "")}`}
            >Ask the PM instead</a
          >
        {/if}
        <button
          class="ui-prose-link text-micro"
          type="button"
          onclick={cancelEvidence}>Discard draft</button
        >
        <button
          type="button"
          class="ui-prose-link text-micro"
          disabled={movePending}
          onclick={rebaseMove}>Use latest version</button
        >
      </form>
    {/if}
  </div>
  <!--
      In flow, like the evidence form above it. As a floating bar pinned near the
      bottom of the viewport it sat on top of whatever board card happened to be
      there, and stayed there until dismissed.
    -->
  {#if moveNotice}
    <p
      class="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md bg-bg-soft px-3 py-2 text-meta text-fg outline-none"
      role="status"
      tabindex="-1"
      bind:this={moveNoticeElement}
      data-work-move-notice
    >
      <span class="min-w-0 flex-1">{moveNotice.text}</span>
      {#if moveNotice.undo}
        <button
          class="ui-prose-link text-micro"
          type="button"
          onclick={moveNotice.undo}>Undo</button
        >
      {/if}
      {#if moveNotice.href}
        <a class="ui-prose-link text-micro" href={moveNotice.href}
          >{moveNotice.hrefLabel || "Open in Decide"}</a
        >
      {/if}
      <button
        class="ui-prose-link text-micro"
        type="button"
        onclick={() => setMoveNotice(null)}>Dismiss</button
      >
    </p>
  {/if}
  {#if moveError}
    <StateError
      message={moveError}
      onretry={moveSessionExpired ? signInAgain : undefined}
      retryLabel="Sign in again"
    />
  {/if}

  {#if reconnecting}
    <p class="text-micro text-fg-muted" role="status">Reconnecting…</p>
  {/if}
  {#if error}
    <StateError
      title="Initiatives could not be refreshed"
      message={error}
      onretry={sessionExpired ? signInAgain : () => load()}
      retryLabel={sessionExpired ? "Sign in again" : "Retry"}
      retrying={loading}
    />
  {/if}
  {#if records.length && (error || reconnecting)}
    <p class="text-micro text-warn-text">
      Showing the previously loaded records. They may no longer match current
      filters or source state.
    </p>
  {/if}

  {#if (loading && !records.length) || initiativesPending}
    <div aria-busy="true" data-tasks-loading>
      <p class="sr-only" role="status">
        {initiativesPending && !loading
          ? "Reading initiatives…"
          : "Loading initiatives…"}
      </p>
      {#if view === "board"}
        <div
          class="ui-skeleton-reveal flex gap-3 overflow-hidden"
          aria-hidden="true"
        >
          {#each [3, 2, 1, 2] as cards, column (column)}
            <div
              class="flex w-[13.5rem] shrink-0 flex-col gap-2 rounded-md bg-panel p-2.5"
            >
              <span
                class="block h-2.5 w-16 animate-pulse rounded bg-panel-hover"
              ></span>
              {#each Array.from({ length: cards }, (_, index) => index) as index (index)}
                <span
                  class="block h-16 animate-pulse rounded-md border border-line bg-bg-soft"
                ></span>
              {/each}
            </div>
          {/each}
        </div>
      {:else}
        <!-- The table's own frame: header strip, then 40px rows. -->
        <div
          class="ui-skeleton-reveal overflow-hidden rounded-md border border-line"
          aria-hidden="true"
        >
          <div
            class="hidden h-8 items-center gap-6 border-b border-line px-3 sm:flex"
          >
            <span class="block h-2 w-10 rounded bg-panel-hover"></span>
            <span class="ml-[40%] block h-2 w-12 rounded bg-panel-hover"></span>
            <span class="block h-2 w-12 rounded bg-panel-hover"></span>
          </div>
          <SkeletonList
            bare
            rows={6}
            rowClass="h-12 sm:h-10"
            class="bg-panel"
          />
        </div>
      {/if}
    </div>
  {:else if !error && !visible.length}
    <section class="py-14 text-center">
      <h2 class="text-subtitle font-semibold text-fg">
        {activeFilters ? "No matching initiatives" : "Nothing tracked yet"}
      </h2>
      <p class="mx-auto mt-2 max-w-md text-meta text-fg-muted">
        {activeFilters
          ? "Change or clear the filters."
          : "Create an initiative here, or connect a source so its issues show up."}
      </p>
      <a
        class="ui-prose-link mt-4 inline-block text-meta"
        href={activeFilters
          ? workspaceHref("/initiatives")
          : workspaceHref("/initiatives/new")}
        >{activeFilters ? "Clear filters" : "Create initiative"}</a
      >
    </section>
  {:else if visible.length}
    <WorkViews
      records={visible}
      {view}
      {showClosed}
      closedHref={queryHref({ closed: showClosed ? "" : "1" })}
      {workspaceHref}
      {now}
      {requested}
      {requestedDecisions}
      {boardTitles}
      truncated={Boolean(nextCursor)}
      onMove={moveTask}
    />
  {/if}

  {#if nextCursor}
    <button
      class="ui-btn-secondary mx-auto"
      onclick={() => load(true)}
      disabled={loading}>{loading ? "Loading…" : "Load more"}</button
    >
  {/if}
  {#if shortcutsOpen}
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <div
      bind:this={shortcutsDialog}
      class="fixed inset-0 z-50 flex items-start justify-center bg-black/60 px-4 pt-[12vh] outline-none"
      role="dialog"
      aria-modal="true"
      aria-label="Keyboard shortcuts"
      tabindex="-1"
      onclick={(event) => {
        if (event.target === event.currentTarget) shortcutsOpen = false;
      }}
    >
      <div class="w-full max-w-xs rounded-md border border-line bg-panel p-4">
        <h2
          class="text-micro font-semibold uppercase tracking-wide text-fg-muted"
        >
          Keyboard shortcuts
        </h2>
        <dl class="mt-3 space-y-1.5 text-meta">
          {#each SHORTCUTS as [action, keys] (action)}
            <div class="flex items-baseline justify-between gap-6">
              <dt class="text-fg-muted">{action}</dt>
              <dd class="flex gap-1">
                {#each keys as key (key)}
                  <kbd
                    class="rounded border border-line bg-accent-soft px-1.5 py-0.5 font-mono text-micro text-accent-text"
                    >{key}</kbd
                  >
                {/each}
              </dd>
            </div>
          {/each}
        </dl>
      </div>
    </div>
  {/if}
</WorkspacePageShell>
