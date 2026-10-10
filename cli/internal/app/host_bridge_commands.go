package app

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/hostidentity"
)

func init() {
	localHelperTopics = append(localHelperTopics,
		localHelperTopic{Path: "host token", Summary: "Print a short-lived derived-agent bearer from the enrolled host.", JSONShape: "`token`, `expires_at`, `agent: {id, handle}", Composition: "Host assertion grant; text mode prints only the token.", Examples: []string{"anx --json host token"}},
		localHelperTopic{Path: "host bridge check-in", Summary: "Publish an enrolled host bridge check-in.", JSONShape: "Core bridge check-in result", Composition: "Signs the exact request body with the owner-only host key.", Examples: []string{"anx host bridge check-in --host-id <id> --instance-id <id> --ttl-seconds 180"}},
		localHelperTopic{Path: "host bridge wake claim", Summary: "Claim a durable wake for this host.", JSONShape: "Core wake mutation result", Composition: "Uses a host-signed proof; complete or fail after handling.", Examples: []string{"anx host bridge wake claim --host-id <id> --wakeup-id <id> --instance-id <id>"}},
		localHelperTopic{Path: "host bridge wake complete", Summary: "Complete a claimed wake for this host.", JSONShape: "Core wake mutation result", Composition: "Uses a host-signed proof.", Examples: []string{"anx host bridge wake complete --host-id <id> --wakeup-id <id> --instance-id <id>"}},
		localHelperTopic{Path: "host bridge wake fail", Summary: "Record a failed claimed wake for this host.", JSONShape: "Core wake mutation result", Composition: "Uses a host-signed proof and an error summary.", Examples: []string{"anx host bridge wake fail --host-id <id> --wakeup-id <id> --instance-id <id> --error <text>"}},
		localHelperTopic{Path: "runs ingest", Summary: "Ingest an agentctl callback or execution envelope.", JSONShape: "Idempotent run upsert result", Composition: "agentctl command appends an owner-only JSON event path. Its child has only PATH and LANG; pass --config-dir and --base-url explicitly. Failures are logged without secrets under <config-dir>/logs/runs-ingest.log.", Examples: []string{"anx --config-dir /absolute/anx --base-url https://anx.example.com runs ingest /absolute/event.json"}},
	)
}

// Bridge mutations use a host proof over the exact JSON bytes sent to core.
func (a *App) hostBridgeCall(ctx context.Context, cfg config.Resolved, hostID, kind, path string, body any) (*commandResult, error) {
	host, err := a.resolvedHost(cfg)
	if err != nil {
		return nil, err
	}
	if host.ID != hostID {
		return nil, errnorm.Usage("host_id_mismatch", "--host-id does not match the enrolled host")
	}
	if cfg.Sources["base_url"] == "default" {
		cfg.BaseURL = host.BaseURL
	}
	key, err := hostidentity.Key(host)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	signed := time.Now().UTC().Format(time.RFC3339Nano)
	digest := sha256.Sum256(raw)
	message := "anx-host-" + kind + "|" + hostID + "|" + signed + "|" + base64.RawURLEncoding.EncodeToString(digest[:])
	headers := map[string]string{
		"X-ANX-Host-Key-Id":    host.KeyID,
		"X-ANX-Host-Signed-At": signed,
		"X-ANX-Host-Signature": base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte(message))),
	}
	if kind != "bridge-check-in" {
		headers["X-ANX-Host-Id"] = hostID
	}
	result, err := a.hostCall(ctx, cfg, http.MethodPost, path, body, headers)
	if err != nil {
		return nil, err
	}
	return &commandResult{Data: result}, nil
}

func (a *App) runHostBridge(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "host bridge", errnorm.Usage("subcommand_required", "use anx host bridge check-in|wake")
	}
	if args[0] == "check-in" {
		fs := newSilentFlagSet("host bridge check-in")
		var hostID, instanceID trackedString
		var ttl int
		fs.Var(&hostID, "host-id", "Enrolled host ID")
		fs.Var(&instanceID, "instance-id", "Bridge instance ID")
		fs.IntVar(&ttl, "ttl-seconds", 0, "Check-in lifetime")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "host bridge check-in", errnorm.Usage("invalid_flags", err.Error())
		}
		if len(fs.Args()) != 0 || hostID.value == "" || instanceID.value == "" || ttl < 1 || ttl > 3600 {
			return nil, "host bridge check-in", errnorm.Usage("invalid_args", "--host-id, --instance-id and --ttl-seconds (1..3600) are required")
		}
		now := time.Now().UTC()
		body := map[string]any{"bridge_instance_id": instanceID.value, "checked_in_at": now.Format(time.RFC3339Nano), "expires_at": now.Add(time.Duration(ttl) * time.Second).Format(time.RFC3339Nano)}
		r, err := a.hostBridgeCall(ctx, cfg, hostID.value, "bridge-check-in", "/hosts/"+url.PathEscape(hostID.value)+"/bridge/check-in", body)
		return r, "host bridge check-in", err
	}
	if args[0] == "wake" && len(args) >= 2 {
		action := args[1]
		if action != "claim" && action != "complete" && action != "fail" {
			return nil, "host bridge wake", errnorm.Usage("invalid_args", "use claim|complete|fail")
		}
		fs := newSilentFlagSet("host bridge wake " + action)
		var hostID, wakeupID, instanceID, failure trackedString
		fs.Var(&hostID, "host-id", "Enrolled host ID")
		fs.Var(&wakeupID, "wakeup-id", "Wakeup ID")
		fs.Var(&instanceID, "instance-id", "Bridge instance ID")
		fs.Var(&failure, "error", "Failure detail")
		if err := fs.Parse(args[2:]); err != nil {
			return nil, "host bridge wake " + action, errnorm.Usage("invalid_flags", err.Error())
		}
		if len(fs.Args()) != 0 || hostID.value == "" || wakeupID.value == "" || instanceID.value == "" || action != "fail" && failure.set {
			return nil, "host bridge wake " + action, errnorm.Usage("invalid_args", "--host-id, --wakeup-id and --instance-id are required")
		}
		body := map[string]any{"wakeup_id": wakeupID.value, "bridge_instance_id": instanceID.value}
		if failure.set {
			body["error"] = failure.value
		}
		r, err := a.hostBridgeCall(ctx, cfg, hostID.value, "wakeup-"+action, "/agent-wakeups/"+action, body)
		return r, "host bridge wake " + action, err
	}
	return nil, "host bridge", errnorm.Usage("unknown_subcommand", "use anx host bridge check-in|wake claim|complete|fail")
}
