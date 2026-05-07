<script>
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { coreClient } from "$lib/coreClient";
  import { resourceRouteSegment } from "$lib/resourceIdentity.js";
  import { workspacePath } from "$lib/workspacePaths";

  let board = $state(/** @type {Record<string, any> | null} */ (null));
  let loading = $state(true);
  let loadError = $state("");
  let titleDraft = $state("");
  let summaryDraft = $state("");
  let saving = $state(false);
  let saveError = $state("");

  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let boardId = $derived(String($page.params.boardId ?? "").trim());
  let boardRouteSegment = $derived(
    resourceRouteSegment(board, "board") || boardId,
  );

  function boardDetailHref() {
    return workspacePath(
      organizationSlug,
      workspaceSlug,
      `/boards/${encodeURIComponent(boardRouteSegment)}`,
    );
  }

  function boardsHref() {
    return workspacePath(organizationSlug, workspaceSlug, "/boards");
  }

  $effect(() => {
    if (!boardId) return;
    loading = true;
    loadError = "";
    void (async () => {
      try {
        const r = await coreClient.getBoard(boardId);
        const b = r?.board ?? r;
        board = b && typeof b === "object" ? b : null;
        if (!board) {
          loadError = "Board not found.";
          return;
        }
        titleDraft = String(board.title ?? "").trim() || boardId;
        summaryDraft = String(board.summary ?? "");
      } catch (e) {
        loadError = `Failed to load board: ${e instanceof Error ? e.message : String(e)}`;
        board = null;
      } finally {
        loading = false;
      }
    })();
  });

  async function submit() {
    if (!board?.id) return;
    const title = titleDraft.trim();
    if (!title) {
      saveError = "Title is required.";
      return;
    }
    saving = true;
    saveError = "";
    try {
      await coreClient.updateBoard(boardId, {
        patch: {
          title,
          summary: summaryDraft.trim(),
        },
        if_updated_at: board.updated_at,
      });
      await goto(boardDetailHref());
    } catch (e) {
      const status = /** @type {{ status?: number }} */ (e)?.status;
      if (status === 409) {
        try {
          const r = await coreClient.getBoard(boardId);
          const b = r?.board ?? r;
          board = b && typeof b === "object" ? b : board;
          if (board) {
            titleDraft = String(board.title ?? "").trim() || boardId;
            summaryDraft = String(board.summary ?? "");
          }
        } catch {
          /* ignore */
        }
        saveError =
          "Board was updated elsewhere. Reloaded the latest values — review and save again.";
      } else {
        saveError = `Save failed: ${e instanceof Error ? e.message : String(e)}`;
      }
    } finally {
      saving = false;
    }
  }
</script>

<div class="mx-auto max-w-lg">
  <div class="mb-6">
    <a
      class="text-micro text-fg-muted transition-colors hover:text-fg"
      href={boardDetailHref()}
    >
      ← Back to board
    </a>
    <h1 class="mt-2 text-subtitle font-semibold text-fg">Board settings</h1>
    <p class="mt-1 text-micro text-fg-muted">
      Update the display title and short description shown on lists and the
      board header.
    </p>
  </div>

  {#if loading}
    <p class="text-micro text-fg-muted">Loading…</p>
  {:else if loadError}
    <div class="rounded-md bg-danger-soft px-3 py-2 text-meta text-danger-text">
      {loadError}
    </div>
    <a class="mt-4 inline-block text-micro text-accent-text" href={boardsHref()}
      >All boards</a
    >
  {:else if board}
    {#if saveError}
      <div
        class="mb-4 rounded-md bg-warn-soft px-3 py-2 text-meta text-warn-text"
      >
        {saveError}
      </div>
    {/if}

    <div class="rounded-md border border-line bg-panel p-5">
      <label class="block text-meta font-medium text-fg">
        Title
        <input
          bind:value={titleDraft}
          class="mt-2 w-full rounded-md border border-line bg-bg-soft px-3 py-2.5 text-meta text-fg focus:border-accent focus:outline-none"
          type="text"
        />
      </label>
      <label class="mt-4 block text-meta font-medium text-fg">
        Summary
        <textarea
          bind:value={summaryDraft}
          class="mt-2 w-full resize-y rounded-md border border-line bg-bg-soft px-3 py-2.5 text-meta text-fg focus:border-accent focus:outline-none"
          placeholder="Optional one-line description for lists and the board header"
          rows="3"
        ></textarea>
      </label>

      <div class="mt-4 flex flex-wrap gap-2">
        <button
          class="rounded-md bg-accent-solid px-4 py-2 text-meta font-medium text-white transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-60"
          disabled={saving}
          onclick={submit}
          type="button"
        >
          {saving ? "Saving…" : "Save"}
        </button>
        <a
          class="rounded-md border border-line bg-panel px-4 py-2 text-meta font-medium text-fg-muted transition-colors hover:bg-line-subtle hover:text-fg"
          href={boardDetailHref()}
        >
          Cancel
        </a>
      </div>
    </div>
  {/if}
</div>
