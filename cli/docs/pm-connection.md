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
HTTP status, retryability and repair hint) in JSON and text. A successful runner
connect clears the failure. Missing history on an older installation means
unknown, not healthy. No response body, token or runner command is stored there.

On a non-retryable connect authorization failure, serve remains alive and retries
after 1, 2, 4, 8, then at most 15 minutes, until the binding is repaired or the
process receives SIGTERM/interrupt. It does not claim work while denied. This
prevents KeepAlive from repeatedly restarting a permanently denied service.
Other startup errors retain the existing failure behavior.
