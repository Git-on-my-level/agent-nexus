package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Optional NDJSON stdout protocol for any direct PM runner. The adapter maps
// its native events to these safe fields; ordinary stdout is never activity.
type pmProgressKey struct{}
type pmProgress struct {
	mu              sync.Mutex
	events          []map[string]any
	sequence        int
	partial         string
	hasPartial      bool
	disabled        bool
	partialSequence int
	maxBytes        int
	line            []byte
	oversized       bool
}

func newPMProgress(turn map[string]any, maxBytes int) *pmProgress {
	p := &pmProgress{maxBytes: maxBytes}
	for _, row := range asSlice(turn["activity"]) {
		if n, _ := intFromAny(asMap(row)["sequence"]); n > p.sequence {
			p.sequence = n
		}
	}
	p.partialSequence, _ = intFromAny(turn["partial_sequence"])
	p.sequence++
	p.events = []map[string]any{{"sequence": p.sequence, "kind": "status", "label": "Preparing an answer"}}
	return p
}

func progressText(s string, max int, required bool) bool {
	return len(s) <= max && utf8.ValidString(s) && (!required || strings.TrimSpace(s) != "") && strings.IndexFunc(s, unicode.IsControl) < 0
}

func (p *pmProgress) Write(raw []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, b := range raw {
		if b == '\n' {
			if !p.oversized {
				p.accept(p.line)
			}
			p.line = p.line[:0]
			p.oversized = false
		} else if len(p.line) < 6*p.maxBytes+1024 {
			p.line = append(p.line, b)
		} else {
			p.oversized = true
		}
	}
	return len(raw), nil
}

func (p *pmProgress) accept(line []byte) {
	var row struct {
		Type   string `json:"type"`
		Kind   string `json:"kind"`
		Label  string `json:"label"`
		Target string `json:"target"`
		Text   string `json:"text"`
	}
	// Unknown keys (arguments, results, etc.) never enter the outgoing payload.
	if json.Unmarshal(line, &row) != nil {
		return
	}
	switch row.Type {
	case "pm_activity":
		if (row.Kind != "status" && row.Kind != "tool") || !progressText(row.Label, 120, true) || !progressText(row.Target, 160, false) {
			return
		}
		p.sequence++
		p.events = append(p.events, map[string]any{"sequence": p.sequence, "kind": row.Kind, "label": row.Label, "target": row.Target})
		if len(p.events) > 50 {
			p.events = p.events[len(p.events)-50:]
		}
	case "pm_partial":
		if len(row.Text) > p.maxBytes || !utf8.ValidString(row.Text) || strings.ContainsRune(row.Text, 0) {
			return
		}
		p.partial = row.Text
		p.partialSequence++
		p.hasPartial = true
	}
}

func (p *pmProgress) body(token string) map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.disabled {
		return map[string]any{"lease_token": token}
	}
	events := append([]map[string]any(nil), p.events...)
	body := map[string]any{"lease_token": token, "activity": events}
	if p.hasPartial {
		body["partial_sequence"] = p.partialSequence
		body["partial_response"] = p.partial
	}
	return body
}

func withoutPMProgress(raw []byte) []byte {
	var lines [][]byte
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		var row struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(line, &row) == nil && (row.Type == "pm_activity" || row.Type == "pm_partial") {
			continue
		}
		lines = append(lines, line)
	}
	return bytes.Join(lines, []byte{'\n'})
}

func progressFromContext(ctx context.Context) *pmProgress {
	p, _ := ctx.Value(pmProgressKey{}).(*pmProgress)
	return p
}

func (p *pmProgress) disable() { p.mu.Lock(); p.disabled = true; p.mu.Unlock() }
