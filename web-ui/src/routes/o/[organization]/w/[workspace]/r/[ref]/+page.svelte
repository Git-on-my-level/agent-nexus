<script>
  import { untrack } from "svelte";
  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { coreClient } from "$lib/coreClient";
  import { refRouteHref, resolveRefRoute } from "$lib/refRoute.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { usePageTitle } from "$lib/pageTitle.svelte.js";
  import WorkspacePageShell from "$lib/components/layout/WorkspacePageShell.svelte";
  import StateError from "$lib/components/state/StateError.svelte";

  /**
   * `/r/<ref>`: one link for any typed ref. Agents print it; a human opens
   * it and lands on the page that shows the ref.
   *
   * The workspace shell renders this page only once the reader has a
   * session (a signed-out reader is sent to sign in with this path as the
   * return), so the ref is read as the reader, never anonymously. The
   * destination is built in `refRoute.js` and confined to this workspace.
   */
  let organizationSlug = $derived($page.params.organization);
  let workspaceSlug = $derived($page.params.workspace);
  let rawRef = $derived(String($page.params.ref ?? ""));
  let href = $derived(bindWorkspaceHref(organizationSlug, workspaceSlug));

  /** @type {{ status: string, ref?: string, kind?: string, title?: string, error?: unknown }} */
  let result = $state({ status: "resolving" });
  let retrying = $state(false);
  let ticket = 0;

  usePageTitle(() =>
    result.status === "not_found" ? "Not found" : "Opening link",
  );

  async function open() {
    const mine = ++ticket;
    const ref = rawRef;
    const org = organizationSlug;
    const ws = workspaceSlug;
    const next = await resolveRefRoute(ref, coreClient);
    if (mine !== ticket) return;
    if (next.status === "redirect") {
      const target = refRouteHref(org, ws, next.path, {
        origin: window.location.origin,
      });
      if (target) {
        await goto(target, { replaceState: true });
        return;
      }
      result = { status: "not_found", ref: next.ref };
      return;
    }
    result = next;
  }

  async function retry() {
    retrying = true;
    try {
      await open();
    } finally {
      retrying = false;
    }
  }

  $effect(() => {
    if (!browser) return;
    // Re-run when the ref or workspace changes; nothing else.
    void rawRef;
    void organizationSlug;
    void workspaceSlug;
    untrack(() => {
      result = { status: "resolving" };
      void open();
    });
  });
</script>

<WorkspacePageShell>
  {#if result.status === "resolving"}
    <div class="py-10 text-center" role="status" data-ref-route="resolving">
      <p class="text-meta text-fg-muted">Opening link…</p>
      <p
        class="mt-1 font-mono text-micro text-fg-muted [overflow-wrap:anywhere]"
      >
        {rawRef}
      </p>
    </div>
  {:else if result.status === "error"}
    <div data-ref-route="error">
      <StateError
        title="This link didn’t open"
        message="We couldn’t reach the workspace. Check your connection and try again."
        {retrying}
        onretry={retry}
      />
    </div>
  {:else if result.status === "no_page"}
    <div
      class="rounded-md border border-line bg-bg-soft px-6 py-8 text-center"
      data-ref-route="no-page"
    >
      <h1 class="text-subtitle text-fg">
        {result.title || "This item"} doesn’t have its own page
      </h1>
      <p
        class="mx-auto mt-1.5 max-w-md font-mono text-meta text-fg-muted [overflow-wrap:anywhere]"
      >
        {result.ref || rawRef}
      </p>
      <a class="ui-prose-link mt-4 inline-block text-meta" href={href("/")}
        >Go to workspace home</a
      >
    </div>
  {:else}
    <div
      class="rounded-md border border-line bg-bg-soft px-6 py-8 text-center"
      data-ref-route="not-found"
    >
      <h1 class="text-subtitle text-fg">We couldn’t find this</h1>
      <p
        class="mx-auto mt-1.5 max-w-md font-mono text-meta text-fg-muted [overflow-wrap:anywhere]"
        data-ref-route-ref
      >
        {result.ref || rawRef}
      </p>
      <p class="mx-auto mt-1.5 max-w-md text-meta text-fg-muted">
        It may have been removed, or you may not have access.
      </p>
      <a class="ui-prose-link mt-4 inline-block text-meta" href={href("/")}
        >Go to workspace home</a
      >
    </div>
  {/if}
</WorkspacePageShell>
