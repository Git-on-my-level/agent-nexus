<script>
  import { get } from "svelte/store";
  import { durableForm } from "$lib/actions/durableForm.js";
  import { enqueueAuthoring, mutationOutbox } from "$lib/mutationOutbox.js";
  import { localWorkScope } from "$lib/localWorkScope.js";
  import MutationSendStatus from "$lib/components/MutationSendStatus.svelte";
  import { onMount } from "svelte";
  import { page } from "$app/stores";
  import { goto } from "$app/navigation";
  import { coreClient } from "$lib/coreClient";
  import { initializeAuthSession } from "$lib/authSession";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { workKey, errorMessage } from "$lib/pm/presentation.js";
  import { boardRecordFromBoardsListRow } from "$lib/searchHelpers";
  import { datetimeLocalToIso } from "$lib/formatDate";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
  import { usePageTitle } from "$lib/pageTitle.svelte.js";
  let boards = $state([]),
    loading = $state(true),
    saving = $state(false),
    error = $state("");
  let title = $state(""),
    summary = $state(""),
    board = $state(""),
    criteria = $state(""),
    owner = $state(""),
    nextActor = $state(""),
    nextAction = $state(""),
    due = $state("");
  let chooseBoard = $derived(boards.length > 1);
  const target = "work:new";
  let saved = $derived(
    $mutationOutbox.find((entry) => entry.target === target),
  );
  function snapshot() {
    return {
      title,
      summary,
      board,
      criteria,
      owner,
      nextActor,
      nextAction,
      due,
    };
  }
  function restore(value) {
    title = value?.title ?? "";
    summary = value?.summary ?? "";
    board = value?.board ?? "";
    criteria = value?.criteria ?? "";
    owner = value?.owner ?? "";
    nextActor = value?.nextActor ?? "";
    nextAction = value?.nextAction ?? "";
    due = value?.due ?? "";
  }
  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  async function load() {
    loading = true;
    error = "";
    try {
      await initializeAuthSession({
        fetchFn: globalThis.fetch.bind(globalThis),
        workspaceSlug: $page.params.workspace,
        authDriver: "work-create",
      });
    } catch (err) {
      error = errorMessage(err);
      loading = false;
      return;
    }
    try {
      const result = await coreClient.listBoards({ limit: 200 });
      boards = (result.boards || [])
        .map(boardRecordFromBoardsListRow)
        .filter((item) => item && (item.ref || item.handle || item.id));
    } catch (err) {
      // Non-blocking: core defaults the backing board, so the form still works.
      // Say so anyway — silently dropping the list would silently drop the
      // operator's board choice in a workspace that has several.
      boards = [];
      error = errorMessage(err);
    } finally {
      loading = false;
    }
  }
  async function save(event) {
    event.preventDefault();
    if (saving || saved || !title.trim() || !criteria.trim()) return;
    if (chooseBoard && !board) return;
    saving = true;
    error = "";
    try {
      const payload = {
        title: title.trim(),
        summary: summary.trim(),
        definition_of_done: criteria
          .split("\n")
          .map((line) => line.trim())
          .filter(Boolean),
        owner: owner.trim(),
        next_actor: nextActor.trim(),
        next_action: nextAction.trim(),
        ...(due ? { due_at: datetimeLocalToIso(due) } : {}),
        source: { authority: "nexus" },
        phase: "backlog",
      };
      if (chooseBoard) payload.board_ref = board;
      await enqueueAuthoring({
        operation: "work.create",
        body: payload,
        target,
        draft: snapshot(),
        href: $page.url.pathname,
        title: "Saved task",
      });
    } catch {
      error =
        "Your task is still here. Review the saved work above, or copy it before closing this tab.";
    } finally {
      saving = false;
    }
  }
  onMount(() => {
    void load();
    const confirmed = (event) => {
      const entry = event.detail;
      if (entry.scope !== get(localWorkScope) || entry.target !== target)
        return;
      const key = workKey(entry.result?.work || {});
      if (
        key &&
        (!Object.values(snapshot()).some(Boolean) ||
          JSON.stringify(snapshot()) === JSON.stringify(entry.draft))
      ) {
        void goto(workspaceHref(`/tasks/${encodeURIComponent(key)}`));
      }
    };
    window.addEventListener("anx:mutation-confirmed", confirmed);
    return () =>
      window.removeEventListener("anx:mutation-confirmed", confirmed);
  });

  usePageTitle(() => "New task · Agent Nexus");
