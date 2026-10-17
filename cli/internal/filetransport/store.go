package filetransport

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"agent-nexus-cli/internal/integrations"
)

func StateDir(configDir, directory string) string {
	return filepath.Join(configDir, "file-transports", Digest([]byte(directory))[:32])
}
func Load(dir string) (State, error) {
	var s State
	raw, e := integrations.PrivateRead(filepath.Join(dir, "state.json"))
	if e != nil {
		return s, e
	}
	if e = json.Unmarshal(raw, &s); e != nil {
		return s, e
	}
	if s.Version != 1 || len(s.Pending) > MaxPending || s.Records > MaxRecords {
		return s, ErrUnsafe
	}
	return s, s.Binding.Validate()
}
func Save(dir string, s State) error {
	return integrations.PrivateWrite(filepath.Join(dir, "state.json"), integrations.JSON(s))
}
func readRecord(dir, id string) (Record, error) {
	var r Record
	if !ValidUUID(id) {
		return r, ErrUnsafe
	}
	raw, e := integrations.PrivateRead(filepath.Join(dir, "entries", id+".json"))
	if e == nil {
		e = json.Unmarshal(raw, &r)
	}
	if e == nil && (r.ID != id || r.Hash != Digest(r.Raw)) {
		e = ErrUnsafe
	}
	return r, e
}
func saveRecord(dir string, r Record) error {
	if !ValidUUID(r.ID) {
		return ErrUnsafe
	}
	return integrations.PrivateWrite(filepath.Join(dir, "entries", r.ID+".json"), integrations.JSON(r))
}
func setup(directory string, b Binding) (*FS, error) {
	// Only initial enable creates .anx. Existing directories need our matching marker.
	path := filepath.Join(directory, ".anx")
	created := false
	if e := os.Mkdir(path, 0700); e == nil {
		created = true
	} else if !os.IsExist(e) {
		return nil, e
	}
	f, e := Open(path)
	if e != nil {
		return nil, e
	}
	// A retry may remove only its own interrupted marker stage. Never replace
	// another enrollment's marker. This also repairs a crash after link-before-unlink.
	if e = f.remove(".identity-" + b.ID); e != nil {
		f.Close()
		return nil, e
	}
	if created {
		e = f.installMarker(b.ID, integrations.JSON(map[string]any{"schema_version": 1, "directory_uuid": b.ID}))
	} else {
		e = checkMarker(f, b.ID)
		if os.IsNotExist(e) { // Retry a crash before the first marker publication.
			names, err := f.names("", 1)
			if err == nil && len(names) == 0 {
				e = f.installMarker(b.ID, integrations.JSON(map[string]any{"schema_version": 1, "directory_uuid": b.ID}))
			}
		}
	}
	if e != nil {
		f.Close()
		return nil, e
	}
	for _, d := range []string{"outbox", "outbox/.done", "outbox/.failed", "intake", "mirror", "mirror/generations"} {
		if e = f.mkdir(d); e != nil {
			f.Close()
			return nil, e
		}
	}
	return f, nil
}
func checkMarker(f *FS, id string) error {
	raw, e := f.read("IDENTITY.json", 1024)
	if e != nil {
		return e
	}
	var m struct {
		ID string `json:"directory_uuid"`
	}
	if json.Unmarshal(raw, &m) != nil || m.ID != id {
		return integrations.ErrIdentityChanged
	}
	return nil
}
func openBinding(b Binding) (*FS, error) {
	real, e := filepath.EvalSymlinks(b.Directory)
	if e != nil {
		return nil, e
	}
	if real != b.Directory {
		return nil, ErrUnsafe
	}
	f, e := Open(filepath.Join(b.Directory, ".anx"))
	if e != nil {
		return nil, e
	}
	if e = checkMarker(f, b.ID); e != nil {
		f.Close()
		return nil, e
	}
	return f, nil
}
func Enable(ctx context.Context, dir string, b Binding, now time.Time) (State, error) {
	if e := b.Validate(); e != nil {
		return State{}, e
	}
	unlock, e := integrations.Lock(ctx, dir+".lock")
	if e != nil {
		return State{}, e
	}
	defer unlock()
	if old, e := Load(dir); e == nil {
		if old.Binding.ID != b.ID || old.Binding.BaseURL != b.BaseURL || old.Binding.Actor != b.Actor || old.Binding.Host != b.Host {
			return old, integrations.ErrIdentityChanged
		}
		if len(old.Pending) > 0 && !reflect.DeepEqual(old.Binding, b) {
			return old, fmt.Errorf("pending_entries_require_reconciliation")
		}
	} else if !os.IsNotExist(e) {
		return State{}, e
	}
	if _, err := Load(dir); os.IsNotExist(err) { // Journal UUID before touching the directory.
		if e = Save(dir, State{Version: 1, Binding: b, Pending: []string{}}); e != nil {
			return State{}, e
		}
	}
	f, e := setup(b.Directory, b)
	if e != nil {
		return State{}, e
	}
	defer f.Close()
	s := State{Version: 1, Binding: b, Enabled: true, Pending: []string{}}
	// Re-enabling a disabled binding preserves its UUID, ledger and intake order.
	if old, e := Load(dir); e == nil {
		s = old
		s.Enabled = true
		s.Binding = b
		s.LastCode = ""
	}
	if e = os.RemoveAll(filepath.Join(dir, "cache")); e != nil {
		return s, e
	}
	if e = purgeMirror(f); e != nil {
		return s, e
	}
	if e = Save(dir, s); e != nil {
		return s, e
	}
	e = publish(f, s, nil, now, false)
	return s, e
}
func Disable(ctx context.Context, dir string, now time.Time) (State, error) {
	unlock, e := integrations.Lock(ctx, dir+".lock")
	if e != nil {
		return State{}, e
	}
	defer unlock()
	s, e := Load(dir)
	if e != nil {
		return s, e
	}
	s.Enabled = false
	if e = os.RemoveAll(filepath.Join(dir, "cache")); e != nil {
		return s, e
	}
	s.LastCode = "disabled"
	if e = Save(dir, s); e != nil {
		return s, e
	}
	f, e := openBinding(s.Binding)
	if os.IsNotExist(e) {
		return s, nil
	}
	if e != nil {
		return s, e
	}
	defer f.Close()
	if e = purgeMirror(f); e != nil {
		return s, e
	}
	e = publish(f, s, nil, now, true)
	return s, e
}

