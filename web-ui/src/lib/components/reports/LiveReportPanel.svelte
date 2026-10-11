<script>
  import LiveInitiativeDetails from "./LiveInitiativeDetails.svelte";
  import UnavailableValue from "$lib/components/UnavailableValue.svelte";
  import WorkSummary from "$lib/components/WorkSummary.svelte";
  import { workProse, workSummaryModel } from "$lib/workSummary.js";
  import { inboxItemMailboxId } from "$lib/inboxUtils.js";
  import { LIVE_REPORT_TYPES, formatLiveAge } from "$lib/liveReports.js";
  import Time from "$lib/time/Time.svelte";
  import { formatElapsed, instantIso, instantMs } from "$lib/time/format.js";
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
  /*
   * Does a row need to say which state it is in?
   *
   * Not for a panel of open asks: its own heading says "Needs an answer" and
   * repeating it on every row said it N+1 times. But a panel may be queried
   * with `include_answered` or `answered_only`, and a row that has been
   * answered must say so — including when every row has, where "no row says
   * anything" would read as a pile of work still waiting.
   */
  let asksShowState = $derived(
    items.some((item) => item.status === "answered"),
  );
  let buckets = $derived(live?.data?.buckets ?? []);
  let fleetHosts = $derived(live?.data?.hosts ?? []);
  let fleetEnrollments = $derived(live?.data?.enrollments ?? []);
  let fleetSeries = $derived(live?.data?.series ?? []);
  let maxCount = $derived(Math.max(1, ...buckets.map((item) => item.count)));
  /**
   * A fleet reading. `null` when the host has not reported one, so the row
   * shows a dash with the reason rather than a sentence where a value goes.
   */
  const fleetValue = (latest) => {
    if (latest?.state !== undefined) return latest.state;
    if (latest?.value !== undefined) return String(latest.value);
    return null;
  };
  /**
   * Remaining length of an enrollment, not a relative instant: "expires in 41m"
   * while it is still open, "expired" once the deadline has passed.
   */
  const enrollmentExpiry = (expiresAt) => {
    const at = instantMs(expiresAt);
    if (at == null) return "";
    return at <= now ? "expired" : `expires in ${formatElapsed(at - now)}`;
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
              <!-- The state word only where the panel holds both kinds. A
                   view of open asks says "Needs an answer" in its own
                   heading; repeating it on every row said it N+1 times. -->
              {#if asksShowState}{item.status === "answered"
                  ? "Answered"
                  : "Needs an answer"} ·
              {/if}{formatLiveAge(item.age_seconds, now)}
            </p>
            {#if item.response_text}<p>{item.response_text}</p>{/if}
          </li>
        {/each}
      </ul>
    {:else}<p class="muted">No asks in this view.</p>{/if}
  {:else if panel.type === "live-cards"}
    <!--
      Cards matching a filter. One line of what it is and where it stands,
      through the one summary renderer: this panel used to print the stored
      phase, so the same card could read "in progress" here and Blocked on
      the Overview.
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
              <WorkSummary
                summary={workSummaryModel(item, { now })}
                density="row"
                title={item.title}
                {now}
              />
              {#if item.priority}<span>{item.priority.toUpperCase()}</span>{/if}
              {#if instantIso(item.updated_at)}<span
                  >· updated <Time value={item.updated_at} {now} /></span
                >{/if}
            </p>
            {#if workProse(item)}<p>{workProse(item)}</p>{/if}
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
              {#if instantIso(item.ts)}<Time
                  value={item.ts}
                  {now}
                />{:else}<UnavailableValue
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
            {@const expiry = enrollmentExpiry(enrollment.expires_at)}
            <li>
              <strong>{enrollment.requested_slug}</strong>
              <p class="muted">
                {enrollment.status} · {#if expiry}{expiry}{:else}<UnavailableValue
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
              {metric.status} · {metric.adapter} on {metric.host} · last push {#if instantIso(metric.last_push)}<Time
                  value={metric.last_push}
                  {now}
                />{:else}<UnavailableValue
                  reason="This series has never been pushed."
                />{/if}
            </p>
            {#each metric.streams ?? [] as stream}
              {@const observedAt =
                stream.latest?.observed_at || stream.last_observed_at || ""}
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
                {#if instantIso(observedAt)}<span class="muted"
                    >· <Time value={observedAt} {now} /></span
                  >{/if}
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
            {#if workProse(item)}<p>{workProse(item)}</p>{/if}
            {#if item.detail}<p>{item.detail}</p>{/if}
            {#if instantIso(at)}<p class="muted">
                <Time value={at} {now} />
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
