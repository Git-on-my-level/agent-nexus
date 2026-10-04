# Host-local live data adapters

Copy these Python 3 scripts to an enrolled host. They fetch data there and push
through `anx`; core has no source URL or third-party credential. Use the enrolled
agent that owns the declaration. Keep `gh` authentication and Prometheus tokens
on that host. Every process with access to its shared host key can request that
agent identity; a scoped series token itself allows only declared point pushes.

Declare the adapter first (human or explicitly granted auth-admin identity):

```json
{
  "name": "github",
  "description": "Weekly PR work grouped into initiatives",
  "agent_id": "collector.my-host",
  "expected_interval": "24h",
  "series": [{"name": "github-prs", "kind": "gauge", "unit": "PRs"}]
}
```

Save as `github.json`, then `anx adapters declare --body-file github.json`.
On the owner host, `ANX_ADAPTER=github python3 github-prs.py owner/repo` pushes
weekly merged and currently open PR counts using the [GitHub pull requests API](https://docs.github.com/en/rest/pulls/pulls#list-pull-requests). A historical week is timestamped at
its end; the current week is timestamped now. Twelve weeks fit raw retention.
Currently open PR counts are grouped by creation week. As those PRs merge or
close, the adapter corrects their raw weekly counts under its series-write grant.
The last push remains visible as provenance; exact retries are idempotent.
Optional `--rules rules.json` groups by label or title rules, first match wins;
otherwise `initiative:*` labels win, then `other`. Empty weeks push zero.

For Prometheus, declare `latency` as a gauge and an adapter owned by your collector:

```sh
ANX_ADAPTER=prometheus PROMETHEUS_URL=https://prometheus.internal \
  python3 prometheus-query.py latency 'sum(rate(http_requests_total[5m]))'
# Optional PROMETHEUS_TOKEN stays on the host. Range queries keep original timestamps:
ANX_ADAPTER=prometheus PROMETHEUS_URL=https://prometheus.internal \
  python3 prometheus-query.py latency 'sum(rate(http_requests_total[5m]))' \
  --range-seconds 3600 --step-seconds 300
```

Only explicit `--labels job,instance` keys are forwarded. Narrow/aggregate the
PromQL query to fit ANX's caps. See the [Prometheus API](https://prometheus.io/docs/prometheus/latest/querying/api/).
The shared push helper waits 60 seconds on `series_rate_limited` and retries the
same timestamped observation at most three times. Other caps and authentication
errors stop the run. Large backfills may span several minutes under the adapter's
1,200 requests/minute budget; avoid overlapping scheduled runs.

For a generic command, declare its series and run:

```sh
ANX_ADAPTER=command python3 command.py pending-jobs -- ./count-jobs
anx series push pending-jobs --from-command -- ./count-jobs
# stdout may be a number or {"series":"pending-jobs","state":"healthy"}.
```

No additional `anx-hosts` collector is needed: enrolled host/agent presence is
already native ANX data (`anx host list`, `anx agents list` and the Agents page).
Prefer that live source for fleet health over a duplicate heartbeat series.

## Scheduling

Each adapter needs a matching declaration and interval. Example cron snippets
(use absolute paths and the enrolled OS user's environment; protect local env
files with mode 600):

```cron
# github-prs: expected_interval=24h
0 8 * * * ANX_AS=collector ANX_ADAPTER=github /usr/bin/python3 /opt/anx-adapters/github-prs.py owner/repo >> /var/log/anx-github.log 2>&1
# prometheus-query: expected_interval=5m; source URL/token supplied locally
*/5 * * * * ANX_AS=collector ANX_ADAPTER=prometheus /opt/anx-adapters/run-prometheus >> /var/log/anx-prometheus.log 2>&1
# command: expected_interval=1m
* * * * * ANX_AS=collector ANX_ADAPTER=command /usr/bin/python3 /opt/anx-adapters/command.py pending-jobs -- /opt/jobs/count >> /var/log/anx-command.log 2>&1
```

For Hermes, schedule the same argv/command at 24h, 5m, or 1m with the owning
agent workspace selected. Read `hermes cron --help` for your installed version's
schedule syntax. The collector is a host process, not a core scheduler.