// Invalidate never removes pending input or frozen deliveries.
func Invalidate(ctx context.Context, dir, code string, now time.Time) error {
	unlock, e := integrations.Lock(ctx, dir+".lock")
	if e != nil {
		return e
	}
	defer unlock()
	s, e := Load(dir)
	if e != nil {
		return e
	}
	f, e := openBinding(s.Binding)
	if e != nil {
		return e
	}
	defer f.Close()
	return invalidate(dir, f, &s, code, now)
}
func invalidate(dir string, f *FS, s *State, code string, now time.Time) error {
	if e := purgeMirror(f); e != nil {
		return e
	}
	if e := os.RemoveAll(filepath.Join(dir, "cache")); e != nil {
		return e
	}
	s.LastCode = code
	s.LastContact = time.Time{}
	if e := Save(dir, *s); e != nil {
		return e
	}
	return publish(f, *s, nil, now, true)
}
func snapshotPath(dir, id string) string { return filepath.Join(dir, "cache", id+".json") }
func snapshots(dir string, s State) ([]Snapshot, error) {
	out := []Snapshot{}
	for _, c := range s.Binding.Cards {
		b, e := integrations.PrivateRead(snapshotPath(dir, c.ID))
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		var v Snapshot
		if json.Unmarshal(b, &v) != nil || v.Card.ID != c.ID || len(v.Body) > 64<<10 {
			return nil, ErrUnsafe
		}
		out = append(out, v)
	}
	return out, nil
}
func appendPending(dir string, s *State, r Record) error {
	for _, id := range s.Pending {
		if id == r.ID {
			return nil
		}
	}
	if len(s.Pending) >= MaxPending {
		return ErrCapacity
	}
	s.Pending = append(s.Pending, r.ID)
	seq := map[string]int64{}
	for _, id := range s.Pending {
		record, e := readRecord(dir, id)
		if e != nil {
			return e
		}
		seq[id] = record.Sequence
	}
	sort.SliceStable(s.Pending, func(i, j int) bool { return seq[s.Pending[i]] < seq[s.Pending[j]] })
	return nil
}

