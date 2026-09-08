# Contextual PM runtime and channels

`core/internal/pm` implements one durable PM service for web conversations,
Telegram messages and Discord interactions. It uses the existing Nexus thread,
event, wake artifact, queue and bridge session contracts. The provider responds
through the selected registered bridge agent; this package has no model catalog
and no fallback chat generator.

## Provider configuration

Use the installed `anx-agent-bridge` subprocess adapter with `pm_adapter.py` as
its entrypoint. Keep provider/model settings in the existing delegate adapter
configuration. For example, adapt an existing Hermes ACP bridge configuration:

```toml
[adapter]
kind = "subprocess"
command = ["python3", "adapters/pm/pm_adapter.py"]
timeout_seconds = 130
pm_timeout_seconds = 120
pm_max_output_bytes = 16000

[adapter.delegate]
command = ["python3", "-m", "anx_agent_bridge.adapters.hermes_acp"]

[adapter.delegate.adapter]
interactive = false
# Reuse your existing approved Hermes profile/runtime selection here.
# Configure any required environment explicitly via adapter.delegate.env.
```

Resolve executable/script paths against the deployment checkout. This example
contains no credentials and does not change an existing gateway or profile.
Use the provider you already operate, including an existing custom subprocess
adapter, rather than selecting a different model through this package.

The wrapper forwards the existing `anx-bridge-adapter-request/v1` protocol,
`session_key`, and `existing_native_session_id`. It rejects missing/expired PM
policies, empty results and incompatible responses. One absolute deadline
covers stdin, execution and stdout/stderr. Output and diagnostic bytes are
bounded; the process group is terminated on timeout and when descendants
outlive their parent. Only an explicit environment is passed to the delegate,
plus basic process variables. It does not silently forward ambient credentials.

**This is not a sandbox.** Process limits and an environment allowlist do not
restrict filesystem or network access. The core `NexusBridge.Ready` callback
must validate a currently registered bridge and a deployment-enforced PM
capability envelope. The selected PM runtime must use read-only context access;
external mutations belong exclusively to the separately authorized action
executor. Do not enable this lane on a general coding profile with unrestricted
shell/source credentials. Existing Hermes `interactive=false` denies ACP
permission prompts but is not, by itself, proof of source read-only isolation.
When the enforcement gate is unavailable, leave PM execution unconfigured;
requests return unavailable instead of a fabricated answer.

## Core composition

Use the same workspace SQLite connection:

1. `pm.NewStore(workspace.DB())` creates additive `pm_records` storage/indexes.
2. `pm.NexusBridge{Store: primitivesStore, ActorID: serviceActor, Ready: check}`
   provides `EnsureThread` and `Dispatch` callbacks for `pm.Dependencies`.
3. `pm.NewService` selects an existing registered PM actor/handle and sets
   timeout, output and concurrency limits. Defaults are two minutes, 16 KB and
   two conversations. One conversation has at most one active turn. Admission
   is atomic in SQL, including after process restart.
4. Supply current authorization and scoped tracker context callbacks. Bind
   `Handler.Authenticate` to the existing authenticated core principal. Never
   derive identity from request fields or a channel display name.
5. Mount `pm.Handler` under `/pm/` through the core business-route middleware.
6. On an existing bridge reply event, call `SyncBridgeReply` with its turn and
   event IDs. The event must match the selected actor, conversation thread and
   wake ID. A process/queue success alone cannot complete the conversation.
7. Call `DeliverPending` from the existing lifecycle/event mechanism to drain
   channel replies. It is bounded, replay-safe and starts no watcher. It also
   recovers a persisted assistant response whose outbox was not yet created.

PM history is scoped to the originating actor/conversation. A PM agent can use
`GET /pm/turns/{id}/context` to query bounded current evidence on behalf of the
requester. The callback must apply that requester's current authorization; the
PM agent's broader ambient privileges are not used for that query. Proposals
from `/pm/turns/{id}/decisions` are recorded for the requester. Only an
authenticated authorized human can answer/approve a decision.

## Channel identity and ingress

A channel binding is an explicit tuple:

```
transport + tenant_id + channel_id + thread_id + external_user_id
    -> existing workspace principal + can_approve + enabled
```

An authorized human creates/updates it using `POST /pm/bindings`, with revision
0 for creation and the current revision for updates. `pm.bind.target` must check
that the target principal exists and that human approval capability is valid.
There are no wildcard mappings. Another member of a shared channel inherits
no permissions. Revocation is checked again before an outgoing send.

- **Telegram:** mount `ChannelIngress.Telegram` on an explicitly approved
  webhook route. Configure a deployment secret (at least 32 characters) and
  the expected bot ID. Authenticate the Telegram secret header before
  extracting the sender, chat and forum-topic IDs. Tenant ID is the bot ID;
  external user ID is numeric `from.id`, never a username. Only normal text
  messages are accepted; bot/anonymous sender messages are rejected.
- **Discord:** mount `ChannelIngress.Discord` on an explicitly approved
  interactions route. Configure the application's Ed25519 public key and app
  ID. Verify the signature over timestamp + raw body, check timestamp age and
  expected application ID. Tenant ID is `<application-id>/<guild-id>` or
  `<application-id>/dm`; channel IDs include Discord thread channels. Slash
  command `pm` takes a string `text` option. The immediate ephemeral response
  is a transport acknowledgment; the actual PM response uses the durable
  outbox and the bot REST API.

