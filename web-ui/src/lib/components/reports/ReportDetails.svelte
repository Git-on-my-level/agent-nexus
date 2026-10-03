<script>
  let { panel } = $props();
  function sparkline(values) {
    const minimum = Math.min(...values);
    const maximum = Math.max(...values);
    const range = maximum - minimum || 1;
    return values
      .map(
        (value, i) =>
          `${(i / (values.length - 1)) * 160},${36 - ((value - minimum) / range) * 30}`,
      )
      .join(" ");
  }
</script>

{#if panel.type === "callout"}
  <div class="callout" data-tone={panel.data.tone}>
    <span class="callout-symbol" aria-hidden="true"
      >{{ info: "i", success: "✓", warning: "!", critical: "!" }[
        panel.data.tone
      ]}</span
    >
    <div>
      <p class="callout-label">{panel.data.label ?? panel.data.tone}</p>
      <p class="callout-text">{panel.data.text}</p>
    </div>
  </div>
{:else if panel.type === "metric-strip"}
  <div class="metrics">
    {#each panel.data.items as item}
      <div class="metric" data-tone={item.tone ?? "neutral"}>
        <p class="metric-label">{item.label}</p>
        <strong class="metric-value">{item.value}</strong>
        <p class="metric-detail">{item.detail}</p>
        {#if item.trend}
          <svg
            viewBox="0 0 160 42"
            role="img"
            aria-label={`${item.trend_label}: ${item.trend.join(", ")}`}
          >
            <title>{item.trend_label}</title>
            <line x1="0" x2="160" y1="39" y2="39" stroke="var(--line)" />
            <polyline
              points={sparkline(item.trend)}
              fill="none"
              stroke="currentColor"
              stroke-width="2"
            />
          </svg>
          <p class="trend-label">{item.trend_label}</p>
        {/if}
      </div>
    {/each}
  </div>
{:else if panel.type === "comparison"}
  <div class="comparisons">
    {#each panel.data.items as item}
      <article class="comparison" data-verdict={item.verdict}>
        <div class="comparison-heading">
          <h4>{item.title}</h4>
          <span
            >{item.verdict === "neutral" ? "Alternative" : item.verdict}</span
          >
        </div>
        <p class="comparison-summary">{item.summary}</p>
        <dl>
          {#each item.attributes as attribute}<div>
              <dt>{attribute.label}</dt>
              <dd>{attribute.value}</dd>
            </div>{/each}
        </dl>
      </article>
    {/each}
  </div>
{/if}

<style>
  .callout {
    display: flex;
    gap: 14px;
    align-items: flex-start;
    padding: 4px 0;
  }
  .callout-symbol {
    display: grid;
    place-items: center;
    flex: none;
    width: 26px;
    height: 26px;
    border: 1px solid var(--line-strong);
    border-radius: 50%;
    font-weight: 600;
    color: var(--accent-text);
  }
  .callout-label {
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--accent-text);
    margin: 4px 0 8px;
  }
  .callout[data-tone="warning"] .callout-symbol,
  .callout[data-tone="warning"] .callout-label {
    color: var(--warn-text);
  }
  .callout[data-tone="critical"] .callout-symbol,
  .callout[data-tone="critical"] .callout-label {
    color: var(--danger-text);
  }
  .callout-text {
    font-size: 14px;
    line-height: 1.7;
    white-space: pre-line;
  }
  .metrics {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 150px), 1fr));
    gap: 24px;
  }
  .metric {
    min-width: 0;
  }
  .metric-label {
    font-size: 11px;
    color: var(--fg-muted);
  }
  .metric-value {
    display: block;
    font-size: clamp(26px, 3vw, 40px);
    letter-spacing: -0.055em;
    line-height: 1.25;
    margin: 8px 0;
    font-weight: 500;
  }
  .metric-detail {
    font-size: 11px;
    line-height: 1.5;
    color: var(--fg-muted);
  }
  .metric svg {
    display: block;
    width: 100%;
    max-width: 200px;
    margin-top: 12px;
    color: var(--accent-text);
  }
  .metric[data-tone="negative"] svg {
    color: var(--warn-text);
  }
  .trend-label {
    font-size: 10px;
    color: var(--fg-muted);
    margin-top: 4px;
  }
  .comparisons {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 220px), 1fr));
    gap: 24px;
  }
  .comparison {
    min-width: 0;
  }
  .comparison-heading {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  h4 {
    font-size: 16px;
    font-weight: 500;
    letter-spacing: -0.02em;
  }
  .comparison-heading span {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--fg-muted);
  }
  [data-verdict="recommended"] .comparison-heading span {
    color: var(--accent-text);
  }
  [data-verdict="caution"] .comparison-heading span {
    color: var(--warn-text);
  }
  .comparison-summary {
    font-size: 12px;
    line-height: 1.7;
    color: var(--fg-muted);
    margin: 12px 0;
  }
  dl > div {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
    padding: 10px 0;
    border-top: 1px solid var(--line-subtle);
    font-size: 11px;
    line-height: 1.5;
  }
  dt {
    color: var(--fg-muted);
  }
  dd {
    text-align: right;
  }
</style>
