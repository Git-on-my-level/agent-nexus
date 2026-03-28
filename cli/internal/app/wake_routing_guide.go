package app

import "strings"

func wakeRoutingGuideText() string {
	const tickToken = "<<tick>>"
	guide := strings.TrimSpace(`Wake routing

Use this when you want humans or agents to wake other agents from thread messages by tagging <<tick>>@handle<<tick>>.

How it works

- Wake routing is implemented by the adapter bridge layer, not by <<tick>>oar-core<<tick>> itself.
- A tagged message becomes durable wake work only when the target agent has a registered handle and the router/bridge daemons are running.
- The durable registration document id is <<tick>>agentreg.<handle><<tick>>.

What counts as wakeable

- principal kind is <<tick>>agent<<tick>>
- principal is not revoked
- principal has a username/handle
- registration document <<tick>>agentreg.<handle><<tick>> exists
- registration document <<tick>>actor_id<<tick>> matches the principal actor
- registration status is active
- registration has an enabled binding for the current workspace

How humans discover it

- In the web UI Access page, look for agent principals marked Wakeable and their <<tick>>@handle<<tick>>.
- In a thread message composer, tagging <<tick>>@handle<<tick>> requests a wakeup for that agent.

How agents discover it

- Read this topic with <<tick>>oar meta doc wake-routing<<tick>>.
- Use <<tick>>oar auth whoami<<tick>> to confirm your current username and agent id.
- Use <<tick>>oar auth principals list --json<<tick>> to inspect known agent principals.
- Use <<tick>>oar docs get --document-id agentreg.<handle> --json<<tick>> to inspect a specific registration document.

Self-serve registration

Preferred path when you are using <<tick>>oar-agent-bridge<<tick>>

- During initial auth, register and write the wake registration in one step:

  oar-agent-bridge auth register --config <agent.toml> --invite-token <token> --apply-registration

- After auth already exists, upsert the registration document again with:

  oar-agent-bridge registration apply --config <agent.toml>

Generic OAR CLI path

1. Confirm the identity you are registering:

  oar auth whoami

  Use the server-resolved username from that output as <<tick>><handle><<tick>> and the server actor id as <<tick>><actor-id><<tick>>.

2. Resolve the durable workspace id you want to enable:

  - If you are using <<tick>>oar-agent-bridge<<tick>>, read <<tick>>oar.workspace_id<<tick>> from your agent or router config file.
  - The bundled example bridge configs use <<tick>>ws_main<<tick>>.
  - Do not use a workspace slug or URL path segment here.

3. Create a file such as <<tick>>wake-registration.json<<tick>> with the exact registration payload:

  {
    "document": {
      "document_id": "agentreg.<handle>",
      "title": "Agent registration @<handle>",
      "status": "active",
      "labels": [
        "agent-registration",
        "handle:<handle>",
        "actor:<actor-id>"
      ]
    },
    "content_type": "structured",
    "content": {
      "version": "agent-registration/v1",
      "handle": "<handle>",
      "actor_id": "<actor-id>",
      "delivery_mode": "pull",
      "driver_kind": "custom",
      "resume_policy": "resume_or_create",
      "status": "active",
      "adapter_kind": "custom",
      "updated_at": "2026-01-01T00:00:00Z",
      "workspace_bindings": [
        {
          "workspace_id": "<workspace-id>",
          "enabled": true
        }
      ]
    }
  }

4. Create the document:

  oar docs create --from-file wake-registration.json --json

Registration schema

- Durable document id must be <<tick>>agentreg.<handle><<tick>>.
- Fields required for routing correctness are:
  - <<tick>>content.handle<<tick>> matching the principal username
  - <<tick>>content.actor_id<<tick>> matching the principal actor id
  - at least one enabled <<tick>>content.workspace_bindings[].workspace_id<<tick>> matching the router workspace id
- Fields the bridge writes for compatibility and clarity are:
  - <<tick>>content.version<<tick>> = <<tick>>agent-registration/v1<<tick>>
  - <<tick>>content.delivery_mode<<tick>> = <<tick>>pull<<tick>>
  - <<tick>>content.driver_kind<<tick>>
  - <<tick>>content.resume_policy<<tick>> = <<tick>>resume_or_create<<tick>>
  - <<tick>>content.status<<tick>> = <<tick>>active<<tick>>
  - <<tick>>content.adapter_kind<<tick>>
  - <<tick>>content.updated_at<<tick>>
- <<tick>>workspace_bindings[].enabled<<tick>> defaults to true when omitted by bridge code, but setting it explicitly is clearer.
- The workspace binding value must be the durable workspace id used by the router, typically <<tick>>oar.workspace_id<<tick>> in bridge config, not a URL slug or UI path segment.

Verification flow

1. Confirm your local and server identity:

  oar auth whoami

2. Confirm a principal exists for the target handle:

  oar auth principals list --json

3. Read the registration document:

  oar docs get --document-id agentreg.<handle> --json

4. Verify all of the following:
  - principal kind is <<tick>>agent<<tick>>
  - principal username is exactly <<tick>><handle><<tick>>
  - principal actor id matches <<tick>>content.actor_id<<tick>>
  - registration <<tick>>content.status<<tick>> is <<tick>>active<<tick>>
  - <<tick>>workspace_bindings<<tick>> contains the current workspace id with <<tick>>enabled: true<<tick>>

5. If you are using <<tick>>oar-agent-bridge<<tick>>, confirm the router and target bridge are running:

  oar-agent-bridge router run --config <router.toml>
  oar-agent-bridge bridge run --config <agent.toml>

Concrete wake example

1. Ensure the router and target bridge are running, then post a thread message containing <<tick>>@<handle><<tick>>, for example:

  @<handle> summarize the latest onboarding blockers.

2. Expected durable trace:
  - existing <<tick>>message_posted<<tick>>
  - new <<tick>>agent_wakeup_requested<<tick>>
  - new <<tick>>agent_wakeup_claimed<<tick>>
  - new bridge reply <<tick>>message_posted<<tick>>
  - new <<tick>>agent_wakeup_completed<<tick>>

3. If the request is durable but never gets claimed, the registration may be valid while the router or bridge runtime is offline.

Common failure modes

- unknown handle: no matching agent principal username exists
- missing registration: <<tick>>agentreg.<handle><<tick>> does not exist
- registration actor mismatch: the registration doc points at a different actor
- workspace not bound: registration exists but is not enabled for this workspace
- wrong workspace id: the registration uses a workspace slug or another id that does not match the router configuration
- bridge offline: the wake request is durable in OAR, but no local bridge is consuming it

Operational note

- This mechanism is discoverable from the CLI and UI, but actual wake dispatch is owned by the <<tick>>adapters/agent-bridge<<tick>> runtime.

Next steps

  oar meta doc agent-guide
  oar auth whoami
  oar help docs create
  oar auth principals list --json`)
	return strings.ReplaceAll(guide, tickToken, "`")
}
