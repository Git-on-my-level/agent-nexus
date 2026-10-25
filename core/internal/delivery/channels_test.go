package delivery_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/delivery"
)

// addAgent inserts an agent principal. metadata is merged into
// principal_kind/auth_method.
func (e *env) addAgent(t *testing.T, id, actor, kind, method string, extra map[string]any) {
	t.Helper()
	meta := map[string]any{"principal_kind": kind, "auth_method": method}
	for k, v := range extra {
		meta[k] = v
	}
	raw, _ := json.Marshal(meta)
	if _, err := e.db.Exec(`INSERT INTO agents(id, username, actor_id, created_at, updated_at, revoked_at, metadata_json) VALUES(?,?,?,?,?,NULL,?)`,
		id, "u-"+id, actor, rfc(*e.now), rfc(*e.now), string(raw)); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
}

func (e *env) declareGrant(t *testing.T, agent string, d delivery.GrantDeclaration) error {
	t.Helper()
	tx, err := e.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := delivery.UpsertGrantDeclarationTx(context.Background(), tx, agent, "https://issuer.example", d, rfc(*e.now)); err != nil {
		return err
	}
	return tx.Commit()
}

var hostedChannel = delivery.GrantDeclaration{Channel: "hosted", Addressing: delivery.AddressingPrincipal, Kinds: []string{delivery.KindPush}, Label: "Hosted"}

