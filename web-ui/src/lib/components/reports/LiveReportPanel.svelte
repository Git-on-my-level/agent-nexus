<script>
  import LiveInitiatives from "./LiveInitiatives.svelte";
  import { inboxItemMailboxId } from "$lib/inboxUtils.js";
  import { formatLiveAge } from "$lib/liveReports.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  import { page } from "$app/stores";
  let workspaceHref = $derived(
    bindWorkspaceHref($page.params.organization, $page.params.workspace),
  );
  let canNavigate = $derived(
    Boolean($page.params.organization && $page.params.workspace),
  );
  let { panel } = $props();
  let live = $derived(panel.live);
  let items = $derived(live?.data?.items ?? []);
  let buckets = $derived(live?.data?.buckets ?? []);
  let maxCount = $derived(Math.max(1, ...buckets.map((item) => item.count)));
  const date = (value) => {
    const at = new Date(value);
    return Number.isFinite(at.getTime())
      ? at.toISOString().slice(0, 16).replace("T", " ") + " UTC"
      : "Unknown";
  };
</script>

<div class="live-panel" aria-live="polite">
  {#if !live || live.status === "loading"}
    <p class="muted">Reading workspace…</p>
  {:else if live.status !== "ok"}
    <p class="muted">
      {live.message || "Live data unavailable. Check your access or try again."}
    </p>
  {:else if panel.type === "live-initiatives"}
    {#if items.length}<LiveInitiatives {items} />{:else}<p class="muted">
        No open initiatives in this view.
      </p>{/if}
  {:else if panel.type === "live-asks"}
    {#if items.length}
      <ul class="rows">
        {#each items as item (item.id)}
          <li>
            {#if canNavigate}<a
                href={workspaceHref(
                  `/inbox?${new URLSearchParams({ mailbox: item.status === "answered" ? "handled" : "needs-you", item: inboxItemMailboxId(item) })}`,
                )}>{item.title}</a
              >{:else}<strong>{item.title}</strong>{/if}
            <p class="muted">
              {item.status === "answered" ? "Answered" : "Needs an answer"} · {formatLiveAge(
                item.age_seconds,
              )}
            </p>
            {#if item.response_text}<p>{item.response_text}</p>{/if}
          </li>
        {/each}
      </ul>
    {:else}<p class="muted">No asks in this view.</p>{/if}
  {:else if panel.type === "live-work-mix"}
    <p class="muted">{live.data.total} open tasks · by {live.data.group_by}</p>
    {#if buckets.length}
      <ul class="mix" aria-label="Open work distribution">
        {#each buckets as bucket (bucket.key)}
          <li>
            <span>{bucket.label}</span>
            <div class="track">
              <div
                class="bar"
                style:width={`${(100 * bucket.count) / maxCount}%`}
              ></div>
            </div>
            <strong>{bucket.count}</strong>
          </li>
        {/each}
      </ul>
    {:else}<p class="muted">No open work in this view.</p>{/if}
  {:else if panel.type === "live-activity"}
    {#if items.length}
      <ol class="rows">
        {#each items as item}
          <li>
            <p>{item.summary}</p>
            <p class="muted"><time datetime={item.ts}>{date(item.ts)}</time></p>
          </li>
        {/each}
      </ol>
    {:else}<p class="muted">No recent activity in this view.</p>{/if}
  {/if}
  {#if live?.truncated}<p class="partial">
      Partial view. More records may exist beyond this panel’s limit.
    </p>{/if}
  {#if live?.observed_at}
    <p class="observed">
      Live as of <time datetime={live.observed_at}
        >{date(live.observed_at)}</time
      >
    </p>
  {/if}
</div>

<style>
  .live-panel {
    font-size: 12px;
    line-height: 1.6;
    overflow-wrap: anywhere;
  }
  .muted,
  .observed {
    color: var(--fg-muted);
    font-size: 11px;
  }
  .observed {
    margin-top: 16px;
  }
  .partial {
    color: var(--warn-text);
    margin-top: 12px;
    font-size: 11px;
  }
  .rows {
    display: grid;
    gap: 12px;
  }
  .rows li {
    padding-bottom: 12px;
    border-bottom: 1px solid var(--line-subtle);
  }
  .rows li:last-child {
    border: 0;
    padding-bottom: 0;
  }
  a {
    font-weight: 600;
    color: var(--fg);
  }
  a:hover {
    color: var(--accent-text);
    text-decoration: underline;
  }
  .mix {
    display: grid;
    gap: 12px;
    margin-top: 16px;
  }
  .mix li {
    display: grid;
    grid-template-columns: minmax(70px, 1fr) minmax(50px, 2fr) 32px;
    gap: 10px;
    align-items: center;
  }
  .track {
    height: 12px;
    background: var(--bg-soft);
    border-radius: 2px;
    overflow: hidden;
  }
  .bar {
    height: 100%;
    background: var(--accent-solid);
  }
  strong {
    font-weight: 500;
    text-align: right;
  }
</style>
