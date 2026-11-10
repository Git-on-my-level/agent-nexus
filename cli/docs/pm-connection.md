# PM connection diagnostics

`pm install` authenticates the selected profile and calls `POST /pm/connect`
before replacing a service definition or touching launchd/systemd. This is a real
connection: it registers the selected actor and updates presence. A rejected or
unavailable connection fails install, preserves any existing service, and records
the failure locally. Older cores returning 404 retain claim-based onboarding.

Preflight success proves connect authorization at that instant. Only `--wait`
confirms that the launched service has made a fresh accepted claim and heartbeat.
Installed and running describe service-manager state, not PM health.

An authenticated agent whose actor differs from the server's configured
`ANX_PM_AGENT_ACTOR_ID` receives HTTP 403 `pm_actor_mismatch`. The error names
`configured_actor_id` and `acting_actor_id`. A workspace operator must rebind
`ANX_PM_AGENT_ACTOR_ID` to the acting actor and restart `anx-core`. Verify that
the previous runner should be replaced before changing the binding. Successful
`auth whoami` alone does not repair the binding. No hosted service is required;
this is the same deployment configuration for self-hosted workspaces.

`pm serve` records connect failures in mode-0600 `last-connect.json` under its
work directory. `pm status` exposes `last_connect_failure` (code, timestamp,
HTTP status, retryability, repair hint and mismatch actor IDs) in JSON and text. A successful
install preflight or runner connect clears the failure. Missing history on an
older installation means unknown, not healthy, as does unreadable, corrupt or
oversized history. Hints are generated locally from the failure code; remote
hints and response bodies are never saved or displayed from this diagnostic,
including older records. Actor IDs are restricted to bounded identifiers.
Reads refuse symlinked files; writes refuse symlinked or foreign-owned diagnostic
directories. A diagnostic write failure is logged and does not stop serve.

On a non-retryable connect authorization failure, serve remains alive and retries
after 1, 2, 4, 8, then at most 15 minutes, until the binding is repaired or the
process receives SIGTERM/interrupt. Retryable connect failures, including network
outages and HTTP 503, retry after 1, 2, 4 seconds and so on, capped at 2 minutes.
Both waits are interruptible. It does not claim work until connected. This
prevents KeepAlive from repeatedly restarting a permanently denied service.
Other non-retryable startup errors retain the existing failure behavior.