// principalChannelEnv has a grant-declared principal-addressed push-only
// channel, one external-grant human, one passkey human and an agent.
func principalChannelEnv(t *testing.T) *env {
	e := newEnv(t)
	e.addAgent(t, "agent_notifier", "actor_notifier", "agent", "managed_grant", nil)
	e.addAgent(t, "agent_ext", "actor_ext", "human", "external_grant", map[string]any{"external_issuer": "https://issuer.example", "external_subject": "acct_1"})
	e.addAgent(t, "agent_pk", "actor_pk", "human", "passkey", nil)
	e.addAgent(t, "agent_bot", "actor_bot", "agent", "public_key", nil)
	if err := e.declareGrant(t, "agent_notifier", hostedChannel); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestPrincipalChannelDefaultOnAndRecipient(t *testing.T) {
	e := principalChannelEnv(t)
	ctx := context.Background()
	esc, _ := newEscalator(e, nil)
	e.addAsk(t, "ask-1", baseNow.Add(-2*time.Hour), "1h", 0, "Merge?")
	if err := esc.Step(ctx, baseNow); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE channel='hosted' AND kind='push'`); n != 2 {
		t.Fatalf("want a push for each human, got %d", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE principal_id='actor_bot'`); n != 0 {
		t.Fatal("agents never receive human notifications")
	}
	claimed, err := e.store.Claim(ctx, "hosted", "actor_notifier", 10, time.Minute)
	if err != nil || len(claimed) != 2 {
		t.Fatalf("claim %v %v", claimed, err)
	}
	for _, d := range claimed {
		if d.Address != "" {
			t.Fatalf("principal-addressed delivery has address %q", d.Address)
		}
		switch d.PrincipalID {
		case "actor_ext":
			if d.Recipient == nil || d.Recipient.ExternalIssuer != "https://issuer.example" || d.Recipient.ExternalSubject != "acct_1" {
				t.Fatalf("external human recipient %+v", d.Recipient)
			}
		case "actor_pk":
			if d.Recipient != nil {
				t.Fatalf("a passkey human has no external identity, got %+v", d.Recipient)
			}
		default:
			t.Fatalf("unexpected recipient %s", d.PrincipalID)
		}
	}
	// Push-only: the daily digest never queues on this channel.
	e.addAsk(t, "ask-long", baseNow.Add(-72*time.Hour), "24h", 0, "Plan?")
	if err := esc.Step(ctx, time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE channel='hosted' AND kind='digest'`); n != 0 {
		t.Fatalf("push-only channel got %d digests", n)
	}
}

func TestPrincipalChannelExplicitOff(t *testing.T) {
	e := principalChannelEnv(t)
	ctx := context.Background()
	esc, _ := newEscalator(e, nil)
	e.setChannel(t, "actor_ext", "hosted", "", false, false)
	e.addAsk(t, "ask-1", baseNow.Add(-2*time.Hour), "1h", 0, "Merge?")
	if err := esc.Step(ctx, baseNow); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE principal_id='actor_ext'`); n != 0 {
		t.Fatal("a human who turned the channel off gets nothing")
	}
	// Turning it off after a push was queued cancels it at claim.
	e.setChannel(t, "actor_pk", "hosted", "", false, false)
	claimed, err := e.store.Claim(ctx, "hosted", "actor_notifier", 10, time.Minute)
	if err != nil || len(claimed) != 0 {
		t.Fatalf("claim %v %v", claimed, err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE principal_id='actor_pk' AND status='canceled'`); n != 1 {
		t.Fatal("queued push should be canceled")
	}
	summary, err := e.store.AttentionSummary(ctx, baseNow)
	if err != nil {
		t.Fatal(err)
	}
	if got := summary["principals"].([]map[string]any); len(got) != 0 {
		t.Fatalf("principals who turned everything off drop out of the heartbeat: %v", got)
	}
}

func TestPrincipalChannelPrivacy(t *testing.T) {
	e := principalChannelEnv(t)
	ctx := context.Background()
	esc, _ := newEscalator(e, func(p, _ string) bool { return p == "actor_pk" })
	e.addAsk(t, "ask-1", baseNow.Add(-2*time.Hour), "1h", 0, "Secret plan")
	if err := esc.Step(ctx, baseNow); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE principal_id='actor_ext'`); n != 0 {
		t.Fatal("a principal who cannot read the ask learns nothing about it")
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE principal_id='actor_pk'`); n != 1 {
		t.Fatal("the reader gets the push")
	}
}

func TestPrincipalChannelRecipientRevoked(t *testing.T) {
	e := principalChannelEnv(t)
	ctx := context.Background()
	esc, _ := newEscalator(e, nil)
	e.addAsk(t, "ask-1", baseNow.Add(-2*time.Hour), "1h", 0, "Merge?")
	if err := esc.Step(ctx, baseNow); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`UPDATE agents SET revoked_at=? WHERE id='agent_ext'`, rfc(baseNow)); err != nil {
		t.Fatal(err)
	}
	claimed, err := e.store.Claim(ctx, "hosted", "actor_notifier", 10, time.Minute)
	if err != nil || len(claimed) != 1 || claimed[0].PrincipalID != "actor_pk" {
		t.Fatalf("claim %v %v", claimed, err)
	}
}

func TestGrantDeclarationLifecycle(t *testing.T) {
	e := principalChannelEnv(t)
	ctx := context.Background()
	if got, _ := e.store.ChannelsForAgent(ctx, "agent_notifier"); len(got) != 1 || got[0] != "hosted" {
		t.Fatalf("grant agent channels %v", got)
	}
	opts, err := e.store.ChannelOptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if o := opts["hosted"]; o.Addressing != delivery.AddressingPrincipal || strings.Join(o.Kinds, ",") != "push" || o.Label != "Hosted" {
		t.Fatalf("options %+v", o)
	}
	// Another agent may not redeclare the channel with different options.
	e.addAgent(t, "agent_other", "actor_other", "agent", "managed_grant", nil)
	if err := e.declareGrant(t, "agent_other", delivery.GrantDeclaration{Channel: "hosted"}); !errors.Is(err, delivery.ErrDeclarationConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	if err := e.declareGrant(t, "agent_notifier", delivery.GrantDeclaration{Channel: "Bad"}); !errors.Is(err, delivery.ErrInvalid) {
		t.Fatalf("want invalid, got %v", err)
	}
	// An empty channel revokes; a later declaration restores.
	if err := e.declareGrant(t, "agent_notifier", delivery.GrantDeclaration{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.store.DeclaredChannels(ctx); len(got) != 0 {
		t.Fatalf("revoked declaration still declared: %v", got)
	}
	if err := e.declareGrant(t, "agent_notifier", hostedChannel); err != nil {
		t.Fatal(err)
	}
	// Revoking the agent removes the channel.
	if _, err := e.db.Exec(`UPDATE agents SET revoked_at=? WHERE id='agent_notifier'`, rfc(baseNow)); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.store.ChannelsForAgent(ctx, "agent_notifier"); len(got) != 0 {
		t.Fatalf("revoked agent still delivers %v", got)
	}
	if got, _ := e.store.DeclaredChannels(ctx); len(got) != 0 {
		t.Fatalf("revoked agent's channel still declared: %v", got)
	}
}

func TestReceiptSkipped(t *testing.T) {
	e := principalChannelEnv(t)
	ctx := context.Background()
	esc, _ := newEscalator(e, nil)
	e.addAsk(t, "ask-1", baseNow.Add(-2*time.Hour), "1h", 0, "Merge?")
	if err := esc.Step(ctx, baseNow); err != nil {
		t.Fatal(err)
	}
	claimed, err := e.store.Claim(ctx, "hosted", "actor_notifier", 1, time.Minute)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim %v %v", claimed, err)
	}
	d := claimed[0]
	got, err := e.store.RecordReceipt(ctx, d.DeliveryID, d.ClaimToken, "hosted", delivery.ReceiptSkipped, "muted by recipient", false, "actor_notifier")
	if err != nil || got.Status != delivery.StatusCanceled || got.LastError != "" || got.FailedAt != "" {
		t.Fatalf("skip %+v %v", got, err)
	}
	var reason string
	if err = e.db.QueryRow(`SELECT last_error FROM deliveries WHERE delivery_id=?`, d.DeliveryID).Scan(&reason); err != nil || reason != "muted by recipient" {
		t.Fatalf("stored skip reason: %q %v", reason, err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM delivery_receipts WHERE delivery_id=?`, d.DeliveryID); n != 0 {
		t.Fatalf("a skip is not a failure receipt, got %d receipts", n)
	}
	if again, err := e.store.RecordReceipt(ctx, d.DeliveryID, d.ClaimToken, "hosted", delivery.ReceiptSkipped, "", false, "actor_notifier"); err != nil || again.Status != delivery.StatusCanceled || again.ClaimToken != "" {
		t.Fatalf("retried skip %+v %v", again, err)
	}
	if _, err := e.store.RecordReceipt(ctx, d.DeliveryID, d.ClaimToken, "hosted", delivery.ReceiptDelivered, "", false, "actor_notifier"); !errors.Is(err, delivery.ErrConflict) {
		t.Fatalf("a delivered receipt after a skip must not read as success: %v", err)
	}
	if _, err := e.store.RecordReceipt(ctx, d.DeliveryID, d.ClaimToken, "hosted", "bogus", "", false, "w"); !errors.Is(err, delivery.ErrInvalid) {
		t.Fatalf("unknown outcome %v", err)
	}
}

func TestAttentionSummaryIdentityAndPending(t *testing.T) {
	e := principalChannelEnv(t)
	ctx := context.Background()
	esc, _ := newEscalator(e, nil)
	e.store.CanRead = func(context.Context, string, string) bool { return true }
	e.addAsk(t, "ask-1", baseNow.Add(-2*time.Hour), "1h", 0, "Merge?")
	if err := esc.Step(ctx, baseNow); err != nil {
		t.Fatal(err)
	}
	summary, err := e.store.AttentionSummary(ctx, baseNow)
	if err != nil {
		t.Fatal(err)
	}
	principals := summary["principals"].([]map[string]any)
	if len(principals) != 2 {
		t.Fatalf("principals %v", principals)
	}
	for _, p := range principals {
		switch p["principal_id"] {
		case "actor_ext":
			if p["external_issuer"] != "https://issuer.example" || p["external_subject"] != "acct_1" || p["overdue"] != 1 {
				t.Fatalf("external human %v", p)
			}
		case "actor_pk":
			if _, ok := p["external_subject"]; ok {
				t.Fatalf("passkey human must not carry an external identity: %v", p)
			}
		default:
			t.Fatalf("unexpected principal %v", p)
		}
		for key := range p {
			if key == "title" || key == "ref" {
				t.Fatalf("heartbeat carries counts only: %v", p)
			}
		}
	}
	if pending := summary["pending_deliveries"].(map[string]int); pending["hosted"] != 2 {
		t.Fatalf("pending %v", pending)
	}
	// A delivery waiting out its retry backoff is not claimable yet.
	claimed, err := e.store.Claim(ctx, "hosted", "actor_notifier", 1, time.Minute)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim %v %v", claimed, err)
	}
	if _, err := e.store.RecordReceipt(ctx, claimed[0].DeliveryID, claimed[0].ClaimToken, "hosted", delivery.ReceiptFailed, "provider down", true, "actor_notifier"); err != nil {
		t.Fatal(err)
	}
	summary, _ = e.store.AttentionSummary(ctx, baseNow)
	if pending := summary["pending_deliveries"].(map[string]int); pending["hosted"] != 1 {
		t.Fatalf("backoff rows are not pending: %v", pending)
	}
}

