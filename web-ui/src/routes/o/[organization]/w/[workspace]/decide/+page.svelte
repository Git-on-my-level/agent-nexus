<script>
  import { onMount, tick } from "svelte";
  import { page } from "$app/stores";
  import { goto } from "$app/navigation";
  import { briefStore, mountBrief } from "$lib/briefStore.js";
  import {
    DECISION_KIND_LABELS,
    ageSeconds,
    decisionDeadline,
    decisionsForInitiative,
    formatSpan,
    freshnessLabel,
    sortDecisions,
  } from "$lib/brief.js";
  import {
    inboxResponseOverlay,
    inboxUnsentResponses,
    onInboxResponseCommitted,
    undoInboxResponse,
  } from "$lib/inboxResponseQueue.js";
  import { inboxShortcutAction, otherDialogOpen } from "$lib/inboxShortcuts.js";
  import { modSymbol } from "$lib/keyboardHints.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { usePageTitle } from "$lib/pageTitle.svelte.js";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import SkeletonList from "$lib/components/state/SkeletonList.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import SignalBadge from "$lib/components/pm/SignalBadge.svelte";
  import DecisionCard from "$lib/components/decide/DecisionCard.svelte";
  import InboxUndoToast from "$lib/components/inbox/InboxUndoToast.svelte";
  import KeyboardShortcutsDialog from "$lib/components/KeyboardShortcutsDialog.svelte";

  /**
   * Decide: every decision waiting on the reader, from `brief.v1`, laid out
   * as the old Inbox triage — a list on the left, the selected decision on the
   * right, and keys for everything (J/K, 1–5, R, O, ⌘Z, ?).
   *
   * Answering clears the row at once and moves to the next one; the answer
   * waits behind the undo toast (`inboxResponseQueue.js`), and Undo puts the
   * row back. Order is core's: decide_by, blocked count, priority, then age.
   * `?initiative=<ref>` narrows to one initiative (Watch's badges link here).
   * `?item=<inbox id>` is the selection, so a reload keeps the place.
   */
  usePageTitle(() => "Decide");

  const brief = briefStore();
  let now = $state(Date.now());
  let helpOpen = $state(false);
  let detail = $state(null);
  let listEl = $state(null);

  let href = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  let initiative = $derived($page.url.searchParams.get("initiative") ?? "");
  let selectedParam = $derived($page.url.searchParams.get("item") ?? "");
  let all = $derived(
    initiative
      ? decisionsForInitiative($brief.data, initiative)
      : sortDecisions($brief.data?.decisions ?? []),
  );

  /*
   * A decision the reader just answered leaves the list immediately, the way
   * an inbox clears. It stays gone while its answer waits or once it is
   * recorded; Undo drops the overlay and it comes back in place.
   */
  let answeredIds = $derived(
    new Set([
      ...Object.entries($inboxResponseOverlay ?? {})
        .filter(([, overlay]) => overlay && overlay.status !== "failed")
        .map(([id]) => id),
      ...($inboxUnsentResponses ?? [])
        .filter((entry) => entry.state !== "attention")
        .map((entry) => entry.itemId),
    ]),
  );
  /*
   * Only the response queue decides: a send that fails (or needs the reader,
   * like an unreachable requester) drops back to `failed`/`attention` and the
   * row returns with Retry, Answer without notifying and Edit.
   */
  let decisions = $derived(
    all.filter((decision) => !answeredIds.has(decision.inboxItemId)),
  );

  let selectedId = $state("");
  let selected = $derived(
    decisions.find((decision) => decision.inboxItemId === selectedId) ?? null,
  );
  let selectedIndex = $derived(selected ? decisions.indexOf(selected) : -1);
  // Phone: the list and the reading pane are separate screens. A link to an
  // item that is gone shows the list, never some other decision.
  let showingDetail = $derived(
    Boolean(selectedParam && selected?.inboxItemId === selectedParam),
  );
  /** Wide screens show both panes; a phone shows the pane only when open. */
  let wide = $state(true);
  let paneVisible = $derived(wide || showingDetail);

  let freshness = $derived(freshnessLabel($brief.data?.asOf, now));
  let filterTitle = $derived(
    initiative
      ? ($brief.data?.initiatives ?? []).find((row) => row.ref === initiative)
          ?.title || ""
      : "",
  );
  let proposalCount = $derived(Math.min(5, selected?.proposals?.length ?? 0));

  // Keep a selection: the URL's, else the one we had, else the first row.
  $effect(() => {
    if (!decisions.length) return;
    if (
      selectedParam &&
      decisions.some((d) => d.inboxItemId === selectedParam)
    ) {
      if (selectedId !== selectedParam) selectedId = selectedParam;
      return;
    }
    if (!decisions.some((d) => d.inboxItemId === selectedId))
      selectedId = decisions[0].inboxItemId;
  });

  onMount(() => {
    const query = window.matchMedia?.("(min-width: 1024px)");
    const onWidth = () => (wide = query ? query.matches : true);
    onWidth();
    query?.addEventListener?.("change", onWidth);
    const stop = mountBrief();
    const stopCommitted = onInboxResponseCommitted(() =>
      brief.refresh({ force: true }),
    );
    const clock = setInterval(() => (now = Date.now()), 30_000);
    return () => {
      query?.removeEventListener?.("change", onWidth);
      stopCommitted();
      stop();
      clearInterval(clock);
    };
  });

  /*
   * Core caps the decisions it sends. Once the reader has answered everything
   * loaded, there may be more: say so, never "you're clear". Each committed
   * answer re-reads the brief once (no loop: a read is triggered by a commit,
   * not by what the read returns), which pulls the next page in as answers
   * land. An initiative filter is not paged, so it never waits on this.
   */
  let morePending = $derived(
    !initiative &&
      Boolean($brief.data?.truncated?.decisions) &&
      !decisions.length,
  );

  function itemHref(id) {
    const params = new URLSearchParams($page.url.searchParams);
    if (id) params.set("item", id);
    else params.delete("item");
    const query = params.toString();
    return query ? `?${query}` : "?";
  }

  async function select(index, { open = false } = {}) {
    if (!decisions.length) return;
    const bounded = Math.max(0, Math.min(decisions.length - 1, index));
    selectedId = decisions[bounded].inboxItemId;
    if (open || selectedParam)
      await goto(itemHref(selectedId), {
        replaceState: true,
        keepFocus: true,
        noScroll: true,
      });
    await tick();
    const row = listEl?.querySelector(
      `[data-decide-row="${CSS.escape(selectedId)}"]`,
    );
    row?.scrollIntoView({ block: "nearest" });
    // Focus follows the highlight, so Enter opens the row J/K landed on.
    if (row && listEl?.contains(document.activeElement))
      row.focus({ preventScroll: true });
  }

  /**
   * The row was answered: clear it and move to the one below. A PM answer
   * goes through the durable outbox rather than the undo toast, so its row
   * stays (showing its send status) until core confirms it.
   */
  function onAnswered(decision) {
    if (decision.pmDecision) return;
    const index = decisions.findIndex(
      (row) => row.inboxItemId === decision.inboxItemId,
    );
    const next = decisions[index + 1] ?? decisions[index - 1] ?? null;
    selectedId = next?.inboxItemId ?? "";
    if (selectedParam)
      void goto(itemHref(next ? selectedId : ""), {
        replaceState: true,
        keepFocus: true,
        noScroll: true,
      });
  }

  function onUndo() {
    const restored = undoInboxResponse();
    if (!restored) return;
    selectedId = restored.itemId;
  }

  function openSubject() {
    const ref = selected?.subject?.ref || selected?.blocks?.[0]?.ref || "";
    const match = /^card:(.+)$/.exec(ref);
    if (match) void goto(href(`/initiatives/${encodeURIComponent(match[1])}`));
    else if (selected)
      void goto(href(`/decide/${encodeURIComponent(selected.inboxItemId)}`));
  }

  function onKeydown(event) {
    const action = inboxShortcutAction(event, {
      helpOpen,
      modalOpen: otherDialogOpen(),
    });
    if (!action) {
      if (
        event.key === "c" &&
        !event.metaKey &&
        !event.ctrlKey &&
        !event.altKey &&
        !helpOpen &&
        paneVisible &&
        !otherDialogOpen() &&
        !(
          event.target instanceof HTMLElement &&
          (event.target.isContentEditable ||
            ["INPUT", "TEXTAREA", "SELECT"].includes(event.target.tagName))
        )
      ) {
        event.preventDefault();
        detail?.copy();
      }
      return;
    }
    switch (action.type) {
      case "help":
        event.preventDefault();
        helpOpen = true;
        return;
      case "close-help":
        event.preventDefault();
        helpOpen = false;
        return;
      case "undo":
        event.preventDefault();
        onUndo();
        return;
      case "next":
        event.preventDefault();
        void select(selectedIndex + 1);
        return;
      case "previous":
        event.preventDefault();
        void select(selectedIndex - 1);
        return;
      case "proposal":
        // A hidden pane (a phone on the list) must never answer.
        if (paneVisible && detail?.pressProposal(action.index - 1))
          event.preventDefault();
        return;
      case "clear-choice":
        if (detail?.clearChoice()) event.preventDefault();
        return;
      case "reply":
        if (!paneVisible) return;
        event.preventDefault();
        void detail?.focusReply();
        return;
      case "open":
        if (!paneVisible) return;
        event.preventDefault();
        openSubject();
        return;
    }
  }

  const shortcuts = [
    ["Next decision", ["J"]],
    ["Previous decision", ["K"]],
    ["Choose a suggested response, press again to send", ["1", "–", "5"]],
    ["Write your own answer", ["R"]],
    ["Send the answer", [modSymbol(), "Enter"]],
    ["Open the initiative", ["O"]],
    ["Copy the ref", ["C"]],
    ["Undo the last answer", [modSymbol(), "Z"]],
    ["Shortcut help", ["?"]],
    ["Clear the choice, or close this help", ["Esc"]],
  ];

  function rowWait(decision) {
    const deadline = decisionDeadline(decision, now);
    if (deadline) return { text: deadline.text, long: deadline.overdue };
    const age = ageSeconds(decision.createdAt, now);
    return age === null
      ? null
      : { text: formatSpan(age), long: age > 3 * 86_400 };
  }
