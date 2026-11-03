// Package cardcontent stores card revision content (title, summary and
// definition of done) inside card_revisions as keyframe/delta records.
//
// Every revision row holds exactly one of:
//
//   - snapshot: the whole canonical content, zstd-compressed when that saves
//     space ("zstd") and stored verbatim otherwise ("raw");
//   - delta: zstd compression of the content with the card's latest snapshot
//     as a raw dictionary, so only the bytes that differ from it cost space;
//   - ref: no payload; the content is byte-identical to an earlier revision of
//     the same card (content-addressed by its SHA-256), which is never a ref.
//
// A delta's base is always a snapshot, so reading any revision decodes at most
// two records (snapshot, then one delta), whatever the history length. A new
// snapshot starts every KeyframeInterval revisions, and earlier when a delta
// would cost more than half of a fresh snapshot, which bounds both replay and
// the drift a delta can carry.
//
// The package has no workspace access policy of its own. Callers pass the
// transaction or scoped handle that already authorized the card.
package cardcontent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/klauspost/compress/zstd"
)

const (
	EncodingSnapshot = "snapshot"
	EncodingDelta    = "delta"
	EncodingRef      = "ref"

	CodecRaw  = "raw"
	CodecZstd = "zstd"

	// KeyframeInterval bounds how many revisions may follow a snapshot before
	// the next one is a snapshot again.
	KeyframeInterval = 32

	// deltaDictID marks delta frames. Snapshot frames carry no dictionary ID.
	deltaDictID = 1

	// maxDecodedBytes rejects corrupt frames before they allocate without
	// bound. It is far above any content core accepts or has accepted (the
	// JSON request body cap is 1 MiB).
	maxDecodedBytes = 64 << 20
)

var (
	// ErrCorrupt reports stored content that does not decode to its recorded
	// hash and length.
	ErrCorrupt = errors.New("card revision content is corrupt")

	snapshotEncoder *zstd.Encoder
	snapshotDecoder *zstd.Decoder
	initOnce        sync.Once
	initErr         error
)

func initCodecs() error {
	initOnce.Do(func() {
		snapshotEncoder, initErr = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault), zstd.WithEncoderConcurrency(1))
		if initErr != nil {
			return
		}
		snapshotDecoder, initErr = zstd.NewReader(nil, zstd.WithDecoderConcurrency(0), zstd.WithDecoderMaxMemory(maxDecodedBytes))
	})
	return initErr
}

// Hash is the content address of canonical content bytes.
func Hash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// EncodeSnapshot returns the codec and payload for a self-contained record.
// Compression is kept only when it saves at least an eighth of the bytes.
func EncodeSnapshot(content []byte) (string, []byte, error) {
	if err := initCodecs(); err != nil {
		return "", nil, err
	}
	compressed := snapshotEncoder.EncodeAll(content, make([]byte, 0, len(content)/4+64))
	if len(compressed) < len(content)-len(content)/8 {
		return CodecZstd, compressed, nil
	}
	return CodecRaw, append([]byte(nil), content...), nil
}

// DecodeSnapshot reverses EncodeSnapshot.
func DecodeSnapshot(codec string, payload []byte) ([]byte, error) {
	switch codec {
	case CodecRaw:
		return append([]byte(nil), payload...), nil
	case CodecZstd:
		if err := initCodecs(); err != nil {
			return nil, err
		}
		out, err := snapshotDecoder.DecodeAll(payload, nil)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%w: unknown snapshot codec %q", ErrCorrupt, codec)
	}
}

// windowFor returns a power-of-two window covering the base and the content,
// so every match against the dictionary stays reachable.
func windowFor(n int) int {
	w := zstd.MinWindowSize
	for w < n && w < zstd.MaxWindowSize {
		w <<= 1
	}
	return w
}

// EncodeDelta compresses content with base as a raw zstd dictionary.
func EncodeDelta(base, content []byte) ([]byte, error) {
	// The "better" level keeps enough match candidates to find unchanged runs
	// across a dictionary of up to ~1 MiB; the default level misses most of
	// them on large, self-similar evidence text.
	enc, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.SpeedBetterCompression),
		zstd.WithEncoderConcurrency(1),
		zstd.WithWindowSize(windowFor(len(base)+len(content))),
		zstd.WithEncoderDictRaw(deltaDictID, base),
	)
	if err != nil {
		return nil, fmt.Errorf("create delta encoder: %w", err)
	}
	defer enc.Close()
	return enc.EncodeAll(content, nil), nil
}

