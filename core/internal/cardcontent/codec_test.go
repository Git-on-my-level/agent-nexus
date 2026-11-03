package cardcontent

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func evidenceSummary(rev, size int) []byte {
	var b strings.Builder
	b.WriteString(`{"definition_of_done":null,"summary":"Release foundation.\n\n`)
	for i := 0; b.Len() < size; i++ {
		checked := 0
		if i%500 == rev%500 {
			checked = rev
		}
		fmt.Fprintf(&b, `pr-%d fix flaky test %d in module %d merged checked_rev=%d\n`, i, i, i%37, checked)
	}
	b.WriteString(`","title":"Big"}`)
	return []byte(b.String())
}

func TestSnapshotRoundTripAndCompression(t *testing.T) {
	for _, content := range [][]byte{nil, []byte(`{"title":"x"}`), evidenceSummary(1, 64<<10), evidenceSummary(1, 1<<20)} {
		codec, payload, err := EncodeSnapshot(content)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeSnapshot(codec, payload)
		if err != nil || !bytes.Equal(got, content) {
			t.Fatalf("snapshot round trip failed codec=%s err=%v", codec, err)
		}
		if len(content) > 4096 && (codec != CodecZstd || len(payload) > len(content)/3) {
			t.Fatalf("large repetitive content not compressed: codec=%s %d -> %d", codec, len(content), len(payload))
		}
	}
	codec, _, _ := EncodeSnapshot([]byte("ab"))
	if codec != CodecRaw {
		t.Fatalf("tiny content should stay raw, got %s", codec)
	}
}

func TestDeltaAgainstLargeBaseIsSmall(t *testing.T) {
	base := evidenceSummary(1, 1<<20)
	next := evidenceSummary(2, 1<<20)
	delta, err := EncodeDelta(base, next)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeDelta(base, delta)
	if err != nil || !bytes.Equal(got, next) {
		t.Fatalf("delta round trip failed: %v", err)
	}
	_, snap, _ := EncodeSnapshot(next)
	if len(delta)*20 > len(snap) {
		t.Fatalf("delta %d bytes is not much smaller than snapshot %d bytes", len(delta), len(snap))
	}
	t.Logf("content %d bytes, snapshot %d bytes, delta %d bytes", len(next), len(snap), len(delta))
}

func TestPlanChoosesBoundedRecords(t *testing.T) {
	first := evidenceSummary(1, 32<<10)
	rec, err := Plan(PlanInput{Content: first, RevisionNumber: 1})
	if err != nil || rec.Encoding != EncodingSnapshot {
		t.Fatalf("first revision must be a snapshot: %+v %v", rec.Encoding, err)
	}
	kf := &Keyframe{RevisionID: "r1", RevisionNumber: 1, Content: first, PayloadBytes: len(rec.Payload)}
	second := evidenceSummary(2, 32<<10)
	rec, err = Plan(PlanInput{Content: second, RevisionNumber: 2, Keyframe: kf})
	if err != nil || rec.Encoding != EncodingDelta || rec.BaseRevisionID != "r1" {
		t.Fatalf("near revision must be a delta on r1: %+v %v", rec.Encoding, err)
	}
	got, err := DecodeRecord(rec, first)
	if err != nil || !bytes.Equal(got, second) {
		t.Fatalf("delta decode: %v", err)
	}
	rec, err = Plan(PlanInput{Content: second, RevisionNumber: 1 + KeyframeInterval, Keyframe: kf})
	if err != nil || rec.Encoding != EncodingSnapshot {
		t.Fatalf("interval must force a snapshot: %+v %v", rec.Encoding, err)
	}
	unrelated := bytes.Repeat([]byte("zq"), 16<<10)
	rec, err = Plan(PlanInput{Content: unrelated, RevisionNumber: 3, Keyframe: kf})
	if err != nil || rec.Encoding != EncodingSnapshot {
		t.Fatalf("drifted content must start a new snapshot: %+v %v", rec.Encoding, err)
	}
	rec, err = Plan(PlanInput{Content: first, RevisionNumber: 4, DuplicateOf: "r1", Keyframe: kf})
	if err != nil || rec.Encoding != EncodingRef || rec.BaseRevisionID != "r1" || len(rec.Payload) != 0 || rec.ContentHash != Hash(first) {
		t.Fatalf("duplicate must be a payload-free ref: %+v %v", rec, err)
	}
}

func TestDecodeRejectsCorruption(t *testing.T) {
	content := evidenceSummary(1, 16<<10)
	rec, err := Plan(PlanInput{Content: content, RevisionNumber: 1})
	if err != nil {
		t.Fatal(err)
	}
	rec.ContentHash = Hash([]byte("other"))
	if _, err := DecodeRecord(rec, nil); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("hash mismatch must be ErrCorrupt, got %v", err)
	}
	rec.Codec = "lz4"
	if _, err := DecodeRecord(rec, nil); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("unknown codec must be ErrCorrupt, got %v", err)
	}
}
