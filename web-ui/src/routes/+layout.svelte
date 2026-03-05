<script>
  import { onMount } from "svelte";
  import { page } from "$app/stores";

  import "../app.css";
  import {
    actorRegistry,
    actorSessionReady,
    buildActorCreatePayload,
    chooseActor,
    initializeActorSession,
    lookupActorDisplayName,
    selectedActorId,
    shouldShowActorGate,
  } from "$lib/actorSession";
  import { coreClient } from "$lib/coreClient";
  import { navigationItems } from "$lib/navigation";

  let actorError = $state("");
  let loadingActors = $state(false);
  let creatingActor = $state(false);
  let newActorName = $state("");

  let gateVisible = $derived(
    shouldShowActorGate($actorSessionReady, $selectedActorId),
  );
  let selectedActorName = $derived(
    lookupActorDisplayName($selectedActorId, $actorRegistry),
  );
  let initials = $derived(
    selectedActorName
      ? selectedActorName
          .split(/\s+/)
          .map((w) => w[0])
          .join("")
          .slice(0, 2)
          .toUpperCase()
      : "?",
  );

  onMount(async () => {
    initializeActorSession();
    await refreshActors();
  });

  async function refreshActors() {
    loadingActors = true;
    actorError = "";

    try {
      const response = await coreClient.listActors();
      actorRegistry.set(response.actors ?? []);
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      actorError = `Failed to load actors: ${reason}`;
      actorRegistry.set([]);
    } finally {
      loadingActors = false;
    }
  }

  function selectActor(actorId) {
    chooseActor(actorId);
  }

  function switchIdentity() {
    chooseActor("");
  }

  function buildActorId(displayName) {
    const base = displayName
      .toLowerCase()
      .trim()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 24);
    const suffix = Math.random().toString(36).slice(2, 8);
    return `actor-${base || "user"}-${suffix}`;
  }

  async function createActor() {
    if (!newActorName.trim()) {
      actorError = "Display name is required.";
      return;
    }

    creatingActor = true;
    actorError = "";

    try {
      const payload = buildActorCreatePayload({
        id: buildActorId(newActorName),
        displayName: newActorName.trim(),
        tags: ["human"],
      });

      const response = await coreClient.createActor(payload);
      const createdActor = response.actor;
      actorRegistry.set([...$actorRegistry, createdActor]);
      chooseActor(createdActor.id);
      newActorName = "";
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      actorError = `Failed to create actor: ${reason}`;
    } finally {
      creatingActor = false;
    }
  }
</script>

<div class="flex min-h-screen bg-gray-50 text-gray-900">
  {#if !$actorSessionReady}
    <main class="flex flex-1 items-center justify-center">
      <p class="text-sm text-gray-500">Loading...</p>
    </main>
  {:else if gateVisible}
    <main class="flex flex-1 items-center justify-center p-8">
      <section
        class="w-full max-w-md rounded-lg border border-gray-200 bg-white p-6"
      >
        <h1 class="text-lg font-semibold text-gray-900">
          Choose your identity
        </h1>
        <p class="mt-1 text-sm text-gray-500">
          Select an actor or create a new one to continue.
        </p>

        {#if actorError}
          <p class="mt-3 rounded-md bg-red-50 px-3 py-2 text-sm text-red-700">
            {actorError}
          </p>
        {/if}

        <div class="mt-5">
          {#if loadingActors}
            <p class="text-sm text-gray-400">Loading...</p>
          {:else if $actorRegistry.length === 0}
            <p class="text-sm text-gray-400">
              No actors yet. Create one below.
            </p>
          {:else}
            <ul class="space-y-1">
              {#each $actorRegistry as actor}
                <li>
                  <button
                    class="flex w-full items-center gap-3 rounded-md px-3 py-2 text-left text-sm transition-colors hover:bg-gray-50"
                    onclick={() => selectActor(actor.id)}
                    type="button"
                  >
                    <span
                      class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-indigo-100 text-xs font-semibold text-indigo-700"
                    >
                      {(actor.display_name || "?").slice(0, 1).toUpperCase()}
                    </span>
                    <span class="font-medium text-gray-900"
                      >{actor.display_name}</span
                    >
                  </button>
                </li>
              {/each}
            </ul>
          {/if}
        </div>

        <form
          class="mt-5 border-t border-gray-100 pt-5"
          onsubmit={(event) => {
            event.preventDefault();
            createActor();
          }}
        >
          <label
            class="block text-sm font-medium text-gray-700"
            for="actor-display-name"
          >
            New actor name
          </label>
          <div class="mt-1.5 flex gap-2">
            <input
              bind:value={newActorName}
              class="flex-1 rounded-md border border-gray-300 px-3 py-1.5 text-sm"
              id="actor-display-name"
              name="actor-display-name"
              placeholder="Jane Doe"
              type="text"
            />
            <button
              class="rounded-md bg-indigo-600 px-4 py-1.5 text-sm font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
              disabled={creatingActor}
              type="submit"
            >
              {creatingActor ? "Creating..." : "Create"}
            </button>
          </div>
        </form>
      </section>
    </main>
  {:else}
    <aside
      class="flex w-52 shrink-0 flex-col border-r border-gray-200 bg-white"
    >
      <div class="px-4 pb-2 pt-5">
        <p class="text-xs font-semibold uppercase tracking-wider text-gray-400">
          OAR
        </p>
      </div>

      <nav class="flex-1 px-2 py-1" aria-label="Primary">
        <ul class="space-y-0.5">
          {#each navigationItems as item}
            <li>
              <a
                class={`flex items-center rounded-md px-3 py-1.5 text-[13px] font-medium transition-colors ${
                  $page.url.pathname === item.href ||
                  $page.url.pathname.startsWith(item.href + "/")
                    ? "bg-indigo-50 text-indigo-700"
                    : "text-gray-600 hover:bg-gray-50 hover:text-gray-900"
                }`}
                href={item.href}
              >
                {item.label}
              </a>
            </li>
          {/each}
        </ul>
      </nav>

      <div class="border-t border-gray-100 px-3 py-3">
        <div class="flex items-center gap-2">
          <span
            class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-indigo-100 text-xs font-semibold text-indigo-700"
          >
            {initials}
          </span>
          <div class="min-w-0 flex-1">
            <p class="truncate text-[13px] font-medium text-gray-900">
              {selectedActorName}
            </p>
          </div>
        </div>
        <button
          class="mt-2 w-full rounded-md px-2 py-1 text-left text-xs text-gray-500 transition-colors hover:bg-gray-50 hover:text-gray-700"
          onclick={switchIdentity}
          type="button"
        >
          Switch identity
        </button>
      </div>
    </aside>

    <main class="flex-1 overflow-y-auto px-8 py-6">
      <div class="mx-auto max-w-4xl">
        <slot />
      </div>
    </main>
  {/if}
</div>
