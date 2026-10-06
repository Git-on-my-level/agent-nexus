<script>
  import LiveInitiativeDetails from "./LiveInitiativeDetails.svelte";
  import UnavailableValue from "$lib/components/UnavailableValue.svelte";
  import { inboxItemMailboxId } from "$lib/inboxUtils.js";
  import { LIVE_REPORT_TYPES, formatLiveAge } from "$lib/liveReports.js";
  import { formatAge } from "$lib/ageBadge.js";
  import { bindWorkspaceHref } from "$lib/workspacePaths";
  let {
    panel,
    /** Reference time, so a row's age ticks with the rest of the report. */
    now = Date.now(),
    resolved = new Map(),
    organizationSlug = "",
    workspaceSlug = "",
    onpreview = null,
    onpreviewclose = null,
  } = $props();
  /** The live types with a view of their own in this component. */
  const KNOWN_TYPES = LIVE_REPORT_TYPES;
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
    // `new Date(null)` is the epoch, which is finite: without this guard a row
    // with no instant renders "1970-01-01 00:00 UTC" as if it were a reading.
    if (value === null || value === undefined || value === "") return "";
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
  {:else if panel.type === "live-cards"}
    <!--
      Cards matching a filter, not initiatives: no plan, no health, no
      progress bar. One line of what it is and where it stands, so a reader
      scanning ten rows is reading ten facts rather than ten summaries.
    -->
    {#if items.length}
      <ul class="rows">
        {#each items as item, index (item.ref ?? index)}
          <li>
            {#if canNavigate}<a
                href={workspaceHref(`/tasks/${encodeURIComponent(item.ref)}`)}
                >{item.title}</a
              >{:else}<strong class="row-title">{item.title}</strong>{/if}
            <p class="muted">
              {[
                String(item.phase ?? "").replaceAll("_", " "),
                item.priority ? item.priority.toUpperCase() : "",
              ]
                .filter(Boolean)
                .join(" · ")}
              <!-- `date(null)` is the epoch, which is finite; the age is the
                   honest test, and it is empty for a row with no instant. -->
              {#if formatAge(item.updated_at, now)}<span
                  >· updated <time datetime={item.updated_at}
                    >{formatAge(item.updated_at, now)}</time
                  ></span
                >{/if}
            </p>
            {#if item.summary}<p>{item.summary}</p>{/if}
            {#each item.needs ?? [] as need}<p class="partial">{need}</p>{/each}
          </li>
        {/each}
      </ul>
    {:else}<p class="muted">No cards match this filter.</p>{/if}
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
  {:else if !KNOWN_TYPES.includes(panel.type)}
    <!--
      A live type this renderer has no bespoke view for yet: core's
      `live-cards` and `live-timeline` land separately, and the generic shape
      every live panel shares is a list of rows with a title, a line of detail
      and an instant. Rendering that beats an empty panel, and a bespoke view
      can replace it without the reader ever seeing a blank box.
    -->
    {#if items.length}
      <ul class="rows">
        {#each items as item, index (item.id ?? item.ref ?? index)}
          {@const at = item.observed_at ?? item.ts ?? item.at ?? ""}
          {@const title = item.title ?? item.label ?? item.ref ?? ""}
          <li>
            {#if title}<strong class="row-title">{title}</strong>{/if}
            {#if item.summary}<p>{item.summary}</p>{/if}
            {#if item.detail}<p>{item.detail}</p>{/if}
            {#if date(at)}<p class="muted">
                <time datetime={at}>{date(at)}</time>
              </p>{/if}
          </li>
        {/each}
      </ul>
    {:else}<p class="muted">No records in this view.</p>{/if}
  {/if}
  {#if live?.truncated}<p class="partial">
      Partial view. More records may exist beyond this panel’s limit.
    </p>{/if}
  <!--
    The read time used to be a "Live as of 2026-10-06 12:00 UTC" line at the
    bottom of every live panel. It is in the header now, as "Live · updated 2m
    ago" with the instant on hover: the reader asks how current a panel is
    before reading it, not after, and an age answers that where a UTC
    timestamp has to be subtracted first.
  -->
</div>

<style>
  .live-panel {
    font-size: 12px;
    line-height: 1.6;
    overflow-wrap: anywhere;
  }
  .muted {
    color: var(--fg-muted);
    font-size: 11px;
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
  .row-title {
    text-align: left;
    font-weight: 600;
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
