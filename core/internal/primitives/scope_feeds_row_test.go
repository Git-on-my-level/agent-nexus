package primitives_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	p "agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/scopes"
)

func TestScopeInboxDetachedRowRestoresTransportColumnsAndExactNumbers(t *testing.T) {
	t.Parallel()
	i := scopes.ResourceIdentity{ScopeID: "scope", Kind: "inbox", ResourceID: "private-key", CanonicalID: "canonical-item", RID: 7, CanonicalVersion: 2}
	item := p.DerivedInboxItem{ID: i.CanonicalID, ThreadID: "thread", Category: "ask", TriggerAt: "now", GeneratedAt: "generated", SourceEventID: "event", Data: map[string]any{"id": "stale-id", "thread_id": "stale-thread", "unknown": json.Number("9007199254740993")}}
	raw, err := p.EncodeScopeInbox(i, item)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Item json.RawMessage `json:"item"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	whole, err := p.DecodeScopeInbox(i, raw)
	if err != nil {
		t.Fatal(err)
	}
	row, err := p.DecodeScopeInboxRow(envelope.Item)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(whole, row) || row.Data["id"] != i.CanonicalID || row.Data["thread_id"] != "thread" || row.Data["unknown"] != json.Number("9007199254740993") {
		t.Fatal("detached transport lost canonical columns/precision", row)
	}
	for _, bad := range [][]byte{nil, []byte("null"), append(append([]byte{}, envelope.Item...), []byte(` {}`)...), []byte(strings.Repeat("x", p.MaxScopeInboxPayloadBytes+1)), {0xff}} {
		if _, err := p.DecodeScopeInboxRow(bad); err == nil {
			t.Fatal("invalid detached row accepted")
		}
	}
}