Neither handler polls Telegram updates, opens a Discord gateway, registers
commands/webhooks, or changes any existing bot configuration. A deployment
owner must wire approved dedicated channels before a live canary.

Natural-language agreement remains discussion. Explicit Telegram approval:

```
/pm-approve <decision-id> <displayed-revision> <exact answer>
/pm-reject <decision-id> <displayed-revision> <exact answer>
```

Discord `pm-approve`/`pm-reject` use `decision` (string), `revision` (integer),
and `text` (string) options. Approval must come from the exact recorded origin,
a mapping with `can_approve`, and an actor currently permitted for that action
scope. Copying a decision ID into another channel does not transfer authority.

`HTTPSender` uses official Telegram/Discord REST endpoints, disables redirects,
sets request timeouts, suppresses Discord mentions and Telegram previews, and
records authoritative message IDs separately from human acknowledgment. Long
responses become atomically queued plain-text fragments in origin order. If a
fragment outcome is unknown, later fragments wait for reconciliation. No
transport token, Discord interaction token or raw provider error is stored in
a decision/receipt. Transport fixtures are not evidence of live delivery.

## Decisions and action receipts

A proposal preserves the exact instruction, work reference, source revision,
scope and origin. Discussion never creates an approval. Answer + durable action
intent commit in one SQL transaction; approval binds the exact displayed
revision and authorizing human. Dispatch revalidates permissions and current
source revision before invoking the configured action callback. The executor
must enforce that source precondition atomically when supported, or fail closed
when it cannot exclude a race. `Action.ID` is the remote idempotency key.

The executor is a trusted integration of existing permitted source tools, not a
model-generated shell command. No executor is enabled by default. Production
writes need their own authorization; tests do not supply that authorization.

- `pending_delivery`: authorized intent is durable, no send started.
- `sending`: an attempt was persisted before I/O; after a crash its outcome may
  be unknown. Repeated dispatch never sends it again.
- `delivered`: the owning source accepted the handoff with an external ID.
- `acknowledged`: a source acknowledgment exists; this is not completion.
- `source_reported`: the source claims resolution; verification remains open.
- `verified`: read-back supplied independent evidence for the actual outcome.
- `unknown` / `failed`: retain intent, attempts and limitations. Unknown remote
  sends are not automatically retried. A new attempt requires a new explicit
  decision after inspecting the source.

`ReconcileAction` performs read-only source read-back. The trusted callback may
return verified only with an external ID, evidence refs and
`independently_verified=true`. A source completion report cannot promote itself
to verified. `ReconcileDelivery` records an authorized human's inspected
transport evidence and does not silently reopen a send.

## API summary

All routes require the existing authenticated workspace principal. Lists use
`limit` (default 50, maximum 200) and opaque `cursor`; responses include `items`,
`next_cursor`, and `has_more`. Conversation history is bounded to 200 turns;
context queries accept a maximum of 50 records. Partial coverage is not health.

- `/pm/conversations`: list/create; `/{id}` detail;
  `/{id}/messages` creates a replay-safe asynchronous turn.
- `/pm/context`: scoped evidence query without source mutation.
- `/pm/decisions`: list/propose; `/{id}` read; `/{id}/answer` human decision;
  `/{id}/dispatch` explicit authorized action handoff.
- `/pm/actions`: list; `/{id}` read; `/{id}/reconcile` source read-back.
- `/pm/turns/{id}/context`, `/decisions`, `/complete`: selected PM agent tools.
- `/pm/bindings`: explicit administrative identity mapping.

Missing configuration returns HTTP 503; permission denial returns 403; changed
approval/source revisions return 409; occupied capacity returns 429. Body
identity injection and unknown fields are rejected. Request keys are required
for conversations, turns and proposals; changing input under the same key is a
conflict. Replaying an identical accepted request does not dispatch again.

## Validation and reference provenance

```sh
cd core && go test -race ./internal/pm
python3 -m unittest discover -s adapters/pm -v
```

The Go integration test uses real local Nexus SQLite primitives to round-trip a
thread, event, artifact, wake queue entry and authenticated reply. Channel tests
use signed fixtures and mocked HTTPS. Python tests use harmless local child
processes, not a live model. No live provider/channel/source verification is
claimed by these checks.

Design reuse was checked against `Git-on-my-level/codex-autorunner` `car-v3`
head `ec2c44a5bb028f02817e8e2cfbd62e845e6006a4`, fetched through read-only GitHub API:
`v3/docs/architecture/0002-grounded-decisions.md`,
`v3/src/providers/{types,factory}.ts`,
`v3/src/surfaces/web/decision_queries.ts`,
`src/codex_autorunner/adapters/discord/delivery_recovery.py`, and
`src/codex_autorunner/adapters/telegram/chat_transport.py`.
Nexus implementation reuse is its existing `router.WakePacket`,
`primitives.AgentWakeup`, bridge subprocess protocol and native session keys.

Open integration gates: core runtime/auth and source-tool composition; an
approved enforced read-only provider runtime; approved dedicated bot credentials
and channels; live end-to-end provider and transport canaries. Mocked checks do
not close these gates. This lane makes no production changes.