// Pending entries are appended in durable intake order. The caller repairs an
// orphan record before admitting any new source, so no filesystem sort reorders promises.
func classify(c Callbacks, e error) string {
	if e == nil {
		return ""
	}
	if c.Classify != nil {
		return c.Classify(e)
	}
	return "retry"
}
func code(c Callbacks, e error) string {
	if e == nil {
		return ""
	}
	if c.Code != nil {
		return c.Code(e)
	}
	return "file_transport_error"
}
func deliveryBound(b Binding, r Record) bool {
	d := r.Delivery
	return d != nil && d.Key == "anx-file-v1-"+b.ID+"-"+r.ID && d.PrincipalID == b.Actor && d.HostID == b.Host && d.BaseURL == b.BaseURL && d.Agent == b.Agent && d.CommandID == "events.create" && d.Key == fmt.Sprint(d.Body["request_key"]) && b.Actor == fmt.Sprint(d.Body["actor_id"])
}
func intake(dir string, f *FS, s *State, now time.Time) error {
	recovered, e := f.names("intake", MaxPending*2)
	if e != nil {
		return e
	}
	sort.Strings(recovered)
	fresh, e := f.names("outbox", MaxRecords+MaxPending)
	if e != nil {
		return e
	}
	sort.Strings(fresh)
	// Recover claimed originals first; a crash before journal publication never loses input.
	claims := append([]string{}, recovered...)
	for _, name := range fresh {
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			continue
		} // producer temp files are not complete entries
		if len(s.Pending)+len(claims) >= MaxPending || s.Records+len(claims) >= MaxRecords {
			s.LastCode = "capacity"
			break
		}
		claim := strings.TrimSuffix(name, ".json") + "." + UUID() + ".json"
		if len(name) > 128 {
			s.LastCode = "invalid_filename"
			continue
		}
		if e = f.rename("outbox/"+name, "intake/"+claim); e != nil {
			return e
		}
		claims = append(claims, claim)
	}
	// The fresh list is lexical filename order; recovery keeps stored sequence.
	for _, name := range claims {
		path := "intake/" + name
		raw, e := f.read(path, MaxEntry)
		if e != nil {
			s.LastCode = "unsafe_or_oversize_entry"
			continue
		} // retained, never followed or executed
		sourceID := strings.Split(name, ".")[0]
		entry, parseErr := Decode(raw)
		if !ValidUUID(sourceID) {
			sourceID = claimID(name)
			parseErr = fmt.Errorf("invalid_filename")
		}
		if entry.ID != sourceID && parseErr == nil {
			parseErr = fmt.Errorf("filename_id_mismatch")
		}
		hash := Digest(raw)
		r, e := readRecord(dir, sourceID)
		if e == nil {
			if r.Hash != hash {
				// Keep BOTH inputs. A changed UUID never changes the originally frozen request.
				conflictPath := filepath.Join(dir, "conflicts", sourceID+"-"+hash+".json")
				if _, err := integrations.PrivateRead(conflictPath); os.IsNotExist(err) {
					s.Failed++
				} else if err != nil {
					return err
				}
				if e = integrations.PrivateWrite(conflictPath, raw); e != nil {
					return e
				}
				if r.State != "confirmed" {
					r.State = "conflict"
					r.Reason = "id_conflict"
					if e = saveRecord(dir, r); e != nil {
						return e
					}
				}
				s.LastCode = "id_conflict"
				if e = archive(f, path, sourceID+"-"+hash, raw, "failed", "id_conflict", "", nil); e != nil {
					return e
				}
				continue
			}
			if r.State == "confirmed" || r.State == "failed" {
				if e = archive(f, path, r.ID, raw, r.State, r.Reason, r.Receipt, r.Warnings); e != nil {
					return e
				}
				continue
			}
		} else if !os.IsNotExist(e) {
			return e
		} else {
			s.Sequence++
			s.Records++
			r = Record{ID: sourceID, Hash: hash, Raw: raw, Input: entry, Sequence: s.Sequence, Received: now, State: "pending"}
			if _, ok := s.Binding.Card(entry.Card); !ok && parseErr == nil {
				parseErr = fmt.Errorf("card_not_allowlisted")
			}
			if parseErr != nil {
				r.State = "failed"
				r.Reason = parseErr.Error()
				s.Failed++
			}
			if e = saveRecord(dir, r); e != nil {
				return e
			}
		}
		if r.Sequence > s.Sequence {
			s.Sequence = r.Sequence
		}
		if int(r.Sequence) > s.Records {
			s.Records = int(r.Sequence)
		}
		if r.State == "failed" {
			if e = Save(dir, *s); e != nil {
				return e
			}
			if e = archive(f, path, r.ID, raw, r.State, r.Reason, "", r.Warnings); e != nil {
				return e
			}
			continue
		}
		if e = appendPending(dir, s, r); e != nil {
			return e
		}
		if e = Save(dir, *s); e != nil {
			return e
		}
	}
	return Save(dir, *s)
}
func archive(f *FS, claim, id string, raw []byte, state, reason, receipt string, warnings []string) error {
	folder := ".failed"
	if state == "confirmed" {
		folder = ".done"
	}
	if e := f.write("outbox/"+folder+"/"+id+".json", raw, 0600); e != nil {
		return e
	}
	if e := f.write("outbox/"+folder+"/"+id+".receipt.json", integrations.JSON(map[string]any{"state": state, "reason": reason, "event_ref": receipt, "warnings": warnings}), 0600); e != nil {
		return e
	}
	return f.remove(claim)
}
func Tick(ctx context.Context, dir string, now time.Time, c Callbacks) (State, error) {
	unlock, e := integrations.Lock(ctx, dir+".lock")
	if e != nil {
		return State{}, e
	}
	defer unlock()
	s, e := Load(dir)
	if e != nil || !s.Enabled {
		return s, e
	}
	f, e := openBinding(s.Binding)
	if e != nil {
		return s, e
	}
	defer f.Close()
	if e = c.Validate(ctx, s.Binding); e != nil {
		if kind := classify(c, e); kind == "denied" || kind == "identity" {
			if ie := invalidate(dir, f, &s, code(c, e), now); ie != nil {
				return s, ie
			}
		} else {
			s.LastCode = code(c, e)
			_ = Save(dir, s)
			views, _ := snapshots(dir, s)
			_ = publish(f, s, views, now, false)
		}
		return s, e
	}
	s.LastContact = now
	s.LastCode = ""
	var tickErr error
	if e = intake(dir, f, &s, now); e != nil {
		return s, e
	}
	// One directory FIFO prevents a later promise overtaking an uncertain send.
	for n := 0; len(s.Pending) > 0 && n < integrations.MaxDrain; n++ {
		r, e := readRecord(dir, s.Pending[0])
		if e != nil {
			return s, e
		}
		if r.State == "conflict" || r.State == "uncertain" {
			s.LastCode = r.Reason
			break
		}
		if r.State == "confirmed" || r.State == "failed" {
			s.Pending = s.Pending[1:]
			if e = Save(dir, s); e != nil {
				return s, e
			}
			continue
		}
		if !r.FirstAttempt.IsZero() && now.Sub(r.FirstAttempt) >= ReplayCutoff {
			r.State = "uncertain"
			r.Reason = "replay_window_expired"
			s.Failed++
			if e = saveRecord(dir, r); e != nil {
				return s, e
			}
			s.LastCode = r.Reason
			break
		}
		if now.Before(r.RetryAt) {
			break
		}
		r.Attempts++
		if r.FirstAttempt.IsZero() {
			r.FirstAttempt = now
		}
		r.RetryAt = now.Add((time.Second << min(r.Attempts, 5)) + time.Duration(r.ID[0]%10)*50*time.Millisecond)
		if e = saveRecord(dir, r); e != nil {
			return s, e
		}
		if r.Delivery == nil {
			d, warnings, err := c.Prepare(ctx, s.Binding, r.Input, r.Received)
			if err != nil {
				e = err
			} else {
				r.Delivery = &d
				r.Warnings = warnings
			}
		}
		if e == nil && !deliveryBound(s.Binding, r) {
			e = integrations.ErrIdentityChanged
		}
		if e == nil {
			if e = saveRecord(dir, r); e != nil {
				return s, e
			}
			if e = c.Validate(ctx, s.Binding); e == nil {
				r.Receipt, e = c.Send(ctx, s.Binding, *r.Delivery)
			}
		}
		if e != nil {
			tickErr = e
			r.Reason = code(c, e)
			s.LastCode = r.Reason
			if kind := classify(c, e); kind == "invalid" {
				r.State = "failed"
				s.Failed++
			} else if kind == "denied" || kind == "identity" {
				if ie := invalidate(dir, f, &s, r.Reason, now); ie != nil {
					return s, ie
				}
			}
			if e = saveRecord(dir, r); e != nil {
				return s, e
			}
			if kind := classify(c, tickErr); kind == "identity" || kind == "denied" {
				return s, tickErr
			}
			break
		}
		r.State = "confirmed"
		r.Reason = ""
		if e = saveRecord(dir, r); e != nil {
			return s, e
		}
		s.Pending = s.Pending[1:]
		if e = Save(dir, s); e != nil {
			return s, e
		}
	}
	// Refresh only an explicit admitted cohort, never enumerate workspace cards.
	revoked := false
	for n := 0; n < min(10, len(s.Binding.Cards)) && ctx.Err() == nil; n++ {
		i := s.RefreshCursor % len(s.Binding.Cards)
		s.RefreshCursor = (i + 1) % len(s.Binding.Cards)
		card := s.Binding.Cards[i]
		view, err := c.Refresh(ctx, s.Binding, card)
		if err != nil {
			s.LastCode = code(c, err)
			if classify(c, err) == "identity" {
				if ie := invalidate(dir, f, &s, s.LastCode, now); ie != nil {
					return s, ie
				}
				return s, err
			}
			tickErr = err
			if kind := classify(c, err); kind == "denied" || kind == "identity" {
				if e = purgeMirror(f); e != nil {
					return s, e
				}
				if e = os.Remove(snapshotPath(dir, card.ID)); e != nil && !os.IsNotExist(e) {
					return s, e
				}
				revoked = true
			}
			continue
		}
		if view.Card != card || len(view.Body) > 64<<10 {
			return s, ErrUnsafe
		}
		view.AsOf = now
		if e = integrations.PrivateWrite(snapshotPath(dir, card.ID), integrations.JSON(view)); e != nil {
			return s, e
		}
		s.LastContact = now
	}
	if e = Save(dir, s); e != nil {
		return s, e
	}
	views, e := snapshots(dir, s)
	if e != nil {
		return s, e
	}
	e = publish(f, s, views, now, revoked)
	if e != nil {
		return s, e
	}
	return s, tickErr
}

