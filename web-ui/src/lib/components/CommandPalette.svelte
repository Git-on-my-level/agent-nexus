<script>
  import { goto } from "$app/navigation";
  import {
    searchTopics,
    searchDocuments,
    searchBoards,
    searchArtifacts,
  } from "$lib/searchHelpers";
  import { kindLabel } from "$lib/artifactKinds";
  import { workspacePath } from "$lib/workspacePaths";

  let { open = $bindable(false), workspaceSlug = "" } = $props();

  let query = $state("");
  let results = $state({ topics: [], docs: [], boards: [], artifacts: [] });
  let loading = $state(false);
  let activeIndex = $state(-1);
  let inputEl = $state(null);
  let debounceTimer = null;
  let latestRequestId = 0;

  let flatResults = $derived(buildFlatResults(results));

  function buildFlatResults(r) {
    const flat = [];
    if (r.topics.length) {
      flat.push({ type: "header", label: "Topics" });
      for (const t of r.topics) flat.push({ type: "topic", item: t });
    }
    if (r.docs.length) {
      flat.push({ type: "header", label: "Docs" });
      for (const d of r.docs) flat.push({ type: "doc", item: d });
    }
    if (r.boards.length) {
      flat.push({ type: "header", label: "Boards" });
      for (const b of r.boards) flat.push({ type: "board", item: b });
    }
    if (r.artifacts.length) {
      flat.push({ type: "header", label: "Artifacts" });
      for (const a of r.artifacts) flat.push({ type: "artifact", item: a });
    }
    return flat;
  }

  let selectableIndices = $derived(
    flatResults
      .map((r, i) => (r.type !== "header" ? i : -1))
      .filter((i) => i !== -1),
  );

  $effect(() => {
    if (open && inputEl) {
      inputEl.focus();
    }
  });

  $effect(() => {
    if (!open) {
      query = "";
      results = { topics: [], docs: [], boards: [], artifacts: [] };
      loading = false;
      activeIndex = -1;
      if (debounceTimer) clearTimeout(debounceTimer);
    }
  });

  function close() {
    open = false;
  }

  function handleInput(e) {
    query = e.target.value;
    activeIndex = -1;
    debouncedSearch(query);
  }

  function debouncedSearch(q) {
    if (debounceTimer) clearTimeout(debounceTimer);
    const trimmed = q.trim();
    if (!trimmed) {
      results = { topics: [], docs: [], boards: [], artifacts: [] };
      loading = false;
      return;
    }
    loading = true;
    debounceTimer = setTimeout(() => executeSearch(trimmed), 300);
  }

  async function executeSearch(q) {
    const requestId = ++latestRequestId;
    try {
      const [topics, docs, boards, artifacts] = await Promise.allSettled([
        searchTopics(q, 5),
        searchDocuments(q, 5),
        searchBoards(q, 5),
        searchArtifacts(q, 5),
      ]);
      if (requestId !== latestRequestId) return;
      results = {
        topics: topics.status === "fulfilled" ? topics.value : [],
        docs: docs.status === "fulfilled" ? docs.value : [],
        boards: boards.status === "fulfilled" ? boards.value : [],
        artifacts: artifacts.status === "fulfilled" ? artifacts.value : [],
      };
    } catch {
      if (requestId !== latestRequestId) return;
      results = { topics: [], docs: [], boards: [], artifacts: [] };
    } finally {
      if (requestId === latestRequestId) loading = false;
    }
  }

  function navigate(entry) {
    if (!workspaceSlug || entry.type === "header") return;
    const paths = {
      topic: `/topics/${entry.item.id}`,
      doc: `/docs/${entry.item.id}`,
      board: `/boards/${entry.item.id}`,
      artifact: `/artifacts/${entry.item.id}`,
    };
    const target = paths[entry.type];
    if (target) {
      close();
      goto(workspacePath(workspaceSlug, target));
    }
  }

  function handleKeydown(e) {
    if (e.key === "Escape") {
      e.preventDefault();
      close();
      return;
    }
    if (!selectableIndices.length) return;

    if (e.key === "ArrowDown") {
      e.preventDefault();
      const currentPos = selectableIndices.indexOf(activeIndex);
      const nextPos =
        currentPos < 0
          ? 0
          : Math.min(currentPos + 1, selectableIndices.length - 1);
      activeIndex = selectableIndices[nextPos];
      scrollToActive();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      const currentPos = selectableIndices.indexOf(activeIndex);
      const prevPos = currentPos <= 0 ? 0 : currentPos - 1;
      activeIndex = selectableIndices[prevPos];
      scrollToActive();
    } else if (e.key === "Enter" && activeIndex >= 0) {
      e.preventDefault();
      navigate(flatResults[activeIndex]);
    }
  }

  function scrollToActive() {
    const el = document.querySelector(`[data-cmd-index="${activeIndex}"]`);
    if (el) el.scrollIntoView({ block: "nearest" });
  }

  function handleBackdropClick(e) {
    if (e.target === e.currentTarget) close();
  }

  function resultTitle(entry) {
    if (entry.type === "artifact") {
      const summary = String(entry.item.summary ?? "").trim();
      if (summary) return summary;
      return `${kindLabel(entry.item.kind)} artifact`;
    }
    return entry.item.title || entry.item.display_name || entry.item.id;
  }

  function resultSubtitle(entry) {
    if (entry.type === "topic") {
      const parts = [];
      if (entry.item.status) parts.push(entry.item.status);
      if (entry.item.priority) parts.push(entry.item.priority);
      return parts.join(" · ") || entry.item.id;
    }
    if (entry.type === "doc") {
      return entry.item.head_version
        ? `v${entry.item.head_version}`
        : entry.item.id;
    }
    if (entry.type === "board") {
      return entry.item.id;
    }
    if (entry.type === "artifact") {
      return kindLabel(entry.item.kind);
    }
    return "";
  }

  const typeIcons = {
    topic:
      "M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z",
    doc: "M19.5 14.25v-2.625a3.375 3.375 0 00-3.375-3.375h-1.5A1.125 1.125 0 0113.5 7.125v-1.5a3.375 3.375 0 00-3.375-3.375H8.25m2.25 0H5.625c-.621 0-1.125.504-1.125 1.125v17.25c0 .621.504 1.125 1.125 1.125h12.75c.621 0 1.125-.504 1.125-1.125V11.25a9 9 0 00-9-9z",
    board: "M3 6h4v12H3V6zm7 0h4v12h-4V6zm7 0h4v12h-4V6z",
    artifact:
      "M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z",
  };

  const typeLabels = {
    topic: "Topic",
    doc: "Doc",
    board: "Board",
    artifact: "Artifact",
  };
