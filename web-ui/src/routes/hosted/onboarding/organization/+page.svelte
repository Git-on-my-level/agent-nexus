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

  let orgName = $state("");
  let busy = $state(false);
  let message = $state("");
  let ready = $state(false);
  let inputEl = $state(null);

  const session = $derived($hostedSession);
  const orgs = $derived(session.organizations);
  const account = $derived(session.account);

  function slugify(input) {
    return String(input ?? "")
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 48);
  }

  function deriveDefaultName() {
    const displayName = String(account?.display_name ?? "").trim();
    if (displayName) {
      const first = displayName.split(/\s+/)[0];
      return `${first}'s org`;
    }
    const email = String(account?.email ?? "").trim();
    const local = email.split("@")[0] || "user";
    return `${local}'s org`;
  }

  async function ensureSession() {
    if (session.phase === "idle" || session.phase === "loading") {
      await loadHostedSession();
    }
  }

  function handleRedirect() {
    if (session.phase === "unauthed") {
      void goto("/hosted/start", { replaceState: true });
      return;
    }
    if (session.phase === "authed" && orgs.length > 0) {
      void goto("/hosted/dashboard", { replaceState: true });
      return;
    }
    if (session.phase === "authed" && orgs.length === 0) {
      if (!orgName) {
        orgName = deriveDefaultName();
      }
      ready = true;
    }
  }

  onMount(async () => {
    if (!browser) return;
    await ensureSession();
    handleRedirect();
  });

  $effect(() => {
    if (!browser) return;
    void session.phase;
    void orgs.length;
    if (ready) return;
    if (session.phase === "authed") {
      handleRedirect();
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
    const trimmed = orgName.trim();
    if (!trimmed) {
      message = "Organization name is required.";
      return;
    }
    if (trimmed.length > 64) {
      message = "Organization name must be 64 characters or fewer.";
      return;
    }
    busy = true;
    try {
      const res = await hostedCpFetch("organizations", {
        method: "POST",
        body: JSON.stringify({
          slug: slugify(trimmed) || "default-org",
          display_name: trimmed,
          plan_tier: "starter",
        }),
      });
      if (!res.ok) {
        message = await readError(res);
        return;
      }
      const body = await res.json();
      const org = body.organization;
      if (!org?.id) {
        message = "Organization created but no id was returned.";
        return;
      }
      await loadHostedSession();
      setActiveOrg(String(org.id));
      await goto("/hosted/onboarding/workspace");
    } catch (e) {
      message =
        e instanceof Error ? e.message : "Failed to create organization.";
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
  <title>Name your organization — ANX</title>
</svelte:head>

{#if ready}
  <div class="mx-auto max-w-[520px] py-8">
    <h1 class="text-display text-fg">Name your organization</h1>
    <p class="mt-1.5 text-body text-fg-muted">
      This is the billing and team boundary — everything you create belongs to
      one org.
    </p>

    <div class="mt-5 rounded-md border border-line bg-panel p-4">
      <p class="text-subtitle text-fg">Orgs vs workspaces</p>
      <div class="mt-3 space-y-2">
        <div>
          <p class="text-micro uppercase tracking-wider text-fg-muted">
            Organization
          </p>
          <p class="text-body text-fg">
            Your team's billing, members, and audit log. Usually one per
            company.
          </p>
        </div>
        <div>
          <p class="text-micro uppercase tracking-wider text-fg-muted">
            Workspace
          </p>
          <p class="text-body text-fg">
            A project inside the org. You can have many. We'll set up your first
            one next.
          </p>
        </div>
      </div>
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
        Organization name
        <input
          bind:this={inputEl}
          type="text"
          bind:value={orgName}
          disabled={busy}
          required
          maxlength={64}
          class="mt-1 w-full rounded-md border border-line bg-bg px-3 py-1.5 text-body text-fg placeholder:text-[var(--fg-subtle)]"
        />
      </label>

      {#if message}
        <p
          role="alert"
          class="rounded-md bg-danger-soft px-3 py-2 text-micro text-danger-text"
        >
          {message}
        </p>
      {/if}

      <Button type="submit" variant="primary" {busy} disabled={busy}>
        Continue
      </Button>
    </form>
  </div>
{/if}
