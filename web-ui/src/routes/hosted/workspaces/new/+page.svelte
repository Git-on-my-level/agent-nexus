<script>
  import { onMount } from "svelte";

  import { browser } from "$app/environment";
  import { goto } from "$app/navigation";

  import Button from "$lib/components/Button.svelte";
  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";
  import { hostedSession, loadHostedSession } from "$lib/hosted/session.js";

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
      message =
        "Service identity fields must both be set (BYO) or both left empty (platform managed).";
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
        message = await readError(res);
        return;
      }
      await goto("/hosted/dashboard");
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
    Workspaces are isolated environments inside
    <span class="text-fg"
      >{activeOrg?.display_name || activeOrg?.slug || "your organization"}</span
    >. Each one runs its own agent and stores its own threads.
  </p>

  <form
    class="mt-5 space-y-3 rounded-md border border-line bg-bg-soft px-5 py-5"
    onsubmit={(e) => {
      e.preventDefault();
      submit();
    }}
  >
    <label class="block text-micro text-fg-muted">
      Workspace name
      <input
        type="text"
        bind:value={displayName}
        disabled={busy}
        required
        placeholder="Q3 launch"
        class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-1.5 text-body text-fg placeholder:text-[var(--fg-subtle)]"
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
        class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-1.5 font-mono text-mono text-fg placeholder:text-[var(--fg-subtle)]"
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
              Platform provisioning is the default. Set both fields only for
              bring-your-own service identity.
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
