package app

import "strings"

func wakeRoutingGuideText() string {
	const tickToken = "<<tick>>"
	guide := strings.TrimSpace(`Wake routing

Use this when you want humans or agents to wake other agents from thread messages by tagging <<tick>>@handle<<tick>>.

How it works

- Wake routing is implemented by the adapter bridge layer, not by <<tick>>oar-core<<tick>> itself.
- The durable registration document id is <<tick>>agentreg.<handle><<tick>>.
- The bridge-owned readiness proof is the latest <<tick>>agent_bridge_checked_in<<tick>> event referenced by <<tick>>agentreg.<handle><<tick>>.
- A tagged message only becomes durable wake work when the target agent is both registered and bridge-ready.

What counts as wakeable

- principal kind is <<tick>>agent<<tick>>
- principal is not revoked
- principal has a username/handle
- registration document <<tick>>agentreg.<handle><<tick>> exists
- registration document <<tick>>actor_id<<tick>> matches the principal actor
- registration has an enabled binding for the current workspace
- registration status is <<tick>>active<<tick>>
- registration records a bridge check-in event id
- that <<tick>>agent_bridge_checked_in<<tick>> event exists, matches the same actor, and has a fresh bridge check-in window

Important lifecycle rule

- An agent must not become taggable before the bridge is actually running and has checked in.
- Bridge-managed registrations therefore start as <<tick>>pending<<tick>>.
- The bridge flips them to active/fresh on check-in.
- If the bridge stops checking in, the registration becomes stale and routing stops treating it as wakeable.

How humans discover it

- In the web UI Access page, look for agent principals marked Wakeable and their <<tick>>@handle<<tick>>.
- If Access shows pending or stale bridge state, do not expect tagging to work yet.

How agents discover it

- Read this topic with <<tick>>oar meta doc wake-routing<<tick>>.
- Read the preferred runtime path with <<tick>>oar meta doc agent-bridge<<tick>>.
- Use <<tick>>oar help bridge<<tick>> to bootstrap the runtime from the main CLI.
- Use <<tick>>oar bridge workspace-id --handle <handle><<tick>> when an existing registration doc is the easiest source of truth for the durable workspace id.
- Use <<tick>>oar bridge import-auth --config ./agent.toml --from-profile <agent><<tick>> when matching <<tick>>oar<<tick>> auth already exists.
- Use <<tick>>oar auth whoami<<tick>> to confirm your current username and actor id.
- Use <<tick>>oar docs get --document-id agentreg.<handle> --json<<tick>> to inspect a registration document directly.

Preferred path when you are using <<tick>>oar-agent-bridge<<tick>>

1. Install the runtime:

  oar bridge install

2. Generate configs:

  oar bridge init-config --kind router --output ./router.toml --workspace-id <workspace-id>
  oar bridge init-config --kind hermes --output ./agent.toml --workspace-id <workspace-id> --handle <handle>

3. If matching <<tick>>oar<<tick>> auth already exists, import it into the bridge config:

  oar bridge import-auth --config ./agent.toml --from-profile <agent>

4. Register auth and write the initial pending registration when auth does not already exist:

  oar-agent-bridge auth register --config ./agent.toml --invite-token <token> --apply-registration

  If auth already exists and you only need to rewrite the registration document:

  oar-agent-bridge registration apply --config <agent.toml>

5. Start the router and target bridge:

  oar bridge start --config ./router.toml
  oar bridge start --config ./agent.toml

6. Verify the bridge has checked in before telling humans to use <<tick>>@handle<<tick>>:

  oar bridge status --config ./agent.toml
  oar bridge doctor --config ./agent.toml
  oar-agent-bridge registration status --config ./agent.toml

Generic OAR CLI lifecycle

If you are writing the document manually, only create the pending registration skeleton. Manual docs writes do not replace the live bridge-owned check-in event.

1. Confirm the identity you are registering:

  oar auth whoami

  Use the server-resolved username as <<tick>><handle><<tick>> and the server actor id as <<tick>><actor-id><<tick>>.

2. Resolve the durable workspace id you want to enable:

  - If an existing registration doc is available, start with <<tick>>oar bridge workspace-id --handle <handle><<tick>> or <<tick>>oar bridge workspace-id --document-id agentreg.<handle><<tick>>.
  - If you are configuring the router yourself, the source of truth is <<tick>>[oar] workspace_id<<tick>> in the router config.
  - If a router already exists, inspect that deployed router config and copy its <<tick>>workspace_id<<tick>> exactly.
  - If your deployment is driven by control-plane workspace records, copy the durable workspace id from that record, not the slug.
  - The bundled example value <<tick>>ws_main<<tick>> is only a sample.
  - Do not use a workspace slug or URL path segment. If you cannot determine the real value, stop and ask the operator.

3. Create a first-time registration payload such as <<tick>>wake-registration.json<<tick>>:

  {
    "document": {
      "document_id": "agentreg.<handle>",
      "title": "Agent registration @<handle>",
      "status": "pending",
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
      "status": "pending",
      "adapter_kind": "custom",
      "updated_at": "<current-utc-timestamp>",
      "workspace_bindings": [
        {
          "workspace_id": "<workspace-id>",
          "enabled": true
        }
      ]
    }
  }

4. For first-time registration, create the document:

  oar docs create --from-file wake-registration.json --json

5. If <<tick>>agentreg.<handle><<tick>> already exists, update it instead of retrying create:

  oar docs get --document-id agentreg.<handle> --json
  oar docs update --document-id agentreg.<handle> --from-file wake-registration-update.json --json

6. If <<tick>>docs create<<tick>> returns <<tick>>conflict<<tick>>, inspect the existing document and update it instead of retrying create blindly.

Registration schema notes

- Fields required for routing correctness are:
  - <<tick>>content.handle<<tick>> matching the principal username
  - <<tick>>content.actor_id<<tick>> matching the principal actor id
  - at least one enabled <<tick>>content.workspace_bindings[].workspace_id<<tick>> matching the router workspace id
- Bridge readiness fields are:
  - <<tick>>content.bridge_checkin_event_id<<tick>> points at the latest <<tick>>agent_bridge_checked_in<<tick>> event
  - <<tick>>content.bridge_signing_public_key_spki_b64<<tick>> stores the bridge-managed public proof key
  - that event payload includes <<tick>>bridge_instance_id<<tick>>, <<tick>>checked_in_at<<tick>>, and <<tick>>expires_at<<tick>>
  - that event payload also includes <<tick>>proof_signature_b64<<tick>>, which must verify against the registration's public proof key
- <<tick>>updated_at<<tick>> is advisory metadata. Set it to the current UTC time when creating or updating the registration, or let bridge-managed flows populate it.
- Do not hand-edit <<tick>>status = "active"<<tick>> before the bridge has actually checked in.
- Do not try to hand-author the bridge readiness proof. The supported path is to let the running bridge emit <<tick>>agent_bridge_checked_in<<tick>> and rewrite the registration.

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
  - <<tick>>workspace_bindings<<tick>> contains the current workspace id with <<tick>>enabled: true<<tick>>
  - <<tick>>status<<tick>> is <<tick>>active<<tick>>
  - <<tick>>bridge_checkin_event_id<<tick>> is present on the registration
  - <<tick>>oar events get --event-id <bridge-checkin-event-id> --json<<tick>> returns an <<tick>>agent_bridge_checked_in<<tick>> event
  - that event actor id matches the principal actor
  - that event <<tick>>expires_at<<tick>> is still in the future

5. If you are using <<tick>>oar-agent-bridge<<tick>>, prefer:

  oar bridge doctor --config ./agent.toml

Concrete wake example

1. Ensure the router and target bridge are running, and the bridge doctor reports the registration as wakeable.
2. Post a thread message containing <<tick>>@<handle><<tick>>, for example:

  @<handle> summarize the latest onboarding blockers.

3. Expected durable trace:
  - existing <<tick>>message_posted<<tick>>
  - new <<tick>>agent_wakeup_requested<<tick>>
  - new <<tick>>agent_wakeup_claimed<<tick>>
  - new bridge reply <<tick>>message_posted<<tick>>
  - new <<tick>>agent_wakeup_completed<<tick>>

Common failure modes

- unknown handle: no matching agent principal username exists
- missing registration: <<tick>>agentreg.<handle><<tick>> does not exist
- registration actor mismatch: the registration doc points at a different actor
- workspace not bound: registration exists but is not enabled for this workspace
- bridge not checked in: the registration is still pending
- stale bridge check-in: the bridge stopped refreshing readiness
- wrong workspace id: the registration uses a slug or another id that does not match the router configuration

Operational note

- This mechanism is discoverable from the CLI and UI, but actual wake dispatch is still owned by the <<tick>>adapters/agent-bridge<<tick>> runtime.

Next steps

  oar help bridge
  oar meta doc agent-bridge
  oar bridge doctor --config ./agent.toml`)
	return strings.ReplaceAll(guide, tickToken, "`")
}
