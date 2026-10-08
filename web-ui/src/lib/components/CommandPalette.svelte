<script>
  import { onMount, tick, untrack } from "svelte";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { focusTrap } from "$lib/actions/focusTrap.js";
  import { coreClient } from "$lib/coreClient";
  import {
    actorDisplayLabel,
    actorRegistry,
    principalRegistry,
  } from "$lib/actorSession";
  import { filterActorsForUserSelection } from "$lib/systemActor.js";
  import { settingsNavGroups } from "$lib/navigation";
  import { navIconPath } from "$lib/icons.js";
  import { copyText } from "$lib/clipboard.js";
  import { absoluteUrl } from "$lib/absoluteUrl.js";
  import { modSymbol } from "$lib/keyboardHints.js";
  import { applyTaskPhaseMove } from "$lib/taskBoardMove.js";
  import { searchDocuments, searchWork } from "$lib/searchHelpers";
  import {
    resourceDisplayLabel,
    resourceRouteSegment,
  } from "$lib/resourceIdentity.js";
  import {
    cardIdFromWork,
    dedupeWorkBySource,
    errorMessage,
    isNexusOwned,
    label as phaseLabel,
    PHASES,
    safeSourceHref,
    sourceLabel,
    workKey,
  } from "$lib/pm/presentation.js";
  import {
    GO_TO_SHORTCUTS,
    goToCommands,
    rankCommands,
  } from "$lib/commandPaletteModel.js";
  import { workspacePath } from "$lib/workspacePaths";
  import WorkSummary from "$lib/components/WorkSummary.svelte";
  import { workSummaryModel } from "$lib/workSummary.js";

  /**
   * ⌘K: go anywhere, act on what is on screen, or search.
   *
   * Three kinds of rows, in this order: actions on the task or doc the
   * operator is looking at, "Go to" destinations, then task and doc search
   * results. Every action goes through an existing core call (cards.move,
   * cards.patch, pm decisions), exactly as the page controls do.
   *
   * The palette also owns the global letter shortcuts it advertises, so a
   * shortcut shown here is always one that works: G then O/I/A/T/D anywhere, and
   * on a task M (move), A (assign), O (open source); on a doc E (edit).
   * None of them fire while typing or while a dialog is open.
   */
  let {
    open = $bindable(false),
    organizationSlug = "",
    workspaceSlug = "",
  } = $props();

  let query = $state("");
  /** @type {"" | "move" | "assign"} */
  let subpage = $state("");
  let results = $state({ docs: [], tasks: [] });
  let searching = $state(false);
  let activeIndex = $state(0);
  let inputEl = $state(null);
  let running = $state(false);
  let contextWork = $state(null);
  let contextDoc = $state(null);
  /** @type {{ text: string, tone?: "ok" | "danger", href?: string, hrefLabel?: string } | null} */
  let notice = $state(null);
  let noticeTimer = null;
  let debounceTimer = null;
  let latestRequestId = 0;
  let contextRequestId = 0;

  let workId = $derived(String($page.params?.workId ?? "").trim());
  let documentId = $derived(
    /\/edit\/?$/.test($page.url?.pathname ?? "")
      ? ""
      : String($page.params?.documentId ?? "").trim(),
  );

  function href(path) {
    return workspacePath(organizationSlug, workspaceSlug, path);
  }
  function go(path) {
    close();
    void goto(href(path));
  }

  // Load what the operator is looking at, once per open.
  $effect(() => {
    if (!open) return;
    const task = workId;
    const doc = documentId;
    const ticket = ++contextRequestId;
    const [loadedWork, loadedDoc] = untrack(() => [contextWork, contextDoc]);
    // A task from another page is not this page's context.
    if (loadedWork && loadedWork.ref !== task && workKey(loadedWork) !== task)
      contextWork = null;
    // Re-read the task on every open: an agent may have moved it since.
    if (task)
      void coreClient
        .getWork(task)
        .then((result) => {
          if (ticket === contextRequestId) contextWork = result?.work ?? null;
        })
        .catch(() => {});
    if (!task) contextWork = null;
    if (doc && loadedDoc?.id !== doc)
      void coreClient
        .getDocument(doc)
        .then((result) => {
          if (ticket === contextRequestId)
            contextDoc = result?.document ?? null;
        })
        .catch(() => {});
    if (!doc) contextDoc = null;
  });

  $effect(() => {
    if (open) return;
    query = "";
    subpage = "";
    results = { docs: [], tasks: [] };
    searching = false;
    activeIndex = 0;
    if (debounceTimer) clearTimeout(debounceTimer);
  });

  function close() {
    open = false;
  }

  function flash(next, ms = next?.tone === "danger" ? 8000 : 4000) {
    notice = next;
    clearTimeout(noticeTimer);
    if (next) noticeTimer = setTimeout(() => (notice = null), ms);
  }

  /** Run a command: navigate, copy, or call core, then say what happened. */
  async function run(command) {
    if (!command || running) return;
    if (command.page) {
      subpage = command.page;
      query = "";
      activeIndex = 0;
      await tick();
      inputEl?.focus();
      return;
    }
    if (typeof command.run !== "function") return;
    running = true;
    try {
      const outcome = await command.run();
      if (outcome && typeof outcome === "object" && outcome.text) {
        flash(outcome);
      }
    } catch (err) {
      flash({ text: errorMessage(err), tone: "danger" });
    } finally {
      running = false;
    }
  }

  // ---- Commands on the task or doc in view -------------------------------

  let people = $derived(
    filterActorsForUserSelection($actorRegistry)
      .filter((actor) => actor?.id)
      .map((actor) => ({
        id: String(actor.id),
        name: actorDisplayLabel(actor.id, $actorRegistry, $principalRegistry),
      }))
      .sort((a, b) => a.name.localeCompare(b.name)),
  );

  async function moveContextTask(phase) {
    const work = contextWork;
    close();
    const result = await applyTaskPhaseMove(coreClient, work, phase);
    if (result.kind === "moved") {
      contextWork = { ...work, phase };
      return {
        text: `Moved “${work.title}” to ${phaseLabel(phase)}.`,
        tone: "ok",
      };
    }
    if (result.kind === "requested") {
      return {
        text: `Requested a move to ${phaseLabel(phase)} at ${sourceLabel(work.source)}. It waits in Inbox for a yes.`,
        href: result.decision?.id
          ? href(
              `/inbox?item=decision:${encodeURIComponent(result.decision.id)}`,
            )
          : "",
        hrefLabel: "Open in Inbox",
      };
    }
    return null;
  }

  async function assignContextTask(actorId, name) {
    close();
    // Patch against the card as it is now, not as it was when the palette
    // opened: the write is fenced on its updated_at.
    const work = (await coreClient.getWork(workKey(contextWork)))?.work;
    if (!work) throw new Error("This task is no longer available.");
    const ref = `actor:${actorId}`;
    const current = Array.isArray(work.assignee_refs) ? work.assignee_refs : [];
    // The owner is the first assignee; assigning puts this person first and
    // keeps anyone else already on the task.
    const assignees = [ref, ...current.filter((entry) => entry !== ref)];
    await coreClient.updateBoardCard(
      String(work.board_ref || ""),
      cardIdFromWork(work),
      {
        patch: { assignee_refs: assignees },
        if_updated_at: work.updated_at,
      },
    );
    contextWork = { ...work, assignee_refs: assignees, owner: ref };
    return { text: `Assigned “${work.title}” to ${name}.`, tone: "ok" };
  }

  async function copy(value, what) {
    close();
    if (await copyText(value)) return { text: `Copied ${what}.`, tone: "ok" };
    return { text: `Could not copy the ${what}.`, tone: "danger" };
  }

  /** Phases a task can be moved to from here. Done needs evidence, which the
   * board's evidence form collects; the palette does not guess a ref. */
  function moveTargets(work) {
    const current = work?.phase || "unknown";
    return PHASES.filter(
      (phase) => !["unknown", "done", current].includes(phase),
    );
  }

  let taskCommands = $derived.by(() => {
    const work = contextWork;
    if (!work || !workId) return { root: [], move: [], assign: [] };
    const group = `Actions on “${work.title || "this task"}”`;
    const owned = isNexusOwned(work);
    const source = sourceLabel(work.source);
    const move = moveTargets(work).map((phase) => ({
      id: `move:${phase}`,
      group,
      label: owned
        ? `Move to ${phaseLabel(phase)}`
        : `Request move to ${phaseLabel(phase)} at ${source}`,
      keywords: ["status", "phase"],
      icon: "move",
      run: () => moveContextTask(phase),
    }));
    const ownerId = String(work.owner ?? "").replace(/^actor:/, "");
    const assign = owned
      ? people
          .filter((person) => person.id !== ownerId)
          .map((person) => ({
            id: `assign:${person.id}`,
            group,
            label: `Assign to ${person.name}`,
            keywords: ["owner"],
            icon: "persona",
            run: () => assignContextTask(person.id, person.name),
          }))
      : [];
    const sourceHref = safeSourceHref(work.source?.url);
    const link = absoluteUrl(
      href(`/tasks/${encodeURIComponent(workKey(work))}`),
    );
    const root = [
      {
        id: "task:move",
        group,
        label: "Move to…",
        keywords: ["status", "phase"],
        shortcut: ["M"],
        icon: "move",
        page: "move",
      },
      ...(owned
        ? [
            {
              id: "task:assign",
              group,
              label: "Assign to…",
              keywords: ["owner"],
              shortcut: ["A"],
              icon: "persona",
              page: "assign",
            },
          ]
        : []),
      ...(sourceHref
        ? [
            {
              id: "task:open-source",
              group,
              label: `Open in ${source}`,
              keywords: ["source", "external"],
              shortcut: ["O"],
              icon: "external",
              run: () => {
                close();
                window.open(sourceHref, "_blank", "noopener,noreferrer");
              },
            },
          ]
        : []),
      {
        id: "task:copy-link",
        group,
        label: "Copy link",
        keywords: ["url", "share"],
        icon: "chain",
        run: () => copy(link, "link"),
      },
      {
        id: "task:copy-ref",
        group,
        label: "Copy ref",
        keywords: ["id", "cli"],
        icon: "copy",
        run: () => copy(work.ref || workKey(work), "ref"),
      },
      {
        id: "task:ask-pm",
        group,
        label: "Ask PM about this task",
        keywords: ["pm"],
        icon: "askPm",
        run: () =>
          go(`/pm?work_ref=${encodeURIComponent(work.ref || workKey(work))}`),
      },
    ];
    return { root, move, assign };
  });

  let docCommands = $derived.by(() => {
    const doc = contextDoc;
    if (!doc || !documentId) return [];
    const group = `Actions on “${resourceDisplayLabel(doc)}”`;
    const segment = encodeURIComponent(resourceRouteSegment(doc, "document"));
    return [
      {
        id: "doc:edit",
        group,
        label: "Edit doc",
        keywords: ["revise", "write"],
        shortcut: ["E"],
        icon: "edit",
        run: () => go(`/docs/${segment}/edit`),
      },
      {
        id: "doc:copy-link",
        group,
        label: "Copy link",
        keywords: ["url", "share"],
        icon: "chain",
        run: () => copy(absoluteUrl(href(`/docs/${segment}`)), "link"),
      },
      ...(doc.ref
        ? [
            {
              id: "doc:copy-ref",
              group,
              label: "Copy ref",
              keywords: ["id", "cli"],
              icon: "copy",
              run: () => copy(doc.ref, "ref"),
            },
          ]
        : []),
    ];
  });

  let destinations = $derived(
    goToCommands({ settingsGroups: settingsNavGroups, go, mod: modSymbol() }),
  );

  // ---- Search -------------------------------------------------------------

  function handleInput(event) {
    query = event.currentTarget.value;
    activeIndex = 0;
    if (!subpage) debouncedSearch(query);
  }

  function debouncedSearch(q) {
    if (debounceTimer) clearTimeout(debounceTimer);
    const trimmed = q.trim();
    if (trimmed.length < 2) {
      latestRequestId += 1;
      results = { docs: [], tasks: [] };
      searching = false;
      return;
    }
    searching = true;
    debounceTimer = setTimeout(() => executeSearch(trimmed), 250);
  }

  async function executeSearch(q) {
    const requestId = ++latestRequestId;
    try {
      const [docs, tasks] = await Promise.allSettled([
        searchDocuments(q, 5),
        searchWork(q, 8),
      ]);
      if (requestId !== latestRequestId) return;
      results = {
        docs: docs.status === "fulfilled" ? docs.value : [],
        tasks:
          tasks.status === "fulfilled"
            ? dedupeWorkBySource(tasks.value).records.slice(0, 5)
            : [],
      };
    } finally {
      if (requestId === latestRequestId) searching = false;
    }
  }

  let searchCommands = $derived([
    ...results.tasks.map((work) => ({
      id: `result:task:${workKey(work)}`,
      group: "Tasks",
      kind: "task",
      label: work.title || "Untitled task",
      /*
       * A result's state is the shared summary, not a phase this row looked
       * up for itself: a search hit used to read "In progress" for a card the
       * Tasks table called blocked. The source stays a plain subtitle — it is
       * where the card lives, not what state it is in.
       */
      summary: workSummaryModel(work),
      subtitle:
        work.source && !isNexusOwned(work) ? sourceLabel(work.source) : "",
      icon: "tasks",
      run: () => go(`/tasks/${encodeURIComponent(workKey(work))}`),
    })),
    ...results.docs.map((doc) => ({
      id: `result:doc:${doc.id ?? resourceRouteSegment(doc, "document")}`,
      group: "Docs",
      kind: "doc",
      label: doc.title || resourceDisplayLabel(doc),
      // "active" is every doc's state; only a different one is news.
      subtitle: [
        doc.state && doc.state !== "active" ? doc.state : "",
        String(doc.summary ?? "").trim(),
        doc.head_revision_number ? `v${doc.head_revision_number}` : "",
      ]
        .filter(Boolean)
        .join(" · "),
      icon: "docs",
      run: () =>
        go(
          `/docs/${encodeURIComponent(resourceRouteSegment(doc, "document"))}`,
        ),
    })),
  ]);

  // ---- The visible list ---------------------------------------------------

  const SUBPAGE_TITLES = { move: "Move to", assign: "Assign to" };

  // Glyphs for actions; destinations reuse the sidebar's nav icons.
  const ACTION_ICONS = {
    move: "M12 3a9 9 0 100 18 9 9 0 000-18zm-1.5 5.25L14.25 12l-3.75 3.75",
    copy: "M15.75 17.25v2.25c0 .621-.504 1.125-1.125 1.125h-9.75A1.125 1.125 0 013.75 19.5V9.375c0-.621.504-1.125 1.125-1.125h2.25m8.625 9V6.375c0-.621-.504-1.125-1.125-1.125h-9.75",
    chain:
      "M13.19 8.688a4.5 4.5 0 011.242 7.244l-4.5 4.5a4.5 4.5 0 01-6.364-6.364l1.757-1.757m13.35-.709l1.414-1.414a4.5 4.5 0 00-6.364-6.364l-4.5 4.5a4.5 4.5 0 001.242 7.244",
    external:
      "M13.5 6H5.25A2.25 2.25 0 003 8.25v10.5A2.25 2.25 0 005.25 21h10.5A2.25 2.25 0 0018 18.75V10.5m-10.5 6L21 3m0 0h-5.25M21 3v5.25",
    edit: "M16.862 4.487l1.687-1.688a1.875 1.875 0 112.652 2.652L10.582 16.07a4.5 4.5 0 01-1.897 1.13L6 18l.8-2.685a4.5 4.5 0 011.13-1.897l8.932-8.931z",
  };
  function iconPath(key) {
    return ACTION_ICONS[key] || navIconPath(key || "search");
  }

  let items = $derived.by(() => {
    if (subpage === "move") return rankCommands(taskCommands.move, query);
    if (subpage === "assign") return rankCommands(taskCommands.assign, query);
    const trimmed = query.trim();
    const context = trimmed
      ? [
          ...taskCommands.root,
          ...taskCommands.move,
          ...taskCommands.assign,
          ...docCommands,
        ]
      : [...taskCommands.root, ...docCommands];
    // Search results are already ranked by core; they follow the commands.
    return [
      ...rankCommands([...context, ...destinations], trimmed),
      ...searchCommands,
    ];
  });

  /** Rows with a group header before each new group. */
  let rows = $derived.by(() => {
    const out = [];
    let lastGroup = null;
    items.forEach((command, index) => {
      if (command.group !== lastGroup) {
        out.push({ header: command.group, key: `h:${command.group}` });
        lastGroup = command.group;
      }
      out.push({ command, index, key: command.id });
    });
    return out;
  });

  $effect(() => {
    // Keep the highlight on a row that exists.
    if (activeIndex > items.length - 1)
      activeIndex = Math.max(0, items.length - 1);
  });

  function scrollToActive() {
    void tick().then(() =>
      document
        .querySelector(`[data-cmd-index="${activeIndex}"]`)
        ?.scrollIntoView({ block: "nearest" }),
    );
  }

  function handleKeydown(event) {
    if (event.key === "Escape") {
      event.preventDefault();
      if (subpage) {
        subpage = "";
        query = "";
        activeIndex = 0;
      } else close();
      return;
    }
    if (event.key === "Backspace" && subpage && !query) {
      event.preventDefault();
      subpage = "";
      activeIndex = 0;
      return;
    }
    if (!items.length) return;
    if (event.key === "ArrowDown" || (event.ctrlKey && event.key === "n")) {
      event.preventDefault();
      activeIndex = Math.min(activeIndex + 1, items.length - 1);
      scrollToActive();
    } else if (
      event.key === "ArrowUp" ||
      (event.ctrlKey && event.key === "p")
    ) {
      event.preventDefault();
      activeIndex = Math.max(activeIndex - 1, 0);
      scrollToActive();
    } else if (event.key === "Enter") {
      event.preventDefault();
      void run(items[activeIndex]);
    }
  }

  function handleBackdropClick(event) {
    if (event.target === event.currentTarget) close();
  }

  // `focusTrap` on the dialog owns focus: it moves focus in on open, cycles
  // Tab inside, and restores the opener (or the fallback below) on close.
  function fallbackFocus() {
    return (
      document.querySelector('[aria-keyshortcuts="Meta+K"]') ||
      document.querySelector("main")
    );
  }

  // ---- Global letter shortcuts -------------------------------------------

  function isTypingTarget(target) {
    return (
      target instanceof HTMLElement &&
      (target.isContentEditable ||
        ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName))
    );
  }

  let pendingG = 0;
  function openAt(nextSubpage) {
    subpage = nextSubpage;
    query = "";
    activeIndex = 0;
    open = true;
  }

  function handleGlobalKeydown(event) {
    if (
      open ||
      event.defaultPrevented ||
      event.metaKey ||
      event.ctrlKey ||
      event.altKey ||
      !organizationSlug ||
      !workspaceSlug ||
      isTypingTarget(event.target) ||
      document.querySelector('[aria-modal="true"]')
    ) {
      pendingG = 0;
      return;
    }
    const key = event.key.toLowerCase();
    if (pendingG && Date.now() - pendingG < 1200) {
      pendingG = 0;
      const target = GO_TO_SHORTCUTS[key];
      if (target) {
        // Captured before page handlers: "G then T" is Tasks, not the
        // Tasks page's "T" for table view.
        event.preventDefault();
        event.stopPropagation();
        void goto(href(target));
      }
      return;
    }
    if (event.shiftKey) return;
    if (key === "g") {
      pendingG = Date.now();
      return;
    }
    if (workId) {
      if (key === "m") {
        event.preventDefault();
        openAt("move");
      } else if (key === "a") {
        event.preventDefault();
        openAt("assign");
      } else if (key === "o") {
        // The page's own source link: a real click, so no popup blocker.
        const link = document.querySelector("[data-task-source-link]");
        if (link instanceof HTMLElement) {
          event.preventDefault();
          link.click();
        }
      }
    } else if (documentId && key === "e") {
      event.preventDefault();
      void goto(href(`/docs/${encodeURIComponent(documentId)}/edit`));
    }
  }

  onMount(() => {
    window.addEventListener("keydown", handleGlobalKeydown, true);
    return () => {
      window.removeEventListener("keydown", handleGlobalKeydown, true);
      clearTimeout(noticeTimer);
      clearTimeout(debounceTimer);
    };
  });

  let placeholder = $derived(
    subpage === "move"
      ? "Move to…"
      : subpage === "assign"
        ? "Assign to…"
        : "Type a command or search…",
  );
  let emptyText = $derived(
    subpage === "assign" && contextWork && !isNexusOwned(contextWork)
      ? `Assignment for this task is owned by ${sourceLabel(contextWork.source)}.`
      : subpage && !contextWork
        ? "Loading this task…"
        : "No matches",
  );
