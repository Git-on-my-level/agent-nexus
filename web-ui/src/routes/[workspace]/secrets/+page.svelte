<script>
  import { page } from "$app/stores";

  import {
    authenticatedAgent,
    isHumanWorkspacePrincipal,
  } from "$lib/authSession";
  import ConfirmModal from "$lib/components/ConfirmModal.svelte";
  import { coreClient } from "$lib/coreClient";
  import { formatTimestamp } from "$lib/formatDate";

  let secrets = $state([]);
  let loading = $state(true);
  let pageError = $state("");

  let showCreateForm = $state(false);
  let newName = $state("");
  let newValue = $state("");
  let newDescription = $state("");
  let creating = $state(false);
  let createError = $state("");

  let deleteConfirm = $state({ open: false, id: "", name: "" });
  let deleting = $state("");

  let revealedSecrets = $state({});
  let revealingId = $state("");

  const agent = $derived($authenticatedAgent);
  const isHuman = $derived(isHumanWorkspacePrincipal(agent));
  const workspaceSlug = $derived($page.params.workspace ?? "");

  $effect(() => {
    if (!isHuman) {
      loading = false;
      pageError = "";
      secrets = [];
      return;
    }
    void loadSecrets();
  });

  async function loadSecrets() {
    loading = true;
    pageError = "";
    try {
      const result = await coreClient.listSecrets();
      secrets = result.secrets ?? [];
    } catch (err) {
      pageError = err?.message ?? "Failed to load secrets";
    } finally {
      loading = false;
    }
  }

  async function handleCreate() {
    if (!newName.trim() || !newValue) return;
    creating = true;
    createError = "";
    try {
      const payload = { name: newName.trim(), value: newValue };
      if (newDescription.trim()) payload.description = newDescription.trim();
      await coreClient.createSecret(payload);
      newName = "";
      newValue = "";
      newDescription = "";
      showCreateForm = false;
      await loadSecrets();
    } catch (err) {
      createError = err?.message ?? "Failed to create secret";
    } finally {
      creating = false;
    }
  }

  async function handleDelete(id) {
    deleting = id;
    try {
      await coreClient.deleteSecret(id);
      const updated = { ...revealedSecrets };
      delete updated[id];
      revealedSecrets = updated;
      await loadSecrets();
    } catch (err) {
      pageError = err?.message ?? "Failed to delete secret";
    } finally {
      deleting = "";
    }
  }

  async function handleReveal(id) {
    revealingId = id;
    try {
      const result = await coreClient.revealSecret(id);
      revealedSecrets = { ...revealedSecrets, [id]: result.value };
    } catch (err) {
      pageError = err?.message ?? "Failed to reveal secret";
    } finally {
      revealingId = "";
    }
  }

  function hideReveal(id) {
    const updated = { ...revealedSecrets };
    delete updated[id];
    revealedSecrets = updated;
  }

  async function copyToClipboard(text) {
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      // Fallback: ignored
    }
  }
</script>

<svelte:head>
  <title>Secrets{workspaceSlug ? ` - ${workspaceSlug}` : ""} · OAR</title>
</svelte:head>