</script>

<svelte:window onkeydown={onKeydown} />

<WorkspacePageShell>
  <WorkspacePageHeader title="Decide">
    {#snippet meta()}
      <span class="text-meta text-fg-muted">
        {#if $brief.data}
          <span data-decide-count>{decisions.length}</span>
          {decisions.length === 1 ? "decision" : "decisions"}
          {#if freshness}
            · <span data-source="computed:generated_at">{freshness}</span>
          {/if}
        {/if}
      </span>
    {/snippet}
  </WorkspacePageHeader>

  {#if initiative}
    <p class="text-meta text-fg-muted" data-decide-filter>
      Only decisions for <span class="text-fg"
        >{filterTitle || "this initiative"}</span
      >.
      <a class="ui-prose-link" href="?">Show all</a>
    </p>
  {/if}

  {#if $brief.error && $brief.data}
    <div
      class="flex flex-wrap items-center gap-2 rounded-md border border-warn bg-warn-soft px-3 py-2 text-meta text-fg"
      role="status"
      data-decide-stale
    >
      <span
        >Couldn’t refresh. Showing what loaded {freshness.replace(
          /^as of /,
          "",
        )}.</span
      >
      <button
        type="button"
        class="ui-prose-link"
        onclick={() => brief.refresh({ force: true })}>Retry</button
      >
    </div>
  {/if}

  {#if !$brief.data && $brief.error}
    <StateError
      title="Decisions didn’t load"
      message="Check your connection and try again."
      retrying={$brief.refreshing}
      onretry={() => brief.refresh({ force: true })}
    />
  {:else if !$brief.data}
    <SkeletonList rows={4} />
  {:else if !decisions.length}
    <div
      class="flex flex-col items-center gap-1 rounded-md border border-line-subtle bg-panel px-5 py-12 text-center"
      data-decide-empty
    >
      {#if morePending}
        <p class="text-body font-medium text-fg">More are waiting.</p>
        <p class="text-meta text-fg-muted">
          You answered everything that was loaded.
        </p>
        <button
          type="button"
          class="ui-btn-secondary mt-2"
          disabled={$brief.refreshing}
          onclick={() => brief.refresh({ force: true })}>Load more</button
        >
      {:else}
        <p class="text-body font-medium text-fg">You’re clear.</p>
        <p class="text-meta text-fg-muted">Nothing is waiting on you.</p>
      {/if}
    </div>
  {:else}
    <div
      class="grid overflow-hidden rounded-md border border-line-subtle bg-panel lg:min-h-[30rem] lg:grid-cols-[minmax(16rem,0.85fr)_minmax(0,1.5fr)]"
      data-decide-list
    >
      <section
        class="min-w-0 border-line-subtle lg:border-r {showingDetail
          ? 'hidden lg:block'
          : ''}"
        aria-label="Decisions"
      >
        <ul class="divide-y divide-line-subtle" bind:this={listEl}>
          {#each decisions as decision (decision.ref)}
            {@const wait = rowWait(decision)}
            {@const isSelected = decision.inboxItemId === selectedId}
            <li>
              <a
                class="flex min-h-[52px] min-w-0 flex-col justify-center gap-0.5 border-l-2 px-4 py-2 {isSelected
                  ? 'border-transparent hover:bg-panel-hover lg:border-accent lg:bg-bg-soft'
                  : 'border-transparent hover:bg-panel-hover'}"
                href={itemHref(decision.inboxItemId)}
                data-decide-row={decision.inboxItemId}
                aria-current={isSelected ? "true" : undefined}
                onclick={(event) => {
                  if (event.metaKey || event.ctrlKey || event.shiftKey) return;
                  event.preventDefault();
                  const index = decisions.indexOf(decision);
                  // Wide screens read in the pane; a phone opens the item.
                  void select(index, {
                    open: !window.matchMedia?.("(min-width: 1024px)").matches,
                  });
                }}
              >
                <span class="flex min-w-0 items-center gap-2">
                  <span
                    class="min-w-0 flex-1 truncate text-meta font-medium text-fg"
                    title={decision.title}>{decision.title}</span
                  >
                  {#if decision.batch}
                    <SignalBadge class="shrink-0"
                      >{decision.batch.count} grouped</SignalBadge
                    >
                  {:else if decision.kind !== "ask"}
                    <SignalBadge class="shrink-0"
                      >{DECISION_KIND_LABELS[decision.kind] ??
                        decision.kind}</SignalBadge
                    >
                  {/if}
                  {#if decision.blocksCount}
                    <SignalBadge tone="warn" class="shrink-0"
                      >blocks {decision.blocksCount}</SignalBadge
                    >
                  {/if}
                </span>
                <span
                  class="flex min-w-0 items-center gap-2 text-micro text-fg-muted"
                >
                  <span class="min-w-0 flex-1 truncate"
                    >{#if decision.requestedBy}<span class="text-fg-muted"
                        >{decision.requestedBy.display}</span
                      >{/if}{#if decision.subject?.title}<span
                        class="text-fg-subtle"
                        >{decision.requestedBy ? " · " : ""}{decision.subject
                          .title}</span
                      >{/if}</span
                  >
                  {#if wait}
                    <span
                      class="shrink-0 tabular-nums {wait.long
                        ? 'text-warn-text'
                        : ''}">{wait.text}</span
                    >
                  {/if}
                </span>
              </a>
            </li>
          {/each}
        </ul>
        {#if $brief.data.truncated?.decisions}
          <p
            class="border-t border-line-subtle px-4 py-3 text-micro text-fg-muted"
          >
            More are waiting. Answer these to load the rest.
          </p>
        {/if}
      </section>

      <section
        class="flex min-w-0 flex-col {showingDetail ? '' : 'hidden lg:flex'}"
        aria-label="Selected decision"
      >
        {#if selected}
          <div
            class="flex items-center gap-3 border-b border-line-subtle px-4 py-2 text-micro text-fg-muted"
          >
            <a class="text-accent-text lg:hidden" href={itemHref("")}
              >← Decisions</a
            >
            <span class="hidden lg:inline"
              >{selectedIndex + 1} of {decisions.length}</span
            >
            <button
              type="button"
              class="hover:text-fg disabled:opacity-40"
              disabled={selectedIndex <= 0}
              onclick={() => select(selectedIndex - 1)}
              aria-label="Previous decision">↑</button
            >
            <button
              type="button"
              class="hover:text-fg disabled:opacity-40"
              disabled={selectedIndex >= decisions.length - 1}
              onclick={() => select(selectedIndex + 1)}
              aria-label="Next decision">↓</button
            >
            <a
              class="ml-auto hover:text-fg"
              href={href(`/decide/${encodeURIComponent(selected.inboxItemId)}`)}
              >Open on its own page</a
            >
          </div>
          <div class="min-w-0 flex-1">
            {#key selected.inboxItemId}
              <DecisionCard
                bind:this={detail}
                decision={selected}
                variant="pane"
                {now}
                {onAnswered}
              />
            {/key}
          </div>
          <div
            class="hidden flex-wrap items-center gap-x-3 gap-y-1 border-t border-line-subtle px-4 py-2 text-micro text-fg-subtle lg:flex"
          >
            {#if proposalCount}
              <span
                ><kbd class="decide-kbd"
                  >{proposalCount > 1 ? `1–${proposalCount}` : "1"}</kbd
                > choose, again to send</span
              >
            {/if}
            <span><kbd class="decide-kbd">R</kbd> reply</span>
            <span
              ><kbd class="decide-kbd">J</kbd>/<kbd class="decide-kbd">K</kbd> move</span
            >
            <span><kbd class="decide-kbd">O</kbd> open</span>
            <span><kbd class="decide-kbd">{modSymbol()}Z</kbd> undo</span>
            <button
              type="button"
              class="ml-auto hover:text-fg"
              onclick={() => (helpOpen = true)}
              ><kbd class="decide-kbd">?</kbd> all shortcuts</button
            >
          </div>
        {/if}
      </section>
    </div>
  {/if}
</WorkspacePageShell>

<InboxUndoToast {onUndo} />
<KeyboardShortcutsDialog
  bind:open={helpOpen}
  {shortcuts}
  title="Decide shortcuts"
/>

<style>
  .decide-kbd {
    display: inline-block;
    min-width: 1rem;
    padding: 0 0.25rem;
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    font-family: var(--font-sans);
    font-weight: 500;
    font-size: 11px;
    line-height: 16px;
    text-align: center;
    color: var(--fg-muted);
  }
</style>