</script>

{#if open}
  <div
    class="cmd-backdrop"
    onclick={handleBackdropClick}
    onkeydown={handleKeydown}
    role="dialog"
    aria-modal="true"
    aria-label="Command palette"
    tabindex="-1"
  >
    <div class="cmd-modal">
      <div class="cmd-input-wrap">
        <svg
          class="cmd-search-icon"
          viewBox="0 0 20 20"
          fill="currentColor"
          aria-hidden="true"
        >
          <path
            fill-rule="evenodd"
            d="M9 3.5a5.5 5.5 0 100 11 5.5 5.5 0 000-11zM2 9a7 7 0 1112.452 4.391l3.328 3.329a.75.75 0 11-1.06 1.06l-3.329-3.328A7 7 0 012 9z"
            clip-rule="evenodd"
          />
        </svg>
        <input
          bind:this={inputEl}
          class="cmd-input"
          type="text"
          placeholder="Search topics, docs, boards, artifacts..."
          value={query}
          oninput={handleInput}
          spellcheck="false"
          autocomplete="off"
        />
        <kbd class="cmd-esc-hint">ESC</kbd>
      </div>

      <div class="cmd-results">
        {#if loading}
          <div class="cmd-status">Searching...</div>
        {:else if query.trim() && flatResults.length === 0}
          <div class="cmd-status">No results found</div>
        {:else if !query.trim()}
          <div class="cmd-status cmd-status--hint">
            Type to search across your workspace
          </div>
        {/if}

        {#each flatResults as entry, i}
          {#if entry.type === "header"}
            <div class="cmd-group-header">{entry.label}</div>
          {:else}
            <button
              class="cmd-result-row"
              class:cmd-result-row--active={i === activeIndex}
              data-cmd-index={i}
              onclick={() => navigate(entry)}
              onmouseenter={() => (activeIndex = i)}
              type="button"
            >
              <svg
                class="cmd-result-icon"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                stroke-width="1.75"
                aria-hidden="true"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  d={typeIcons[entry.type]}
                />
              </svg>
              <div class="cmd-result-text">
                <span class="cmd-result-title">{resultTitle(entry)}</span>
                <span class="cmd-result-subtitle">{resultSubtitle(entry)}</span>
              </div>
              <span class="cmd-result-badge"
                >{entry.type === "artifact"
                  ? kindLabel(entry.item.kind)
                  : typeLabels[entry.type]}</span
              >
            </button>
          {/if}
        {/each}
      </div>
    </div>
  </div>
{/if}

<style>
  .cmd-backdrop {
    position: fixed;
    inset: 0;
    z-index: 9999;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding-top: 15vh;
    background: rgba(0, 0, 0, 0.6);
    backdrop-filter: blur(2px);
  }

  .cmd-modal {
    width: 540px;
    max-width: calc(100vw - 2rem);
    max-height: 420px;
    display: flex;
    flex-direction: column;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-modal);
    overflow: hidden;
  }

  .cmd-input-wrap {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 14px;
    border-bottom: 1px solid var(--line);
  }

  .cmd-search-icon {
    width: 16px;
    height: 16px;
    flex-shrink: 0;
    color: var(--fg-muted);
  }

  .cmd-input {
    flex: 1;
    background: transparent;
    border: none;
    outline: none;
    color: var(--fg);
    font-size: 13px;
    font-family: var(--font-sans);
    line-height: 1.4;
  }

  .cmd-input::placeholder {
    color: var(--fg-subtle);
  }

  .cmd-esc-hint {
    flex-shrink: 0;
    padding: 1px 6px;
    font-size: 10px;
    font-family: var(--font-sans);
    color: var(--fg-subtle);
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: 3px;
    line-height: 1.6;
  }

  .cmd-results {
    flex: 1;
    overflow-y: auto;
    padding: 4px 0;
  }

  .cmd-status {
    padding: 20px 14px;
    text-align: center;
    font-size: 12px;
    color: var(--fg-muted);
  }

  .cmd-status--hint {
    color: var(--fg-subtle);
  }

  .cmd-group-header {
    padding: 8px 14px 4px;
    font-size: 11px;
    font-weight: 600;
    color: var(--fg-muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .cmd-result-row {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 7px 14px;
    background: transparent;
    border: none;
    cursor: pointer;
    text-align: left;
    color: var(--fg);
    font-family: var(--font-sans);
  }

  .cmd-result-row:hover,
  .cmd-result-row--active {
    background: var(--bg-soft);
  }

  .cmd-result-icon {
    width: 16px;
    height: 16px;
    flex-shrink: 0;
    color: var(--fg-muted);
  }

  .cmd-result-text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .cmd-result-title {
    font-size: 13px;
    line-height: 1.3;
    color: var(--fg);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .cmd-result-subtitle {
    font-size: 13px;
    line-height: 1.3;
    color: var(--fg-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .cmd-result-badge {
    flex-shrink: 0;
    font-size: 10px;
    padding: 1px 6px;
    border-radius: 3px;
    background: var(--bg);
    border: 1px solid var(--line);
    color: var(--fg-muted);
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }
</style>
