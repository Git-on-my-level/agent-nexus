<script>
  import { page } from "$app/stores";
  import { goto } from "$app/navigation";
  import { coreClient } from "$lib/coreClient";
  import { workspacePath } from "$lib/workspacePaths";

  let creating = $state(false);
  let createError = $state("");
  let createTitle = $state("");

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
      const created = await coreClient.createBoard({
        board: { title, document_refs: [], pinned_refs: [] },
      });
      await goto(
        workspacePath(
          organizationSlug,
          workspaceSlug,
          `/boards/${created.board.id}`,
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
      class="text-micro text-[var(--fg-muted)] transition-colors hover:text-[var(--fg)]"
      href={boardsHref()}
    >
      ← Boards
    </a>
    <h1 class="mt-2 text-subtitle font-semibold text-[var(--fg)]">New board</h1>
    <p class="mt-1 text-micro text-[var(--fg-muted)]">
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

  <div class="rounded-md border border-[var(--line)] bg-[var(--panel)] p-5">
    <label class="block text-meta font-medium text-[var(--fg)]">
      Board title
      <input
        bind:value={createTitle}
        class="mt-2 w-full rounded-md border border-[var(--line)] bg-[var(--bg-soft)] px-3 py-2.5 text-meta text-[var(--fg)] focus:border-[var(--accent)] focus:outline-none"
        placeholder="e.g. Q3 launch, Incident response, Onboarding"
        type="text"
        onkeydown={(e) => e.key === "Enter" && submit()}
      />
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
        class="rounded-md border border-[var(--line)] bg-[var(--panel)] px-4 py-2 text-meta font-medium text-[var(--fg-muted)] transition-colors hover:bg-[var(--line-subtle)] hover:text-[var(--fg)]"
        href={boardsHref()}
      >
        Cancel
      </a>
    </div>
  </div>
</div>
