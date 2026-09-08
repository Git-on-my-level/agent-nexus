<script>
  import { onMount } from "svelte";
  import { page } from "$app/stores";
  import { beforeNavigate, goto } from "$app/navigation";
  import { coreClient } from "$lib/coreClient";
  import { initializeAuthSession } from "$lib/authSession";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { workKey, errorMessage } from "$lib/pm/presentation.js";
  import { datetimeLocalToIso } from "$lib/formatDate";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import WorkspacePageHeader from "$lib/components/layout/WorkspacePageHeader.svelte";
  import StateError from "$lib/components/state/StateError.svelte";
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
  let created = $state(false);
  let dirty = $derived(
    !created &&
      Boolean(
        title || summary || criteria || owner || nextActor || nextAction || due,
      ),
  );
  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  beforeNavigate(({ cancel }) => {
    if (saving && !created) {
      cancel();
      return;
    }
    if (dirty && !window.confirm("Leave without saving this commitment?"))
      cancel();
  });
  async function load() {
    loading = true;
    error = "";
    try {
      await initializeAuthSession({
        fetchFn: globalThis.fetch.bind(globalThis),
        workspaceSlug: $page.params.workspace,
        authDriver: "work-create",
      });
      const result = await coreClient.listBoards({ limit: 200 });
      boards = result.boards || [];
    } catch (err) {
      error = errorMessage(err);
    } finally {
      loading = false;
    }
  }
  async function save(event) {
    event.preventDefault();
    if (saving || !title.trim() || !board || !criteria.trim()) return;
    saving = true;
    error = "";
    try {
      const result = await coreClient.createWork({
        board_ref: board,
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
      });
      const key = workKey(result.work || {});
      if (!key)
        throw new Error(
          "The save response did not identify the commitment. Check Work before retrying to avoid a duplicate.",
        );
      created = true;
      await goto(workspaceHref(`/work/${encodeURIComponent(key)}`));
    } catch (err) {
      error = errorMessage(err);
    } finally {
      saving = false;
    }
  }
  onMount(() => {
    void load();
  });
</script>

<svelte:window
  onbeforeunload={(event) => {
    if (dirty) {
      event.preventDefault();
      event.returnValue = "";
    }
  }}
/>
<svelte:head><title>New work · Agent Nexus</title></svelte:head>
<WorkspacePageShell>
  <a
    class="w-fit text-micro text-accent-text hover:underline"
    href={workspaceHref("/work")}>← Work</a
  >
  <WorkspacePageHeader title="New work"
    >{#snippet subtitle()}Say what must be true when it is done.{/snippet}</WorkspacePageHeader
  >
  {#if error}<StateError message={error} />{/if}
  {#if loading}<p role="status" class="text-fg-muted">
      Loading workspace boards…
    </p>{:else if !boards.length}<section
      class="rounded-md border border-line bg-panel p-5"
    >
      <h2 class="text-meta font-semibold text-fg">
        A board gives this commitment a home
      </h2>
      <p class="mt-2 text-meta text-fg-muted">
        Create a workspace board, then return to register the outcome.
      </p>
      <a class="ui-btn-primary mt-3" href={workspaceHref("/boards/new")}
        >Create a board</a
      ><button class="ui-btn-secondary mt-3 ml-2" onclick={load}
        >Reload boards</button
      >
    </section>{:else}
    <form
      class="max-w-3xl space-y-4 rounded-md border border-line bg-panel p-4 sm:p-5"
      onsubmit={save}
      data-anx-save-scope
    >
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
      <label class="block text-micro font-medium text-fg-muted"
        >Board<select class="ui-input mt-1" bind:value={board} required
          ><option value="" disabled>Choose a board</option
          >{#each boards as item}<option
              value={item.ref || item.handle || item.id}
              >{item.title || item.name || item.handle || item.id}</option
            >{/each}</select
        ></label
      >
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
          disabled={saving || !board || !title.trim() || !criteria.trim()}
          data-anx-save-shortcut>{saving ? "Creating…" : "Create work"}</button
        ><a
          class="text-meta text-fg-muted hover:text-fg"
          href={workspaceHref("/work")}>Cancel</a
        >
      </div>
    </form>{/if}
</WorkspacePageShell>
