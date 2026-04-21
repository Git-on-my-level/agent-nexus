<script>
  import { onMount } from "svelte";

  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { get } from "svelte/store";

  import Button from "$lib/components/Button.svelte";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";
  import { hostedSession, loadHostedSession } from "$lib/hosted/session.js";

  let workspaceName = $state("Main");
  let busy = $state(false);
  let message = $state("");
  let ready = $state(false);
  let inputEl = $state(null);
  /** Prevents overlapping async `handleRedirect` runs from `$effect`. */
  let redirectBusy = false;

  const session = $derived($hostedSession);
  const orgs = $derived(session.organizations);
  const activeOrgId = $derived(session.activeOrgId);

  function slugify(input) {
    return String(input ?? "")
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 48);
  }

  async function ensureSession() {
    if (session.phase === "idle" || session.phase === "loading") {
      await loadHostedSession();
    }
  }

  async function hasWorkspaces(orgId) {
    if (!orgId) return false;
    try {
      const res = await hostedCpFetch(
        `workspaces?organization_id=${encodeURIComponent(orgId)}&limit=1`,
      );
      if (!res.ok) return false;
      const body = await res.json();
      return Array.isArray(body.workspaces) && body.workspaces.length > 0;
    } catch {
      return false;
    }
  }

  function currentPathname() {
    if (!browser) {
      return "";
    }
    return get(page).url.pathname;
  }

  async function handleRedirect() {
    if (redirectBusy) {
      return;
    }
    const path = currentPathname();

    if (session.phase === "unauthed") {
      if (path === "/hosted/start" || path.startsWith("/hosted/start/")) {
        return;
      }
      redirectBusy = true;
      try {
        await goto("/hosted/start", { replaceState: true });
      } finally {
        redirectBusy = false;
      }
      return;
    }
    if (session.phase !== "authed") {
      return;
    }

    if (orgs.length === 0) {
      if (
        path === "/hosted/onboarding/organization" ||
        path.startsWith("/hosted/onboarding/organization/")
      ) {
        return;
      }
      redirectBusy = true;
      try {
        await goto("/hosted/onboarding/organization", { replaceState: true });
      } finally {
        redirectBusy = false;
      }
      return;
    }

    if (!activeOrgId) {
      ready = true;
      return;
    }

    redirectBusy = true;
    try {
      if (await hasWorkspaces(activeOrgId)) {
        if (
          path === "/hosted/dashboard" ||
          path.startsWith("/hosted/dashboard/")
        ) {
          return;
        }
        await goto("/hosted/dashboard", { replaceState: true });
      } else {
        ready = true;
      }
    } finally {
      redirectBusy = false;
    }
  }

  onMount(async () => {
    if (!browser) return;
    await ensureSession();
    await handleRedirect();
  });

  $effect(() => {
    if (!browser) return;
    void session.phase;
    void orgs.length;
    void activeOrgId;
    if (ready) return;
    if (session.phase === "authed") {
      void handleRedirect();
    }
  });

  $effect(() => {
    if (inputEl) inputEl.focus();
  });

  async function readError(res) {
    try {
      const j = await res.json();
      return j?.error?.message || j?.error?.code || res.statusText;
    } catch {
      return res.statusText;
    }
  }

  async function submit() {
    message = "";
    const trimmed = workspaceName.trim();
    if (!trimmed) {
      message = "Workspace name is required.";
      return;
    }
    if (trimmed.length > 64) {
      message = "Workspace name must be 64 characters or fewer.";
      return;
    }
    if (!activeOrgId) {
      message = "No active organization found. Please go back and create one.";
      return;
    }
    busy = true;
    try {
      const res = await hostedCpFetch("workspaces", {
        method: "POST",
        body: JSON.stringify({
          organization_id: activeOrgId,
          slug: slugify(trimmed) || "main",
          display_name: trimmed,
        }),
      });
      if (!res.ok) {
        message = await readError(res);
        return;
      }
      const body = await res.json();
      const ws = body.workspace ?? body;
      const slug = ws?.slug;
      if (!slug) {
        message = "Workspace created but no slug was returned.";
        return;
      }
      await goto(`/${slug}/inbox`, { replaceState: true });
    } catch (e) {
      message = e instanceof Error ? e.message : "Failed to create workspace.";
    } finally {
      busy = false;
    }
  }

  function handleKeydown(e) {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      submit();
    }
  }
</script>

<svelte:head>
  <title>Name your first workspace — ANX</title>
</svelte:head>

{#if ready}
  <div class="mx-auto max-w-[520px] py-8">
    <h1 class="text-display text-fg">Name your first workspace</h1>
    <p class="mt-1.5 text-body text-fg-muted">
      A workspace holds one project's topics, boards, artifacts, and inbox. You
      can add more later.
    </p>

    <div class="mt-5 rounded-md border border-line bg-panel p-4">
      <p class="text-body text-fg">
        Most teams start with one workspace per product or codebase. Keeping
        workspaces focused is how the agent stays useful — one workspace, one
        coherent body of work.
      </p>
    </div>

    <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
    <form
      class="mt-5 space-y-3"
      onsubmit={(e) => {
        e.preventDefault();
        submit();
      }}
      onkeydown={handleKeydown}
    >
      <label class="block text-micro text-fg-muted">
        Workspace name
        <input
          bind:this={inputEl}
          type="text"
          bind:value={workspaceName}
          disabled={busy}
          required
          maxlength={64}
          class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-1.5 text-body text-fg placeholder:text-[var(--fg-subtle)]"
        />
      </label>
      <p class="text-meta text-fg-subtle">
        You can rename anytime from workspace settings.
      </p>

      {#if message}
        <p
          role="alert"
          class="rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
        >
          {message}
        </p>
      {/if}

      <Button type="submit" variant="primary" {busy} disabled={busy}>
        Create workspace
      </Button>
    </form>
  </div>
{/if}
