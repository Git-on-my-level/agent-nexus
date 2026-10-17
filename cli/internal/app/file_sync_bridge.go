package app

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	hostbridge "agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/filetransport"
	"agent-nexus-cli/internal/integrations"
)

// Every operation uses the frozen enrollment. No producer-controlled identity,
// URL, endpoint or config path crosses this boundary.
func (a *App) fileAuth(ctx context.Context, w hostbridge.Workspace, agent hostbridge.Agent, b filetransport.Binding) (config.Resolved, error) {
	if b.Workspace != w.ID || b.BaseURL != w.BaseURL || b.Host != w.HostID || b.Actor != agent.ActorID || b.Agent != agent.Name {
		return config.Resolved{}, integrations.ErrIdentityChanged
	}
	cfg, e := a.validateBridgeAgent(ctx, w, agent)
	if e != nil {
		if fileErrorClass(e) == "denied" {
			return cfg, integrations.ErrIdentityChanged
		}
		return cfg, e
	}
	// Always contact core: cached tokens and local enrollment alone do not prove
	// current access. All subsequent reads/writes remain ordinarily authorized.
	r, e := a.invokeRawJSON(ctx, cfg, "sync identity", "GET", "/meta/handshake", nil)
	if e != nil {
		if fileErrorClass(e) == "denied" {
			return cfg, integrations.ErrIdentityChanged
		}
		return cfg, e
	}
	if anyString(commandResultBody(r)["workspace_id"]) != b.Workspace {
		return cfg, integrations.ErrIdentityChanged
	}
	r, e = a.invokeRawJSON(ctx, cfg, "sync identity", "GET", "/agents/me", nil)
	if e != nil {
		if fileErrorClass(e) == "denied" {
			return cfg, integrations.ErrIdentityChanged
		}
		return cfg, e
	}
	if anyString(asMap(commandResultBody(r)["agent"])["actor_id"]) != b.Actor {
		return cfg, integrations.ErrIdentityChanged
	}
	return cfg, nil
}
func (a *App) fileCallbacks(w hostbridge.Workspace, agent hostbridge.Agent) filetransport.Callbacks {
	return filetransport.Callbacks{
		Classify: fileErrorClass, Code: func(e error) string {
			if e == nil {
				return ""
			}
			if fileErrorClass(e) == "identity" {
				return "identity_changed"
			}
			return errnorm.Normalize(e).Code
		},
		Validate: func(ctx context.Context, b filetransport.Binding) error {
			_, e := a.fileAuth(ctx, w, agent, b)
			return e
		},
		Prepare: func(ctx context.Context, b filetransport.Binding, in filetransport.Entry, received time.Time) (integrations.Delivery, []string, error) {
			cfg, e := a.fileAuth(ctx, w, agent, b)
			if e != nil {
				return integrations.Delivery{}, nil, e
			}
			allowed, ok := b.Card(in.Card)
			if !ok {
				return integrations.Delivery{}, nil, errnorm.Usage("card_not_allowlisted", "card is outside this directory's allowlist")
			}
			card, e := a.fileCard(ctx, cfg, allowed.ID)
			if e != nil {
				return integrations.Delivery{}, nil, e
			}
			if anyString(card["id"]) != allowed.ID {
				return integrations.Delivery{}, nil, integrations.ErrIdentityChanged
			}
			target, e := cardMessageTarget(card, allowed.ID)
			if e != nil {
				return integrations.Delivery{}, nil, e
			}
			body, e := buildMessagePostedBody(cfg, "", target, in.Text, "", nil, "")
			if e != nil {
				return integrations.Delivery{}, nil, e
			}
			warnings := []string{}
			payload := asMap(asMap(body["event"])["payload"])
			for _, ref := range in.Evidence {
				if e = validateEvidenceRef(ref); e != nil {
					return integrations.Delivery{}, nil, errnorm.Usage("invalid_evidence", e.Error())
				}
			}
			if len(in.Evidence) > 0 {
				payload["evidence_refs"] = in.Evidence
			}
			if in.Next != nil {
				at, ok := parsePromiseWhen(in.Next.At, received)
				if !ok {
					return integrations.Delivery{}, nil, errnorm.Usage("invalid_next", "next.at requires a UTC date, RFC3339 timestamp or positive duration")
				}
				if at.Sub(a.clockNow()) > promiseHorizon {
					warnings = append(warnings, "note_beyond_horizon")
				}
				if len(in.Evidence) == 0 {
					if prev, ok := a.previousOwnNext(ctx, cfg, target.ThreadID); ok && !prev.Equal(at) {
						warnings = append(warnings, "note_moved_without_evidence")
					}
				}
				payload["next"] = map[string]any{"at": at.Format(time.RFC3339Nano), "what": truncateRunes(in.Next.What, maxPromiseWhatRunes)}
			}
			key := "anx-file-v1-" + b.ID + "-" + in.ID
			body["request_key"] = key
			if e = validateEventsCreateInput(body, "sync files"); e != nil {
				return integrations.Delivery{}, nil, e
			}
			return integrations.Delivery{Key: key, PrincipalID: b.Actor, HostID: b.Host, BaseURL: b.BaseURL, Agent: b.Agent, CommandID: "events.create", Body: body}, warnings, nil
		},
		Send: func(ctx context.Context, b filetransport.Binding, d integrations.Delivery) (string, error) {
			cfg, e := a.fileAuth(ctx, w, agent, b)
			if e != nil {
				return "", e
			}
			cfg.HostID = ""
			r, e := a.invokeTypedJSON(ctx, cfg, "sync files", "events.create", nil, nil, d.Body)
			if e != nil {
				return "", e
			}
			id := anyString(asMap(commandResultBody(r)["event"])["id"])
			if id == "" {
				return "", errnorm.Network("invalid_receipt", "core returned no event receipt; retain and replay the same request")
			}
			return "event:" + strings.TrimPrefix(id, "event:"), nil
		},
		Refresh: func(ctx context.Context, b filetransport.Binding, c filetransport.Card) (filetransport.Snapshot, error) {
			cfg, e := a.fileAuth(ctx, w, agent, b)
			if e != nil {
				return filetransport.Snapshot{}, e
			}
			card, e := a.fileCard(ctx, cfg, c.ID)
			if e != nil {
				return filetransport.Snapshot{}, e
			}
			if anyString(card["id"]) != c.ID {
				return filetransport.Snapshot{}, integrations.ErrIdentityChanged
			}
			path := "/cards/" + url.PathEscape(c.ID)
			r, e := a.invokeRawJSON(ctx, cfg, "sync mirror", "GET", path+"/plan", nil)
			if e != nil {
				return filetransport.Snapshot{}, e
			}
			plan := commandResultBody(r)
			r, e = a.invokeRawJSON(ctx, cfg, "sync mirror", "GET", "/events?thread_id="+url.QueryEscape(anyString(card["thread_id"]))+"&type=message_posted&limit=10", nil)
			if e != nil {
				return filetransport.Snapshot{}, e
			}
			// Whitelist bounded plain text only. Never copy arbitrary API JSON, tokens,
			// observations, transcripts, commands or workspace-wide enumerations.
			var out strings.Builder
			fmt.Fprintf(&out, "Title: %s\nStatus: %s\n\n%s\n\n", mirrorText(card["title"], 256), mirrorText(card["column_key"], 80), mirrorText(card["summary"], 2048))
			promise := asMap(asMap(plan["plan_health"])["promise"])
			fmt.Fprintf(&out, "Next: %s — %s\n\nPlan:\n", mirrorText(promise["at"], 80), mirrorText(promise["what"], 160))
			effective := map[string]string{}
			for i, v := range asSlice(asMap(plan["plan_state"])["steps"]) {
				if i >= 200 {
					break
				}
				step := asMap(v)
				effective[anyString(step["id"])] = mirrorText(step["status"], 40)
			}
			for i, v := range asSlice(asMap(plan["plan"])["steps"]) {
				if i >= 20 {
					out.WriteString("- (plan truncated)\n")
					break
				}
				step := asMap(v)
				fmt.Fprintf(&out, "- %s [%s]\n", mirrorText(step["title"], 256), firstNonEmpty(effective[anyString(step["id"])], mirrorText(step["status"], 40)))
			}
			out.WriteString("\nRecent notes (at most 10):\n")
			for i, v := range asSlice(commandResultBody(r)["events"]) {
				if i >= 10 {
					break
				}
				ev := asMap(v)
				fmt.Fprintf(&out, "- %s %s: %s\n", mirrorText(ev["ts"], 40), mirrorText(ev["actor_id"], 128), mirrorText(asMap(ev["payload"])["text"], 1024))
			}
			return filetransport.Snapshot{Card: c, Version: mirrorText(firstNonEmpty(anyString(plan["if_updated_at"]), anyString(card["updated_at"])), 128), Body: truncateHook(out.String(), 60<<10)}, nil
		},
	}
}
func mirrorText(v any, limit int) string {
	return truncateRunes(strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' || r == 127 {
			return -1
		}
		return r
	}, anyString(v)), limit)
}
func (a *App) bridgeFilesTick(ctx context.Context, w hostbridge.Workspace, agent hostbridge.Agent, dir string) error {
	s, e := filetransport.Load(dir)
	if e != nil {
		return e
	}
	if !s.Enabled {
		return errnorm.Local("file_transport_disabled", "directory binding is disabled")
	}
	_, e = filetransport.Tick(ctx, dir, a.clockNow(), a.fileCallbacks(w, agent))
	if fileErrorClass(e) == "identity" { // A revoked enrollment covers every directory, not just today's cohort.
		for _, other := range agent.FileDirs {
			if other != dir {
				if ie := filetransport.Invalidate(ctx, other, "identity_changed", a.clockNow()); ie != nil {
					return ie
				}
			}
		}
	}
	return e
}