</script>

<WorkspacePageShell>
  <a
    class="w-fit text-micro text-accent-text hover:underline"
    href={workspaceHref("/tasks")}>← Tasks</a
  >
  <WorkspacePageHeader title="New task" />
  {#if error}<StateError message={error} />{/if}
  {#if loading}<p role="status" class="text-fg-muted">Loading…</p>{:else}
    <form
      use:durableForm={{
        target,
        value: snapshot(),
        read: snapshot,
        onRestore: restore,
      }}
      class="max-w-3xl space-y-4 rounded-md border border-line bg-panel p-4 sm:p-5"
      onsubmit={save}
      data-anx-save-scope
    >
      <MutationSendStatus {target} />
      <p class="text-micro text-fg-subtle">
        Nexus will own this record. Work that lives in GitHub or Multica shows
        up through its integration instead.
      </p>
      <label class="block text-micro font-medium text-fg-muted"
        >Outcome<input
          class="ui-input mt-1"
          bind:value={title}
          required
          maxlength="500"
          placeholder="What needs to be true when this is done?"
        /></label
      >
      {#if chooseBoard}<label class="block text-micro font-medium text-fg-muted"
          >Board<select class="ui-input mt-1" bind:value={board} required
            ><option value="" disabled>Choose a board</option
            >{#each boards as item}<option
                value={item.ref || item.handle || item.id}
                >{item.title || item.name || item.handle || item.id}</option
              >{/each}</select
          ></label
        >{/if}
      <label class="block text-micro font-medium text-fg-muted"
        >Context<textarea
          class="ui-input mt-1"
          rows="3"
          bind:value={summary}
          placeholder="Scope, motivation, and relevant constraints"
        ></textarea></label
      >
      <label class="block text-micro font-medium text-fg-muted"
        >Acceptance criteria<textarea
          class="ui-input mt-1"
          rows="4"
          bind:value={criteria}
          required
          placeholder="One testable outcome per line"
        ></textarea></label
      >
      <div class="grid gap-3 sm:grid-cols-2">
        <label class="text-micro font-medium text-fg-muted"
          >Accountable owner<input
            class="ui-input mt-1"
            bind:value={owner}
            placeholder="Person or agent"
          /></label
        ><label class="text-micro font-medium text-fg-muted"
          >Next actor<input
            class="ui-input mt-1"
            bind:value={nextActor}
            placeholder="Who acts next?"
          /></label
        >
      </div>
      <label class="block text-micro font-medium text-fg-muted"
        >Next action<input
          class="ui-input mt-1"
          bind:value={nextAction}
          placeholder="The concrete next step"
        /></label
      >
      <label class="block text-micro font-medium text-fg-muted"
        >Due date (optional, local time)<input
          class="ui-input mt-1 sm:max-w-xs"
          type="datetime-local"
          bind:value={due}
        /></label
      >
      <div class="flex flex-wrap items-center gap-3 border-t border-line pt-4">
        <button
          class="ui-btn-primary"
          type="submit"
          disabled={saving ||
            Boolean(saved) ||
            !title.trim() ||
            !criteria.trim() ||
            (chooseBoard && !board)}
          data-anx-save-shortcut>{saving ? "Creating…" : "Create task"}</button
        ><a
          class="text-meta text-fg-muted hover:text-fg"
          href={workspaceHref("/tasks")}>Cancel</a
        >
      </div>
    </form>{/if}
</WorkspacePageShell>
