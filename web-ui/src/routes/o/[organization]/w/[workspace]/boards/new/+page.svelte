<script>
  import { page } from "$app/stores";
  import { goto } from "$app/navigation";
  import { coreClient } from "$lib/coreClient";
  import { resourceRouteSegment } from "$lib/resourceIdentity.js";
  import { workspacePath } from "$lib/workspacePaths";

  let creating = $state(false);
  let createError = $state("");
  let createTitle = $state("");
  let createSummary = $state("");

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);

  function boardsHref() {
    return workspacePath(organizationSlug, workspaceSlug, "/boards");
  }

  async function submit() {
    createError = "";
    const title = createTitle.trim();
    if (!title) {
      createError = "Board title is required.";
      return;
    }
    creating = true;
    try {
      const boardPayload = { title, document_refs: [], pinned_refs: [] };
      const s = createSummary.trim();
      if (s) {
        boardPayload.summary = s;
      }
      const created = await coreClient.createBoard({
        board: boardPayload,
      });
      await goto(
        workspacePath(
          organizationSlug,
          workspaceSlug,
          `/boards/${encodeURIComponent(resourceRouteSegment(created.board, "board"))}`,
        ),
      );
    } catch (e) {
      createError = `Failed to create board: ${e instanceof Error ? e.message : String(e)}`;
    } finally {
      creating = false;
    }
  }
</script>

<div class="mx-auto max-w-lg">
  <div class="mb-6">
    <a
      class="text-micro text-fg-muted transition-colors hover:text-fg"
      href={boardsHref()}
    >
      ← Boards
    </a>
    <h1 class="mt-2 text-subtitle font-semibold text-fg">New board</h1>
    <p class="mt-1 text-micro text-fg-muted">
      Give it a name — you can link topics, docs, and owners after creation.
    </p>
  </div>

  {#if createError}
    <div
      class="mb-4 rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text"
    >
      {createError}
    </div>
  {/if}

  <div class="rounded-md border border-line bg-panel p-5">
    <label class="block text-meta font-medium text-fg">
      Board title
      <input
        bind:value={createTitle}
        class="mt-2 w-full rounded-md border border-line bg-bg-soft px-3 py-2.5 text-meta text-fg focus:border-accent focus:outline-none"
        placeholder="e.g. Q3 launch, Incident response, Onboarding"
        type="text"
        onkeydown={(e) => e.key === "Enter" && submit()}
      />
    </label>
    <label class="mt-4 block text-meta font-medium text-fg">
      Summary
      <textarea
        bind:value={createSummary}
        class="mt-2 w-full resize-y rounded-md border border-line bg-bg-soft px-3 py-2.5 text-meta text-fg focus:border-accent focus:outline-none"
        placeholder="Optional one-line description for lists and the board header"
        rows="2"
      ></textarea>
    </label>

    <div class="mt-4 flex gap-2">
      <button
        class="rounded-md bg-accent-solid px-4 py-2 text-meta font-medium text-white transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-60"
        disabled={creating}
        onclick={submit}
        type="button"
      >
        {creating ? "Creating…" : "Create board"}
      </button>
      <a
        class="rounded-md border border-line bg-panel px-4 py-2 text-meta font-medium text-fg-muted transition-colors hover:bg-line-subtle hover:text-fg"
        href={boardsHref()}
      >
        Cancel
      </a>
    </div>
  </div>
</div>
