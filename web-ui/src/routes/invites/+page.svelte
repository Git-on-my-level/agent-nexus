<script>
  import { goto } from "$app/navigation";

  import { logoutControlSession } from "$lib/controlSession.js";

  let { data } = $props();

  let organizationId = $derived(data.organizationId ?? "");
  let invite = $state(null);
  let error = $state("");
  let expired = $state(false);

  $effect(() => {
    invite = data.invite ?? null;
    error = data.inviteError ?? "";
    expired = Boolean(data.expired);
  });

  async function handleLogout() {
    await logoutControlSession({});
    goto("/auth");
  }
</script>

<svelte:head>
  <title>Accept Invite - OAR Control</title>
</svelte:head>

<main class="min-h-screen bg-[var(--ui-bg)] px-4 py-6 text-[var(--ui-text)]">
  <div class="mx-auto max-w-2xl">
    <header class="mb-6">
      <div class="flex items-center justify-between">
        <div>
          <h1 class="text-lg font-semibold text-[var(--ui-text)]">
            Accept Invite
          </h1>
          <p class="text-[var(--ui-text-muted)]">Join an organization</p>
        </div>
        <button
          class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-3 py-2 text-[12px] font-medium text-[var(--ui-text)] hover:bg-[var(--ui-border-subtle)]"
          onclick={handleLogout}
          type="button"
        >
          Sign out
        </button>
      </div>
    </header>

    {#if !organizationId}
      <div class="rounded-md border border-red-500/50 bg-red-500/10 px-4 py-6">
        <div class="flex items-start gap-3">
          <svg
            class="h-5 w-5 flex-shrink-0 text-red-400"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
            />
          </svg>
          <div>
            <p class="font-medium text-red-400">Invalid invite link</p>
            <p class="mt-2 text-[12px] text-[var(--ui-text-muted)]">
              The invite link is missing required information.
            </p>
          </div>
        </div>
        <div class="mt-4 text-center">
          <a
            class="rounded-md bg-indigo-600 px-4 py-2 text-[13px] font-medium text-white hover:bg-indigo-500"
            href="/dashboard"
          >
            Go to Dashboard
          </a>
        </div>
      </div>
    {:else if error}
      <div class="rounded-md border border-red-500/50 bg-red-500/10 px-4 py-6">
        <div class="flex items-start gap-3">
          <svg
            class="h-5 w-5 flex-shrink-0 text-red-400"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
            />
          </svg>
          <div>
            <p class="font-medium text-red-400">{error}</p>
            {#if expired}
              <p class="mt-2 text-[12px] text-[var(--ui-text-muted)]">
                Contact an administrator to request a new invite.
              </p>
            {/if}
          </div>
        </div>
        <div class="mt-4 text-center">
          <a
            class="rounded-md bg-indigo-600 px-4 py-2 text-[13px] font-medium text-white hover:bg-indigo-500"
            href="/dashboard"
          >
            Go to Dashboard
          </a>
        </div>
      </div>
    {:else if invite}
      <div
        class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-4 py-6"
      >
        <div class="text-center">
          <svg
            class="mx-auto h-12 w-12 text-indigo-500"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="1.5"
              d="M3 8l7.89 5.26a2 2 0 002.22 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"
            />
          </svg>
          <h2 class="mt-4 text-lg font-semibold text-[var(--ui-text)]">
            You've been invited!
          </h2>
          <p class="mt-2 text-[var(--ui-text-muted)]">
            Organization invites are applied through the control-plane
            registration flow, not with an extra accept step in the web UI.
          </p>
          {#if invite.role}
            <p class="mt-1 text-[12px] text-[var(--ui-text-muted)]">
              Role: {invite.role}
            </p>
          {/if}
        </div>

        <div class="mt-6 flex gap-3">
          <a
            class="flex-1 rounded-md bg-indigo-600 px-4 py-2 text-center text-[13px] font-medium text-white hover:bg-indigo-500"
            href="/dashboard"
          >
            Continue to Dashboard
          </a>
          <a
            class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)] px-4 py-2 text-[13px] font-medium text-[var(--ui-text)] hover:bg-[var(--ui-border-subtle)]"
            href="/dashboard"
          >
            Decline
          </a>
        </div>
      </div>
    {/if}
  </div>
</main>