<div class="mx-auto max-w-3xl px-4 py-6">
  <div class="mb-4 flex items-center justify-between">
    <h1 class="text-[15px] font-semibold text-[var(--ui-text)]">Secrets</h1>
    {#if isHuman}
      <button
        class="cursor-pointer rounded-md bg-[var(--ui-accent-strong)] px-3 py-1.5 text-[12px] font-medium text-white hover:bg-[var(--ui-accent)] disabled:opacity-50"
        onclick={() => {
          showCreateForm = !showCreateForm;
        }}
        type="button"
      >
        {showCreateForm ? "Cancel" : "New secret"}
      </button>
    {/if}
  </div>

  <p class="mb-4 text-[12px] text-[var(--ui-text-muted)]">
    Workspace credentials for agent use. Values are encrypted at rest. Reveals
    are logged in audit.
  </p>

  {#if pageError}
    <div
      class="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-[12px] text-red-400"
    >
      {pageError}
    </div>
  {/if}

  {#if showCreateForm}
    <div
      class="mb-4 rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] p-4"
    >
      <h2 class="mb-3 text-[13px] font-medium text-[var(--ui-text)]">
        Create secret
      </h2>
      {#if createError}
        <div class="mb-2 text-[12px] text-red-400">{createError}</div>
      {/if}
      <div class="space-y-3">
        <div>
          <label
            class="mb-1 block text-[11px] font-medium text-[var(--ui-text-muted)]"
            for="secret-name">Name</label
          >
          <input
            id="secret-name"
            type="text"
            placeholder="OPENAI_API_KEY"
            bind:value={newName}
            class="w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-3 py-1.5 text-[13px] text-[var(--ui-text)] placeholder:text-[var(--ui-text-subtle)] focus:border-[var(--ui-accent)] focus:outline-none"
          />
        </div>
        <div>
          <label
            class="mb-1 block text-[11px] font-medium text-[var(--ui-text-muted)]"
            for="secret-value">Value</label
          >
          <input
            id="secret-value"
            type="password"
            placeholder="sk-..."
            bind:value={newValue}
            class="w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-3 py-1.5 font-mono text-[13px] text-[var(--ui-text)] placeholder:text-[var(--ui-text-subtle)] focus:border-[var(--ui-accent)] focus:outline-none"
          />
        </div>
        <div>
          <label
            class="mb-1 block text-[11px] font-medium text-[var(--ui-text-muted)]"
            for="secret-desc">Description (optional)</label
          >
          <input
            id="secret-desc"
            type="text"
            placeholder="API key for the summarizer agent"
            bind:value={newDescription}
            class="w-full rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-3 py-1.5 text-[13px] text-[var(--ui-text)] placeholder:text-[var(--ui-text-subtle)] focus:border-[var(--ui-accent)] focus:outline-none"
          />
        </div>
        <div class="flex justify-end gap-2">
          <button
            class="cursor-pointer rounded-md px-3 py-1.5 text-[12px] font-medium text-[var(--ui-text-muted)] hover:text-[var(--ui-text)]"
            onclick={() => {
              showCreateForm = false;
            }}
            type="button"
          >
            Cancel
          </button>
          <button
            class="cursor-pointer rounded-md bg-[var(--ui-accent-strong)] px-3 py-1.5 text-[12px] font-medium text-white hover:bg-[var(--ui-accent)] disabled:opacity-50"
            disabled={creating || !newName.trim() || !newValue}
            onclick={handleCreate}
            type="button"
          >
            {creating ? "Creating..." : "Create"}
          </button>
        </div>
      </div>
    </div>
  {/if}

  {#if loading}
    <div class="py-8 text-center text-[13px] text-[var(--ui-text-muted)]">
      Loading secrets...
    </div>
  {:else if secrets.length === 0}
    <div
      class="rounded-md border border-[var(--ui-border)] bg-[var(--ui-panel)] px-4 py-8 text-center"
    >
      <p class="text-[13px] text-[var(--ui-text-muted)]">
        No secrets configured.
      </p>
      {#if isHuman}
        <p class="mt-1 text-[12px] text-[var(--ui-text-subtle)]">
          Create a secret to store API keys and credentials for agent use.
        </p>
      {/if}
    </div>
  {:else}
    <div
      class="overflow-hidden rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg-soft)]"
    >
      {#each secrets as secret, i}
        {@const revealed = revealedSecrets[secret.id]}
        <div
          class="px-4 py-3 {i > 0 ? 'border-t border-[var(--ui-border)]' : ''}"
        >
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2">
                <span
                  class="font-mono text-[13px] font-medium text-[var(--ui-text)]"
                  >{secret.name}</span
                >
              </div>
              {#if secret.description}
                <p class="mt-0.5 text-[12px] text-[var(--ui-text-muted)]">
                  {secret.description}
                </p>
              {/if}
              <p class="mt-1 text-[11px] text-[var(--ui-text-subtle)]">
                Updated {formatTimestamp(secret.updated_at)}
              </p>
            </div>
            <div class="flex shrink-0 items-center gap-1.5">
              {#if revealed}
                <div
                  class="flex items-center gap-1.5 rounded-md border border-[var(--ui-border)] bg-[var(--ui-bg)] px-2 py-1"
                >
                  <code
                    class="max-w-[200px] truncate font-mono text-[11px] text-[var(--ui-text)]"
                    >{revealed}</code
                  >
                  <button
                    class="cursor-pointer text-[11px] text-[var(--ui-accent)] hover:text-[var(--ui-accent-strong)]"
                    onclick={() => copyToClipboard(revealed)}
                    type="button"
                    title="Copy"
                  >
                    Copy
                  </button>
                  <button
                    class="cursor-pointer text-[11px] text-[var(--ui-text-muted)] hover:text-[var(--ui-text)]"
                    onclick={() => hideReveal(secret.id)}
                    type="button"
                  >
                    Hide
                  </button>
                </div>
              {:else}
                <button
                  class="cursor-pointer rounded px-2 py-1 text-[11px] font-medium text-[var(--ui-accent)] hover:bg-[var(--ui-accent)]/10 disabled:opacity-50"
                  disabled={revealingId === secret.id}
                  onclick={() => handleReveal(secret.id)}
                  type="button"
                >
                  {revealingId === secret.id ? "Revealing..." : "Reveal"}
                </button>
              {/if}
              {#if isHuman}
                <button
                  class="cursor-pointer rounded px-2 py-1 text-[11px] font-medium text-red-400 hover:bg-red-400/10 disabled:opacity-50"
                  disabled={deleting === secret.id}
                  onclick={() => {
                    deleteConfirm = {
                      open: true,
                      id: secret.id,
                      name: secret.name,
                    };
                  }}
                  type="button"
                >
                  {deleting === secret.id ? "Deleting..." : "Delete"}
                </button>
              {/if}
            </div>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

<ConfirmModal
  open={deleteConfirm.open}
  title="Delete secret"
  message={`Permanently delete "${deleteConfirm.name}"? This cannot be undone. Agents using this secret will lose access.`}
  confirmLabel="Delete"
  variant="danger"
  busy={deleting === deleteConfirm.id}
  onconfirm={() => {
    void handleDelete(deleteConfirm.id);
    deleteConfirm = { open: false, id: "", name: "" };
  }}
  oncancel={() => {
    deleteConfirm = { open: false, id: "", name: "" };
  }}
/>
