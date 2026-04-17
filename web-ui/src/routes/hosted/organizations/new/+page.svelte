<script>
  import { goto } from "$app/navigation";

  import { hostedCpFetch } from "$lib/hosted/cpFetch.js";
  import { loadHostedSession, setActiveOrg } from "$lib/hosted/session.js";

  let displayName = $state("");
  let slug = $state("");
  let slugTouched = $state(false);
  let busy = $state(false);
  let message = $state("");

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
    if (!displayName.trim() || !slug.trim()) {
      message = "Organization name and slug are required.";
      return;
    }
    busy = true;
    try {
      const res = await hostedCpFetch("organizations", {
        method: "POST",
        body: JSON.stringify({
          slug: slug.trim(),
          display_name: displayName.trim(),
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
      await goto(`/hosted/workspaces/new`);
    } catch (e) {
      message =
        e instanceof Error ? e.message : "Failed to create organization.";
    } finally {
      busy = false;
    }
  }
</script>

<svelte:head>
  <title>New organization — OAR</title>
</svelte:head>

<div class="mx-auto max-w-lg py-6">
  <p class="text-[12px] text-gray-500">
    <a class="text-indigo-400 hover:text-indigo-300" href="/hosted/dashboard"
      >← Dashboard</a
    >
  </p>
  <h1 class="mt-2 text-lg font-semibold text-gray-900">
    Create an organization
  </h1>
  <p class="mt-1 text-[12px] text-gray-500">
    Organizations group workspaces, members, and billing. You can rename the
    organization later, but the slug is permanent.
  </p>

  <form
    class="mt-5 space-y-3 rounded-md border border-gray-200 bg-gray-100 px-5 py-5"
    onsubmit={(e) => {
      e.preventDefault();
      submit();
    }}
  >
    <label class="block text-[12px] font-medium text-gray-600">
      Organization name
      <input
        type="text"
        bind:value={displayName}
        disabled={busy}
        required
        placeholder="Acme Robotics"
        class="mt-1 w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-1.5 text-[13px] text-gray-900 placeholder:text-[var(--ui-text-subtle)]"
      />
    </label>

    <label class="block text-[12px] font-medium text-gray-600">
      URL slug
      <div
        class="mt-1 flex items-stretch overflow-hidden rounded-md border border-gray-200 bg-gray-50"
      >
        <span
          class="flex items-center border-r border-gray-200 bg-gray-100 px-2.5 text-[12px] text-gray-500"
          >oar.app /</span
        >
        <input
          type="text"
          bind:value={slug}
          oninput={() => (slugTouched = true)}
          disabled={busy}
          required
          placeholder="acme-robotics"
          pattern="[a-z0-9-]+"
          class="w-full bg-transparent px-2.5 py-1.5 text-[13px] text-gray-900 placeholder:text-[var(--ui-text-subtle)]"
        />
      </div>
      <span class="mt-1 block text-[11px] text-gray-500">
        Lowercase letters, numbers, and hyphens. Used in URLs.
      </span>
    </label>

    {#if message}
      <p
        role="alert"
        class="rounded-md bg-red-500/10 px-3 py-2 text-[12px] text-red-400"
      >
        {message}
      </p>
    {/if}

    <div class="flex items-center justify-end gap-2 pt-2">
      <a
        class="rounded-md px-3 py-1.5 text-[12px] font-medium text-gray-500 hover:bg-gray-200 hover:text-gray-800"
        href="/hosted/dashboard">Cancel</a
      >
      <button
        type="submit"
        disabled={busy}
        class="rounded-md bg-indigo-600 px-3 py-1.5 text-[12px] font-medium text-white transition-colors hover:bg-indigo-500 disabled:opacity-60"
      >
        {busy ? "Creating…" : "Create organization"}
      </button>
    </div>
  </form>
</div>
