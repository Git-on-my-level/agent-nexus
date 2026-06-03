<script>
  import { onMount } from "svelte";

  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";

  import Button from "$lib/components/Button.svelte";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";
  import {
    hostedSession,
    loadHostedSession,
    setActiveOrg,
  } from "$lib/hosted/session.js";
  import {
    readHostedCreateError,
    workspaceCreateRedirectHref,
  } from "$lib/hosted/workspaceCreation.js";

  let displayName = $state("");
  let slug = $state("");
  let slugTouched = $state(false);
  let serviceId = $state("");
  let servicePublicKey = $state("");
  let advancedOpen = $state(false);
  let busy = $state(false);
  let message = $state("");

  const session = $derived($hostedSession);
  const orgs = $derived(session.organizations);
  const activeOrg = $derived(
    orgs.find((o) => String(o.id) === session.activeOrgId) ?? null,
  );
  const activeOrgLabel = $derived(
    activeOrg?.display_name || activeOrg?.slug || "the active organization",
  );

  function slugify(input) {
    return String(input ?? "")
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 48);
  }

  $effect(() => {
    if (!slugTouched) {
      slug = slugify(displayName);
    }
  });

  const canBringOwnServiceIdentity = $derived(
    Boolean(activeOrg?.flags?.allow_byo_service_identity),
  );

  onMount(async () => {
    if (!browser) return;
    if (session.phase !== "authed") {
      await loadHostedSession();
    }
  });

  async function submit() {
    message = "";
    if (!activeOrg?.id) {
      message = "Pick an organization first.";
      return;
    }
    if (!displayName.trim() || !slug.trim()) {
      message = "Workspace name and slug are required.";
      return;
    }
    const sid = serviceId.trim();
    const pub = servicePublicKey.trim();
    if ((sid && !pub) || (!sid && pub)) {
      advancedOpen = true;
      message = "Fill in both advanced fields, or leave both blank.";
      return;
    }
    busy = true;
    try {
      const body = {
        organization_id: activeOrg.id,
        slug: slug.trim(),
        display_name: displayName.trim(),
      };
      if (sid && pub) {
        body.service_identity_id = sid;
        body.service_identity_public_key = pub;
      }
      const res = await hostedCpFetch("workspaces", {
        method: "POST",
        body: JSON.stringify(body),
      });
      if (!res.ok) {
        message = await readHostedCreateError(res, activeOrg);
        return;
      }
      const createBody = await res.json();
      const workspace = createBody.workspace ?? createBody;
      if (!workspace?.id) {
        message =
          "Workspace was created, but we couldn't open it yet. Go to the dashboard and try again.";
        return;
      }
      setActiveOrg(String(activeOrg.id));
      await loadHostedSession();
      await goto(workspaceCreateRedirectHref(activeOrg, workspace), {
        replaceState: true,
      });
    } catch (e) {
      message = e instanceof Error ? e.message : "Failed to create workspace.";
    } finally {
      busy = false;
    }
  }
</script>

<svelte:head>
  <title>New workspace — ANX</title>
</svelte:head>

<div class="mx-auto max-w-lg py-6">
  <p class="text-micro text-fg-subtle">
    <a class="text-accent-text hover:text-accent-text" href="/hosted/dashboard"
      >← Dashboard</a
    >
  </p>
  <h1 class="mt-2 text-display text-fg">Create a workspace</h1>
  <p class="mt-1 text-meta text-fg-subtle">
    Create a separate workspace for each project or codebase in
    <span class="text-fg"
      >{activeOrg?.display_name || activeOrg?.slug || "your organization"}</span
    >. Each workspace keeps its work and history separate.
  </p>

  <form
    class="mt-5 space-y-3 rounded-md border border-line bg-bg-soft px-5 py-5"
    onsubmit={(e) => {
      e.preventDefault();
      submit();
    }}
  >
    <div class="rounded-md border border-line bg-panel px-3 py-2">
      <p class="text-micro uppercase text-fg-subtle">Creating in</p>
      <p class="mt-0.5 truncate text-body text-fg">{activeOrgLabel}</p>
      {#if activeOrg?.slug}
        <p class="mt-0.5 truncate font-mono text-mono text-fg-subtle">
          {activeOrg.slug}
        </p>
      {/if}
    </div>

    <label class="block text-micro text-fg-muted">
      Workspace name
      <input
        type="text"
        bind:value={displayName}
        disabled={busy}
        required
        placeholder="Q3 launch"
        class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-1.5 text-body text-fg placeholder:text-fg-subtle"
      />
    </label>

    <label class="block text-micro text-fg-muted">
      Slug
      <input
        type="text"
        bind:value={slug}
        oninput={() => (slugTouched = true)}
        disabled={busy}
        required
        placeholder="q3-launch"
        pattern="[-a-z0-9]+"
        class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-1.5 font-mono text-mono text-fg placeholder:text-fg-subtle"
      />
      <span class="mt-1 block text-micro text-fg-subtle">
        Lowercase letters, numbers, and hyphens. Used in workspace URLs.
      </span>
    </label>

    {#if canBringOwnServiceIdentity}
      <div class="border-t border-line pt-3">
        <button
          type="button"
          class="flex items-center gap-1.5 text-micro text-fg-subtle hover:text-fg"
          onclick={() => (advancedOpen = !advancedOpen)}
          aria-expanded={advancedOpen}
        >
          <svg
            viewBox="0 0 12 12"
            class="h-3 w-3 transition-transform {advancedOpen
              ? 'rotate-90'
              : ''}"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            aria-hidden="true"
          >
            <path d="M4.5 3l3 3-3 3" />
          </svg>
          Advanced settings
        </button>

        {#if advancedOpen}
          <div
            class="mt-3 space-y-3 rounded-md border border-line bg-bg px-3 py-3"
          >
            <p class="text-micro text-fg-subtle">
              Most teams can leave this blank. These fields are only for teams
              bringing their own workspace service identity.
            </p>
            <label class="block text-micro text-fg-muted">
              Service identity id
              <input
                type="text"
                bind:value={serviceId}
                disabled={busy}
                class="mt-1 w-full rounded-md border border-line bg-bg-soft px-3 py-1.5 text-body text-fg"
              />
            </label>
            <label class="block text-micro text-fg-muted">
              Service identity public key (base64)
              <textarea
                bind:value={servicePublicKey}
                disabled={busy}
                rows="3"
                class="mt-1 w-full rounded-md border border-line bg-bg-soft px-3 py-1.5 font-mono text-mono text-fg"
              ></textarea>
            </label>
          </div>
        {/if}
      </div>
    {/if}

    {#if message}
      <p
        role="alert"
        class="rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
      >
        {message}
      </p>
    {/if}

    <div class="flex items-center justify-end gap-2 pt-2">
      <Button variant="ghost" href="/hosted/dashboard">Cancel</Button>
      <Button type="submit" variant="primary" disabled={busy || !activeOrg}>
        {busy ? "Creating…" : "Create workspace"}
      </Button>
    </div>
  </form>
</div>