</script>

{#if open}
  <div
    class="cmd-backdrop"
    onclick={handleBackdropClick}
    onkeydown={handleKeydown}
    role="dialog"
    aria-modal="true"
    aria-label="Command palette"
    tabindex="-1"
    use:focusTrap={{ initialFocus: () => inputEl, fallbackFocus }}
  >
    <div class="cmd-modal">
      <div class="cmd-input-wrap">
        {#if subpage}
          <button
            class="cmd-crumb"
            type="button"
            onclick={() => {
              subpage = "";
              query = "";
              inputEl?.focus();
            }}
            title="Back (Backspace)">{SUBPAGE_TITLES[subpage]}</button
          >
        {:else}
          <svg
            class="cmd-search-icon"
            viewBox="0 0 20 20"
            fill="currentColor"
            aria-hidden="true"
          >
            <path
              fill-rule="evenodd"
              d="M9 3.5a5.5 5.5 0 100 11 5.5 5.5 0 000-11zM2 9a7 7 0 1112.452 4.391l3.328 3.329a.75.75 0 11-1.06 1.06l-3.329-3.328A7 7 0 012 9z"
              clip-rule="evenodd"
            />
          </svg>
        {/if}
        <input
          bind:this={inputEl}
          class="cmd-input"
          type="text"
          {placeholder}
          value={query}
          oninput={handleInput}
          spellcheck="false"
          autocomplete="off"
          role="combobox"
          aria-expanded="true"
          aria-autocomplete="list"
          aria-controls="cmd-results"
          aria-activedescendant={items.length
            ? `cmd-option-${activeIndex}`
            : undefined}
        />
        <kbd class="cmd-esc-hint">{subpage ? "Esc back" : "Esc"}</kbd>
      </div>

      <div
        class="cmd-results"
        id="cmd-results"
        role="listbox"
        aria-label="Commands and results"
      >
        {#each rows as row (row.key)}
          {#if row.header}
            <div class="cmd-group-header" role="presentation">
              {row.header}
            </div>
          {:else}
            {@const command = row.command}
            <!-- Keyboard handling lives on the dialog (arrows move, Enter
                 runs); the option is a listbox row, not a second control. -->
            <!-- svelte-ignore a11y_click_events_have_key_events a11y_interactive_supports_focus -->
            <div
              class="cmd-result-row"
              class:cmd-result-row--active={row.index === activeIndex}
              data-cmd-index={row.index}
              data-cmd-id={command.id}
              id={`cmd-option-${row.index}`}
              role="option"
              aria-selected={row.index === activeIndex}
              onclick={() => void run(command)}
              onmousemove={() => (activeIndex = row.index)}
            >
              <svg
                class="cmd-result-icon"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-linecap="round"
                stroke-linejoin="round"
                aria-hidden="true"
              >
                <path d={iconPath(command.icon)} />
              </svg>
              <div class="cmd-result-text">
                <span class="cmd-result-title">{command.label}</span>
                {#if command.summary}
                  <WorkSummary
                    summary={command.summary}
                    density="row"
                    title={command.label}
                    class="cmd-result-summary"
                  />
                {/if}
                {#if command.subtitle}
                  <span class="cmd-result-subtitle">{command.subtitle}</span>
                {/if}
              </div>
              {#if command.hint}
                <span class="cmd-result-hint">{command.hint}</span>
              {/if}
              {#if command.kind}
                <span class="cmd-result-badge"
                  >{command.kind === "task" ? "Task" : "Doc"}</span
                >
              {:else if command.shortcut}
                <span class="cmd-shortcut" aria-hidden="true">
                  {#each command.shortcut as key, keyIndex (keyIndex)}<kbd
                      >{key}</kbd
                    >{/each}
                </span>
              {:else if command.page}
                <span class="cmd-result-hint" aria-hidden="true">›</span>
              {/if}
            </div>
          {/if}
        {/each}
        {#if searching}
          <div class="cmd-status" role="status">Searching…</div>
        {:else if !items.length}
          <div class="cmd-status">{emptyText}</div>
        {/if}
      </div>
      <div class="cmd-footer" aria-hidden="true">
        <span><kbd>↑</kbd><kbd>↓</kbd> move</span>
        <span><kbd>↵</kbd> run</span>
        {#if subpage}<span><kbd>⌫</kbd> back</span>{/if}
        <span><kbd>G</kbd> then <kbd>I</kbd><kbd>T</kbd><kbd>D</kbd> go</span>
      </div>
    </div>
  </div>
{/if}

{#if notice}
  <div class="cmd-notice" class:cmd-notice--danger={notice.tone === "danger"}>
    <span role="status">{notice.text}</span>
    {#if notice.href}
      <a
        class="ui-prose-link"
        href={notice.href}
        onclick={() => (notice = null)}>{notice.hrefLabel || "Open"}</a
      >
    {/if}
    <button
      class="cmd-notice-dismiss"
      type="button"
      aria-label="Dismiss"
      onclick={() => (notice = null)}>×</button
    >
  </div>
{/if}

<style>
  .cmd-backdrop {
    position: fixed;
    inset: 0;
    z-index: 9999;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding-top: 15vh;
    background: rgba(0, 0, 0, 0.6);
    backdrop-filter: blur(2px);
  }

  .cmd-modal {
    width: 560px;
    max-width: calc(100vw - 2rem);
    max-height: min(480px, 70vh);
    display: flex;
    flex-direction: column;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-modal);
    overflow: hidden;
  }

  .cmd-input-wrap {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 14px;
    border-bottom: 1px solid var(--line);
  }

  .cmd-search-icon {
    width: 16px;
    height: 16px;
    flex-shrink: 0;
    color: var(--fg-muted);
  }

  .cmd-crumb {
    flex-shrink: 0;
    padding: 1px 8px;
    font-size: 12px;
    line-height: 18px;
    color: var(--fg);
    background: var(--bg-soft);
    border: 1px solid var(--line);
    border-radius: 4px;
    cursor: pointer;
  }

  .cmd-input {
    flex: 1;
    /* An input's intrinsic minimum width would otherwise push the ESC hint
       out of the row on the narrowest viewports. */
    min-width: 0;
    background: transparent;
    border: none;
    outline: none;
    color: var(--fg);
    font-size: 13px;
    font-family: var(--font-sans);
    line-height: 1.4;
  }

  .cmd-input::placeholder {
    color: var(--fg-subtle);
  }

  .cmd-esc-hint,
  .cmd-shortcut kbd,
  .cmd-footer kbd {
    flex-shrink: 0;
    min-width: 18px;
    padding: 0 5px;
    font-size: 10.5px;
    font-family: var(--font-sans);
    text-align: center;
    color: var(--fg-muted);
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: 3px;
    line-height: 16px;
  }

  .cmd-results {
    flex: 1;
    overflow-y: auto;
    padding: 4px 0 6px;
  }

  .cmd-status {
    padding: 14px;
    text-align: center;
    font-size: 12px;
    color: var(--fg-muted);
  }

  .cmd-group-header {
    padding: 8px 14px 4px;
    font-size: 10.5px;
    font-weight: 600;
    color: var(--fg-subtle);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .cmd-result-row {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 6px 14px;
    background: transparent;
    border: none;
    cursor: pointer;
    text-align: left;
    color: var(--fg-muted);
    font-family: var(--font-sans);
  }

  .cmd-result-row--active {
    background: var(--panel-hover, var(--bg-soft));
    color: var(--fg);
  }

  .cmd-result-row--active .cmd-result-title {
    color: var(--fg);
  }

  .cmd-result-icon {
    width: 15px;
    height: 15px;
    flex-shrink: 0;
    color: var(--fg-subtle);
  }

  .cmd-result-text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .cmd-result-title {
    font-size: 13px;
    line-height: 1.35;
    color: var(--fg-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .cmd-result-subtitle {
    font-size: 12px;
    line-height: 1.3;
    color: var(--fg-subtle);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .cmd-result-hint {
    flex-shrink: 0;
    font-size: 11px;
    color: var(--fg-subtle);
  }

  .cmd-shortcut {
    display: inline-flex;
    flex-shrink: 0;
    gap: 3px;
  }

  /* The summary sits on the subtitle line's own scale. */
  .cmd-result-text :global(.cmd-result-summary) {
    font-size: 11px;
  }
  .cmd-result-badge {
    flex-shrink: 0;
    font-size: 10px;
    padding: 1px 6px;
    border-radius: 3px;
    background: var(--bg);
    border: 1px solid var(--line);
    color: var(--fg-muted);
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }

  .cmd-footer {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 14px;
    padding: 7px 14px;
    border-top: 1px solid var(--line);
    font-size: 11px;
    color: var(--fg-subtle);
  }

  .cmd-footer span {
    display: inline-flex;
    align-items: center;
    gap: 3px;
  }

  .cmd-notice {
    position: fixed;
    left: 50%;
    bottom: 24px;
    z-index: 9999;
    transform: translateX(-50%);
    display: flex;
    align-items: center;
    gap: 10px;
    max-width: calc(100vw - 2rem);
    padding: 7px 8px 7px 12px;
    font-size: 12.5px;
    color: var(--fg);
    background: var(--panel);
    border: 1px solid var(--line-strong, var(--line));
    border-radius: 8px;
    box-shadow: var(--shadow-menu);
  }

  .cmd-notice--danger {
    color: var(--danger-text);
  }

  .cmd-notice-dismiss {
    padding: 0 4px;
    font-size: 15px;
    line-height: 1;
    color: var(--fg-muted);
    background: transparent;
    border: none;
    cursor: pointer;
  }

  .cmd-notice-dismiss:hover {
    color: var(--fg);
  }
</style>