func claimID(name string) string {
	h := Digest([]byte(name))
	return h[:8] + "-" + h[8:12] + "-4" + h[13:16] + "-8" + h[17:20] + "-" + h[20:32]
}

// Inspect is local-only: doctor never drains a queue or refreshes cached data.
func Inspect(dir string, now time.Time) (map[string]any, error) {
	s, e := Load(dir)
	if e != nil {
		return nil, e
	}
	out := map[string]any{"enabled": s.Enabled, "directory_uuid": s.Binding.ID, "pending": len(s.Pending), "failed": s.Failed, "last_error_code": s.LastCode, "last_server_contact": s.LastContact, "stale_after": s.LastContact.Add(StaleAfter), "stale": !s.Enabled || s.LastContact.IsZero() || now.Sub(s.LastContact) >= StaleAfter}
	f, e := openBinding(s.Binding)
	if e != nil {
		return out, e
	}
	defer f.Close()
	for _, path := range []string{"outbox", "outbox/.done", "outbox/.failed", "intake", "mirror", "mirror/generations"} {
		d, _, err := f.parent(path + "/.probe")
		if err != nil {
			return out, err
		}
		d.Close()
	}
	target, e := f.readlink("mirror/current")
	if e != nil {
		return out, e
	}
	generation := strings.TrimPrefix(target, "generations/")
	if generation == target || !ValidUUID(generation) {
		return out, ErrUnsafe
	}
	raw, e := f.read("mirror/"+target+"/STATUS.json", 16<<10)
	if e != nil {
		return out, e
	}
	var status Status
	if json.Unmarshal(raw, &status) != nil || status.Generation != generation {
		return out, ErrUnsafe
	}
	out["mirror"] = status
	claims, e := f.names("intake", MaxPending*2)
	if e != nil {
		return out, e
	}
	out["retained_claims"] = len(claims)
	if len(s.Pending) > 0 {
		r, err := readRecord(dir, s.Pending[0])
		if err != nil {
			return out, err
		}
		out["head"] = map[string]any{"id": r.ID, "state": r.State, "reason": r.Reason, "first_attempt": r.FirstAttempt, "retry_at": r.RetryAt, "attempts": r.Attempts, "warnings": r.Warnings}
		if r.State == "uncertain" || r.State == "conflict" || r.State == "failed" {
			return out, fmt.Errorf("file transport requires reconciliation: %s", r.Reason)
		}
	}
	if s.LastCode != "" {
		return out, fmt.Errorf("file transport degraded: %s", s.LastCode)
	}
	if s.Enabled && (s.LastContact.IsZero() || now.Sub(s.LastContact) >= StaleAfter || !status.Enabled || status.Stale || !now.Before(status.StaleAfter)) {
		return out, fmt.Errorf("file transport mirror is stale")
	}
	return out, nil
}
