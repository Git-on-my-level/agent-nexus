<script>
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { page } from "$app/stores";
  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  let { items = [] } = $props();
  const href = (ref) => workspaceHref(`/tasks/${encodeURIComponent(ref)}`);
</script>

<ul class="initiatives" aria-label="Open initiatives">
  {#each items as item (item.ref)}
    <li>
      <div class="initiative-heading">
        <a href={href(item.ref)}>{item.title}</a>
        <span class="metadata"
          >{item.priority ? `${item.priority} · ` : ""}{String(
            item.phase ?? "unknown",
          ).replaceAll("_", " ")}</span
        >
      </div>
      {#if item.summary}<p class="summary">{item.summary}</p>{/if}
      {#if item.progress?.total > 0}
        <div class="progress-row">
          <progress
            value={item.progress.done}
            max={item.progress.total}
            aria-label={`${item.title} checklist`}
          ></progress>
          <span>{item.progress.done}/{item.progress.total}</span>
        </div>
      {:else}<p class="metadata">No checklist</p>{/if}
      {#each item.needs ?? [] as need}<p class="need">{need}</p>{/each}
    </li>
  {/each}
</ul>

<style>
  .initiatives {
    display: grid;
    gap: 16px;
  }
  li {
    min-width: 0;
    padding-bottom: 14px;
    border-bottom: 1px solid var(--line-subtle);
  }
  li:last-child {
    border-bottom: 0;
    padding-bottom: 0;
  }
  .initiative-heading {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    align-items: baseline;
    gap: 6px 12px;
  }
  a {
    color: var(--fg);
    font-size: 13px;
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  a:hover {
    color: var(--accent-text);
    text-decoration: underline;
  }
  .metadata {
    color: var(--fg-muted);
    font-size: 11px;
  }
  .summary {
    color: var(--fg-muted);
    font-size: 12px;
    line-height: 1.6;
    margin-top: 5px;
    overflow-wrap: anywhere;
  }
  .progress-row {
    display: flex;
    gap: 10px;
    align-items: center;
    font-size: 11px;
    color: var(--fg-muted);
    margin-top: 8px;
  }
  progress {
    width: 100%;
    max-width: 180px;
    height: 5px;
    accent-color: var(--accent-solid);
    border: 0;
    border-radius: 3px;
    overflow: hidden;
    background: var(--bg-soft);
  }
  progress::-webkit-progress-bar {
    background: var(--bg-soft);
  }
  progress::-webkit-progress-value {
    background: var(--accent-solid);
  }
  .need {
    color: var(--warn-text);
    font-size: 12px;
    margin-top: 6px;
    overflow-wrap: anywhere;
  }
</style>
