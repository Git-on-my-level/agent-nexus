package primitives

import (
	"context"
	"errors"
	"testing"
)

func TestAskTitleKeyIgnoresCasePunctuationAndInvisibleCharacters(t *testing.T) {
	t.Parallel()
	want := AskTitleKey("Restore the loop-audit card, which keeps failing?")
	for _, title := range []string{"restore the loop audit card which keeps failing", "  Restore the  loop​-audit card,\twhich keeps failing?! ", "RESTORE THE LOOP-AUDIT CARD, WHICH KEEPS FAILING"} {
		if got := AskTitleKey(title); got != want {
			t.Fatalf("AskTitleKey(%q)=%q, want %q", title, got, want)
		}
	}
	if AskTitleKey("Digest 2026-11-12") == AskTitleKey("Digest 2026-11-13") {
		t.Fatal("different dates must stay different questions")
	}
}

// A requester's repeat of an open question on the same card returns the open
// ask; another card, another requester, another kind, --again or a closed ask
// files normally.
func TestRepeatedOpenAskReturnsTheOpenAsk(t *testing.T) {
	t.Parallel()
	s, _, card, first := askDeliveryFixture(t)
	ctx := context.Background()
	other, err := s.CreateWork(ctx, "requester", "", map[string]any{"title": "Another subject", "phase": "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	otherCard, err := s.GetBoardCard(ctx, "", anyStringValue(other["id"]))
	if err != nil {
		t.Fatal(err)
	}
	file := func(actor string, subject map[string]any, extra map[string]any) (map[string]any, error) {
		payload := map[string]any{"subject_ref": subject["ref"], "requester_actor_id": actor, "kind": "ask", "title": "  proceed ", "response_proposals": []string{"Proceed"}}
		for k, v := range extra {
			payload[k] = v
		}
		return s.AppendTaskAttentionEvent(ctx, actor, map[string]any{"type": "human_attention_requested", "thread_id": subject["thread_id"], "refs": []string{anyStringValue(subject["ref"])}, "payload": payload})
	}
	var duplicate *DuplicateAskError
	for i := 0; i < 2; i++ {
		if _, err = file("requester", card, nil); !errors.As(err, &duplicate) || duplicate.AskID != anyStringValue(first["id"]) {
			t.Fatalf("repeat %d: err=%v", i, err)
		}
	}
	var asks int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM brief_asks`).Scan(&asks); err != nil || asks != 1 {
		t.Fatalf("asks=%d err=%v, want the one open ask", asks, err)
	}
	// The same words about another card are another question.
	if _, err = file("requester", otherCard, nil); err != nil {
		t.Fatalf("the same title on another card should file: %v", err)
	}
	for name, extra := range map[string]map[string]any{"again": {"again": true}, "review": {"kind": "review"}, "other recipient": {"recipient_actor_id": "someone"}} {
		if _, err = file("requester", card, extra); err != nil {
			t.Fatalf("%s should file: %v", name, err)
		}
	}
	if _, err = file("someone-else", card, nil); err != nil {
		t.Fatalf("another requester should file: %v", err)
	}
	// Once every open copy is answered the question may be asked again.
	rows, err := s.db.QueryContext(ctx, `SELECT ask_id FROM brief_asks WHERE requester='requester' AND kind='ask' AND recipient='' AND state='open' AND subject_ref=?`, anyStringValue(asMapValue(first["payload"])["subject_ref"]))
	if err != nil {
		t.Fatal(err)
	}
	var open []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		open = append(open, id)
	}
	rows.Close()
	for _, id := range open {
		if _, err = s.AppendHumanAttentionWithdrawal(ctx, "requester", id, map[string]any{"provenance": map[string]any{"sources": []string{"actor_statement:test"}}, "payload": map[string]any{"reason": "answered elsewhere"}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = file("requester", card, nil); err != nil {
		t.Fatalf("a closed question may be asked again: %v", err)
	}
}