func TestSettingsChannelUnaffectedByPrincipalChannel(t *testing.T) {
	e := principalChannelEnv(t)
	ctx := context.Background()
	esc, _ := newEscalator(e, nil)
	// A settings-addressed row on the principal channel is ignored; a row on
	// an undeclared settings channel keeps today's behavior.
	e.setChannel(t, "actor_bot", "hosted", "x", true, true)
	e.setChannel(t, "alice", "email", "a@example.com", true, true)
	e.addAsk(t, "ask-1", baseNow.Add(-2*time.Hour), "1h", 0, "Merge?")
	if err := esc.Step(ctx, baseNow); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE principal_id='actor_bot'`); n != 0 {
		t.Fatal("an address on a principal channel does not make an agent a recipient")
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE principal_id='alice' AND channel='email' AND kind='push'`); n != 1 {
		t.Fatal("settings channels still deliver")
	}
}

func TestPrincipalRecipientsQueryPlan(t *testing.T) {
	e := newEnv(t)
	rows, err := e.db.Query(`EXPLAIN QUERY PLAN SELECT DISTINCT a.actor_id FROM agents a INDEXED BY idx_agents_active_humans
		LEFT JOIN notification_channel_settings s ON s.principal_id=a.actor_id AND s.channel=?
		WHERE a.revoked_at IS NULL AND json_extract(a.metadata_json,'$.principal_kind')='human' AND (s.principal_id IS NULL OR s.push_enabled=1)
		ORDER BY a.actor_id LIMIT ?`, "hosted", 200)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	joined := strings.Join(plan, "\n")
	if !strings.Contains(joined, "idx_agents_active_humans") || !strings.Contains(joined, "sqlite_autoindex_notification_channel_settings_1") {
		t.Fatalf("plan:\n%s", joined)
	}
}

