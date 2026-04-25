package app

import "strings"

func wakeRoutingGuideText() string {
	const tickToken = "<<tick>>"
	guide := strings.TrimSpace(`Wake routing

Use this when you want humans or agents to wake other agents from thread messages by tagging <<tick>>@handle<<tick>>.

How it works

- Wake routing is provided by a workspace-owned sidecar hosted inside <<tick>>anx-core<<tick>>, not by the per-agent CLI.
- The durable wake registration now lives on the agent principal metadata, not in <<tick>>docs<<tick>>.
- The bridge-owned readiness proof is the latest <<tick>>agent_bridge_checked_in<<tick>> event referenced by that principal registration.
- A tagged message becomes durable wake work when the target agent is registered for the workspace. Bridge readiness only changes whether delivery is immediate or queued.

What counts as taggable

- principal kind is <<tick>>agent<<tick>>
- principal is not revoked
- principal has a username/handle
- principal has wake registration metadata
- registration <<tick>>actor_id<<tick>> matches the principal actor
- registration has an enabled binding for the current workspace
- registration status is <<tick>>active<<tick>>

What counts as online

- the agent is already taggable
- registration records a bridge check-in event id
- that <<tick>>agent_bridge_checked_in<<tick>> event exists, matches the same actor, and has a fresh bridge check-in window

Important lifecycle rule

- Bridge-managed registrations still start as <<tick>>pending<<tick>> until the bridge checks in and finalizes the live registration payload.
- Once registration and workspace binding are valid, humans can tag the agent even if the bridge is offline.
- If the bridge stops checking in, the agent becomes offline but remains taggable; pending notifications queue until the bridge returns.

How humans discover it

- In the web UI Access page, look for registered agent principals and their <<tick>>@handle<<tick>>.
- <<tick>>Online<<tick>> means immediate delivery is available now. <<tick>>Offline<<tick>> means tags still queue durable notifications for later delivery.

How agents discover it

- Read this topic with <<tick>>anx meta doc wake-routing<<tick>>.
- Read the preferred runtime path with <<tick>>anx meta doc agent-bridge<<tick>>.
- Use <<tick>>anx help bridge<<tick>> to bootstrap the per-agent bridge runtime from the main CLI.
- Use <<tick>>anx bridge workspace-id --handle <handle><<tick>> when an existing registration is the easiest source of truth for the durable workspace id.
- Use <<tick>>anx bridge import-auth --config ./agent.toml --from-profile <agent><<tick>> when matching <<tick>>anx<<tick>> auth already exists.
- Use <<tick>>anx notifications list --status unread<<tick>> to inspect queued notifications with the main CLI.
- Use <<tick>>anx notifications dismiss --wakeup-id <wakeup-id><<tick>> to dismiss a notification so it no longer wakes the bridge.
- Use <<tick>>anx auth whoami<<tick>> to confirm your current username and actor id.
- Use <<tick>>anx auth principals list --handles-only<<tick>> to inspect the exact handles that can be mentioned.
- Use <<tick>>anx auth principals list --taggable<<tick>> if you want the filtered principal rows as well.
- Use <<tick>>anx auth principals list<<tick>> for readable rows; add <<tick>>--json<<tick>> when you need the full wake-routing metadata in a parseable envelope (scripts, debugging).

Preferred path when you are using <<tick>>anx-agent-bridge<<tick>>

1. Install the runtime:

  anx bridge install

2. Confirm the workspace deployment's <<tick>>anx-core<<tick>> config and note the durable workspace id it uses.

3. Generate the agent config:

  anx bridge init-config --kind hermes --output ./agent.toml --workspace-id <workspace-id> --handle <handle> --workspace-path /absolute/path/to/hermes/workspace

  If you omit <<tick>>--workspace-path<<tick>>, the generated Hermes config keeps placeholder paths and must be edited before the bridge can start.

4. If matching <<tick>>anx<<tick>> auth already exists, import it into the bridge config:

  anx bridge import-auth --config ./agent.toml --from-profile <agent>

  This also syncs the default local <<tick>>[anx].base_url<<tick>> in the bridge config to the imported profile when they differ.

5. Register auth and write the initial pending registration when auth does not already exist:

  anx-agent-bridge auth register --config ./agent.toml --invite-token <token> --apply-registration

  If auth already exists and you only need to rewrite the principal registration:

  anx-agent-bridge registration apply --config <agent.toml>

6. Start the target bridge:

  anx bridge start --config ./agent.toml

7. Verify the bridge has checked in before expecting immediate delivery:

  anx bridge status --config ./agent.toml
  anx bridge doctor --config ./agent.toml
  anx-agent-bridge registration status --config ./agent.toml

8. Pull or dismiss queued notifications directly when needed:

  anx notifications list --status unread
  anx-agent-bridge notifications list --config ./agent.toml --status unread
  anx notifications dismiss --wakeup-id <wakeup-id>

9. If the bridge is online but tagged delivery still does not work, ask the workspace operator to inspect the embedded wake-routing sidecar in <<tick>>anx-core<<tick>>.

Generic ANX CLI lifecycle

If you are writing registration state manually, update the agent principal registration only. Manual principal updates do not replace the live bridge-owned check-in event.

1. Confirm the identity you are registering:

  anx auth whoami

  Use the server-resolved username as <<tick>><handle><<tick>> and the server actor id as <<tick>><actor-id><<tick>>.

2. Resolve the durable workspace id you want to enable:

  - If an existing registration is available, start with <<tick>>anx bridge workspace-id --handle <handle><<tick>>.
  - If the workspace deployment already documents the configured <<tick>>workspace_id<<tick>>, copy that exact value.
  - If your deployment is driven by control-plane workspace records, copy the durable workspace id from that record, not the slug.
  - The bundled example value <<tick>>ws_main<<tick>> is only a sample.
  - Do not use a workspace slug or URL path segment. If you cannot determine the real value, stop and ask the operator.

3. Create a first-time registration payload such as <<tick>>wake-registration.json<<tick>>:

  {
    "registration": {
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

4. For first-time registration, patch the current authenticated agent:

  curl -X PATCH "$ANX_BASE_URL/agents/me" \
    -H "Authorization: Bearer <access-token>" \
    -H "Content-Type: application/json" \
    --data @wake-registration.json

5. If auth already exists, prefer the supported bridge-managed path instead of hand-patching:

  anx-agent-bridge registration apply --config ./agent.toml

Registration schema notes

- Fields required for routing correctness are:
  - <<tick>>content.handle<<tick>> matching the principal username
  - <<tick>>content.actor_id<<tick>> matching the principal actor id
  - at least one enabled <<tick>>content.workspace_bindings[].workspace_id<<tick>> matching the current workspace id
- Bridge readiness fields are:
  - <<tick>>content.bridge_checkin_event_id<<tick>> points at the latest <<tick>>agent_bridge_checked_in<<tick>> event
  - <<tick>>content.bridge_signing_public_key_spki_b64<<tick>> stores the bridge-managed public proof key
  - that event payload includes <<tick>>bridge_instance_id<<tick>>, <<tick>>checked_in_at<<tick>>, and <<tick>>expires_at<<tick>>
  - that event payload also includes <<tick>>proof_signature_b64<<tick>>, which must verify against the registration's public proof key
- <<tick>>updated_at<<tick>> is advisory metadata. Set it to the current UTC time when creating or updating the registration, or let bridge-managed flows populate it.
- Do not hand-edit <<tick>>status = "active"<<tick>> before the bridge has actually checked in.
- Do not try to hand-author the bridge readiness proof. The supported path is to let the running bridge emit <<tick>>agent_bridge_checked_in<<tick>> and refresh the registration.

Verification flow

1. Confirm your local and server identity:

  anx auth whoami

2. Confirm a principal exists for the target handle:

  anx auth principals list --handles-only

3. Read the principal registration (<<tick>>--json<<tick>> when a script parses the full payload):

  anx auth principals list --json

4. Verify all of the following:
  - principal kind is <<tick>>agent<<tick>>
  - principal username is exactly <<tick>><handle><<tick>>
  - principal actor id matches <<tick>>content.actor_id<<tick>>
  - <<tick>>workspace_bindings<<tick>> contains the current workspace id with <<tick>>enabled: true<<tick>>
  - <<tick>>status<<tick>> is <<tick>>active<<tick>>
  - if you need online delivery right now, <<tick>>bridge_checkin_event_id<<tick>> is present on the registration
  - if you need online delivery right now, <<tick>>anx events get --event-id <bridge-checkin-event-id><<tick>> (add <<tick>>--json<<tick>> for the CLI JSON envelope) returns an <<tick>>agent_bridge_checked_in<<tick>> event
  - if you need online delivery right now, that event actor id matches the principal actor
  - if you need online delivery right now, that event <<tick>>expires_at<<tick>> is still in the future

5. If you are using <<tick>>anx-agent-bridge<<tick>>, prefer:

  anx bridge doctor --config ./agent.toml

Concrete wake example

1. Ensure the target registration is valid for the workspace, and ensure the bridge is running if you want immediate delivery. The workspace deployment must also be running <<tick>>anx-core<<tick>> with the embedded wake-routing sidecar enabled.
2. Post a thread message containing <<tick>>@<handle><<tick>>, for example:

  @<handle> summarize the latest onboarding blockers.

3. Expected durable trace:
- existing <<tick>>message_posted<<tick>>
- new <<tick>>agent_wakeup_requested<<tick>>
- if online, new <<tick>>agent_wakeup_claimed<<tick>>
- if online, new bridge reply <<tick>>message_posted<<tick>>
- if online, new <<tick>>agent_wakeup_completed<<tick>>
- if offline, the <<tick>>agent_wakeup_requested<<tick>> stays pending until the bridge later claims it

Common failure modes

- unknown handle: no matching agent principal username exists
- missing registration: the agent principal does not have wake registration metadata
- registration actor mismatch: the registration points at a different actor
- workspace not bound: registration exists but is not enabled for this workspace
- bridge not checked in: the registration may still be pending, or the bridge may simply be offline for immediate delivery
- stale bridge check-in: the bridge stopped refreshing readiness, so delivery is queued until it returns
- wake-routing sidecar unavailable: the workspace deployment is not currently routing tagged messages
- wrong workspace id: the registration uses a slug or another id that does not match the workspace deployment

Operational note

- This mechanism is discoverable from the CLI and UI, but actual wake dispatch is owned by the workspace deployment's <<tick>>anx-core<<tick>> process plus the per-agent bridge runtime.

Next steps

  anx help bridge
  anx meta doc agent-bridge
  anx bridge doctor --config ./agent.toml`)
	return strings.ReplaceAll(guide, tickToken, "`")
}
