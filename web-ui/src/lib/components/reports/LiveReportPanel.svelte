<script>
  import LiveInitiativeDetails from "./LiveInitiativeDetails.svelte";
  import UnavailableValue from "$lib/components/UnavailableValue.svelte";
  import { inboxItemMailboxId } from "$lib/inboxUtils.js";
  import { formatLiveAge } from "$lib/liveReports.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  let {
    panel,
    resolved = new Map(),
    organizationSlug = "",
    workspaceSlug = "",
    onpreview = null,
    onpreviewclose = null,
  } = $props();
  let canNavigate = $derived(Boolean(organizationSlug && workspaceSlug));
  let workspaceHref = $derived(
    canNavigate ? bindWorkspaceHref(organizationSlug, workspaceSlug) : null,
  );
  let live = $derived(panel.live);
  let items = $derived(live?.data?.items ?? []);
  let buckets = $derived(live?.data?.buckets ?? []);
  let fleetHosts = $derived(live?.data?.hosts ?? []);
  let fleetEnrollments = $derived(live?.data?.enrollments ?? []);
  let fleetSeries = $derived(live?.data?.series ?? []);
  let maxCount = $derived(Math.max(1, ...buckets.map((item) => item.count)));
  /** An instant, or `""` so the caller can render the dash. */
  const date = (value) => {
    const at = new Date(value);
    return Number.isFinite(at.getTime())
      ? at.toISOString().slice(0, 16).replace("T", " ") + " UTC"
      : "";
  };
  /**
   * A fleet reading. `null` when the host has not reported one, so the row
   * shows a dash with the reason rather than a sentence where a value goes.
   */
  const fleetValue = (latest) => {
    if (latest?.state !== undefined) return latest.state;
    if (latest?.value !== undefined) return String(latest.value);
    return null;
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
    {#if items.length}
      <LiveInitiativeDetails
        {items}
        {resolved}
        {organizationSlug}
        {workspaceSlug}
        {onpreview}
        {onpreviewclose}
      />{:else}<p class="muted">No open initiatives in this view.</p>{/if}
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
            <p class="muted">
              {#if date(item.ts)}<time datetime={item.ts}>{date(item.ts)}</time
                >{:else}<UnavailableValue
                  reason="This activity row carries no timestamp."
                />{/if}
            </p>
          </li>
        {/each}
      </ol>
    {:else}<p class="muted">No recent activity in this view.</p>{/if}
  {:else if panel.type === "live-fleet-health"}
    <div class="fleet-summary">
      <strong>{live.data.active_host_count ?? 0} active hosts</strong>
      <span class="muted">{live.data.host_count ?? 0} enrolled</span>
    </div>
    {#if fleetHosts.length}
      <ul class="rows">
        {#each fleetHosts as host}
          <li>
            <strong>{host.display_name || host.slug}</strong>
            <p class="muted">
              {host.hostname || host.slug} · {host.agent_count} agents
              {host.revoked_at ? " · Revoked" : ""}
            </p>
            {#if host.discovered_adapters?.length}
              <p class="muted">
                Adapters: {host.discovered_adapters.join(", ")}
              </p>
            {/if}
          </li>
        {/each}
      </ul>
    {:else}
      <p class="muted">No hosts have been enrolled.</p>
    {/if}
    {#if live.data.enrollment_available}
      <h4>Enrollment requests ({live.data.enrollment_count ?? 0})</h4>
      {#if fleetEnrollments.length}
        <ul class="rows">
          {#each fleetEnrollments as enrollment}
            <li>
              <strong>{enrollment.requested_slug}</strong>
              <p class="muted">
                {enrollment.status} · expires {#if date(enrollment.expires_at)}{date(
                    enrollment.expires_at,
                  )}{:else}<UnavailableValue
                    reason="No expiry recorded for this request."
                  />{/if}
              </p>
            </li>
          {/each}
        </ul>
      {/if}
    {:else if live.data.enrollment_message}
      <p class="muted">{live.data.enrollment_message}</p>
    {/if}
    <h4>Fleet series</h4>
    {#if fleetSeries.length}
      <ul class="rows">
        {#each fleetSeries as metric}
          <li>
            <strong>{metric.name}</strong>
            <p class="muted">
              {metric.status} · {metric.adapter} on {metric.host} · last push {#if date(metric.last_push)}{date(
                  metric.last_push,
                )}{:else}<UnavailableValue
                  reason="This series has never been pushed."
                />{/if}
            </p>
            {#each metric.streams ?? [] as stream}
              {@const observedAt = date(
                stream.latest?.observed_at || stream.last_observed_at,
              )}
              <p>
                {Object.entries(stream.labels ?? {})
                  .map(([key, value]) => key + "=" + value)
                  .join(" · ")}
                {#if fleetValue(stream.latest) !== null}{fleetValue(
                    stream.latest,
                  )}
                  {metric.unit}{:else}<UnavailableValue
                    reason="No recent reading from this host."
                  />{/if}
                {#if observedAt}<span class="muted">· {observedAt}</span>{/if}
              </p>
            {/each}
            {#if metric.message}<p class="muted">{metric.message}</p>{/if}
          </li>
        {/each}
      </ul>
    {:else}
      <p class="muted">
        {live.data.series_message || "No fleet.* series are available."}
      </p>
    {/if}
  {/if}
  {#if live?.truncated}<p class="partial">
      Partial view. More records may exist beyond this panel’s limit.
    </p>{/if}
  {#if live?.observed_at}
    <p class="observed">
      Live as of {#if date(live.observed_at)}<time datetime={live.observed_at}
          >{date(live.observed_at)}</time
        >{:else}<UnavailableValue
          reason="This read carries no observation time."
        />{/if}
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
    /* An implicit column sizes to its widest child's max-content. */
    grid-template-columns: minmax(0, 1fr);
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
    /* An implicit column sizes to its widest child's max-content. */
    grid-template-columns: minmax(0, 1fr);
    gap: 12px;
    margin-top: 16px;
  }
  .fleet-summary {
    display: flex;
    gap: 8px;
    align-items: baseline;
    margin-bottom: 14px;
  }
  h4 {
    margin: 18px 0 8px;
    font-size: 12px;
    font-weight: 600;
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