func TestClaimRechecksReadAccess(t *testing.T) {
	e := principalChannelEnv(t)
	ctx := context.Background()
	esc, _ := newEscalator(e, nil)
	e.addAsk(t, "ask-1", baseNow.Add(-2*time.Hour), "1h", 0, "Secret plan")
	if err := esc.Step(ctx, baseNow); err != nil {
		t.Fatal(err)
	}
	// actor_ext loses access after the push was queued.
	e.store.CanRead = func(_ context.Context, principal, _ string) bool { return principal != "actor_ext" }
	claimed, err := e.store.Claim(ctx, "hosted", "actor_notifier", 10, time.Minute)
	if err != nil || len(claimed) != 1 || claimed[0].PrincipalID != "actor_pk" {
		t.Fatalf("claim %v %v", claimed, err)
	}
	var status, payload string
	if err := e.db.QueryRow(`SELECT status, payload_json FROM deliveries WHERE principal_id='actor_ext'`).Scan(&status, &payload); err != nil {
		t.Fatal(err)
	}
	if status != "canceled" || strings.Contains(payload, "Secret") {
		t.Fatalf("a recipient who lost access gets nothing and nothing is kept: %s %s", status, payload)
	}
}

func TestGrantManageURL(t *testing.T) {
	e := principalChannelEnv(t)
	ctx := context.Background()
	for _, bad := range []string{"http://example.com/x", "javascript:alert(1)", "/relative", "https://user:pw@example.com/", "https://" + strings.Repeat("a", 600) + ".example"} {
		d := hostedChannel
		d.ManageURL = bad
		if err := delivery.ValidateGrantDeclaration(d); !errors.Is(err, delivery.ErrInvalid) {
			t.Errorf("%q should be refused: %v", bad, err)
		}
	}
	d := hostedChannel
	d.ManageURL = "https://app.example.com/account/notifications"
	if err := e.declareGrant(t, "agent_notifier", d); err != nil {
		t.Fatal(err)
	}
	opts, _ := e.store.ChannelOptions(ctx)
	if opts["hosted"].ManageURL != "https://app.example.com/account/notifications" {
		t.Fatalf("manage_url %+v", opts["hosted"])
	}
}

func TestHeartbeatListsEachPrincipalsChannels(t *testing.T) {
	e := principalChannelEnv(t)
	ctx := context.Background()
	e.addAgent(t, "agent_mail", "actor_mail", "agent", "public_key", nil)
	// actor_ext turns the principal channel off but keeps another channel on.
	e.setChannel(t, "actor_ext", "hosted", "", false, false)
	e.setChannel(t, "actor_ext", "email", "me@example.com", true, true)
	sum, err := e.store.AttentionSummary(ctx, baseNow)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, p := range sum["principals"].([]map[string]any) {
		got[p["principal_id"].(string)] = strings.Join(p["channels"].([]string), ",")
	}
	if got["actor_ext"] != "email" || got["actor_pk"] != "hosted" {
		t.Fatalf("channels per principal %v", got)
	}
}