// DecodeDelta reverses EncodeDelta.
func DecodeDelta(base, payload []byte) ([]byte, error) {
	dec, err := zstd.NewReader(nil,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderMaxMemory(maxDecodedBytes),
		zstd.WithDecoderMaxWindow(uint64(windowFor(maxDecodedBytes))),
		zstd.WithDecoderDictRaw(deltaDictID, base),
	)
	if err != nil {
		return nil, fmt.Errorf("create delta decoder: %w", err)
	}
	defer dec.Close()
	out, err := dec.DecodeAll(payload, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	return out, nil
}

// Record is one stored revision content row.
type Record struct {
	Encoding       string
	Codec          string
	BaseRevisionID string
	ContentHash    string
	ContentBytes   int
	Payload        []byte
}

// Keyframe is the latest snapshot of a card, as the planner needs it.
type Keyframe struct {
	RevisionID     string
	RevisionNumber int
	Content        []byte
	PayloadBytes   int
}

// PlanInput describes the revision being written.
type PlanInput struct {
	Content        []byte
	RevisionNumber int
	// DuplicateOf names an earlier non-ref revision of the same card whose
	// content hash equals this content's hash, when one exists.
	DuplicateOf string
	// Keyframe is the card's latest snapshot, or nil for a card without one.
	Keyframe *Keyframe
}

// Plan chooses the cheapest bounded record for a revision and verifies that it
// decodes back to the exact input bytes before returning it.
func Plan(in PlanInput) (Record, error) {
	rec := Record{ContentHash: Hash(in.Content), ContentBytes: len(in.Content)}
	if in.DuplicateOf != "" {
		rec.Encoding = EncodingRef
		rec.BaseRevisionID = in.DuplicateOf
		return rec, nil
	}
	codec, snapshot, err := EncodeSnapshot(in.Content)
	if err != nil {
		return Record{}, err
	}
	rec.Encoding, rec.Codec, rec.Payload = EncodingSnapshot, codec, snapshot
	kf := in.Keyframe
	if kf != nil && in.RevisionNumber-kf.RevisionNumber < KeyframeInterval {
		delta, err := EncodeDelta(kf.Content, in.Content)
		if err != nil {
			return Record{}, err
		}
		if len(delta) <= len(snapshot)/2 {
			rec.Encoding, rec.Codec, rec.Payload, rec.BaseRevisionID = EncodingDelta, CodecZstd, delta, kf.RevisionID
		}
	}
	var base []byte
	if kf != nil {
		base = kf.Content
	}
	got, err := DecodeRecord(rec, base)
	if err != nil {
		return Record{}, err
	}
	if !bytes.Equal(got, in.Content) {
		return Record{}, fmt.Errorf("%w: planned record does not round-trip", ErrCorrupt)
	}
	return rec, nil
}

// DecodeRecord returns the content of a snapshot or delta record. base is the
// decoded content of a delta's snapshot and is ignored otherwise.
func DecodeRecord(rec Record, base []byte) ([]byte, error) {
	var (
		out []byte
		err error
	)
	switch rec.Encoding {
	case EncodingSnapshot:
		out, err = DecodeSnapshot(rec.Codec, rec.Payload)
	case EncodingDelta:
		if rec.Codec != CodecZstd {
			return nil, fmt.Errorf("%w: unknown delta codec %q", ErrCorrupt, rec.Codec)
		}
		out, err = DecodeDelta(base, rec.Payload)
	default:
		return nil, fmt.Errorf("%w: cannot decode %q record directly", ErrCorrupt, rec.Encoding)
	}
	if err != nil {
		return nil, err
	}
	if len(out) != rec.ContentBytes || Hash(out) != rec.ContentHash {
		return nil, fmt.Errorf("%w: content does not match its recorded hash", ErrCorrupt)
	}
	return out, nil
}
