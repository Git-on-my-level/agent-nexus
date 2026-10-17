package filetransport

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"agent-nexus-cli/internal/integrations"
)

// One current symlink selects a complete immutable generation. Readers that need
// multiple files must pin its target, or compare generation IDs before/after.
// Only the outbox is producer-writable; the mirror is a read-only cache.
func publish(f *FS, s State, views []Snapshot, now time.Time, revoked bool) error {
	prior, _ := f.readlink("mirror/current")
	previous := strings.TrimPrefix(prior, "generations/")
	if previous == prior || !ValidUUID(previous) {
		previous = ""
	}
	id := UUID()
	build := "mirror/.build-" + id
	generation := "mirror/generations/" + id
	if e := f.mkdir(build); e != nil {
		return e
	}
	clean := true
	defer func() {
		if clean {
			_ = f.removeTree(build)
		}
	}()
	if e := f.mkdir(build + "/cards"); e != nil {
		return e
	}
	status := Status{Version: 1, Generation: id, AsOf: now, LastContact: s.LastContact, StaleAfter: s.LastContact.Add(StaleAfter), Stale: !s.Enabled || s.LastContact.IsZero() || now.Sub(s.LastContact) >= StaleAfter, Enabled: s.Enabled, Pending: len(s.Pending), Failed: s.Failed, LastCode: s.LastCode}
	var index strings.Builder
	fmt.Fprintf(&index, "# ANX card mirror\n\nGeneration: %s\nAs of: %s\nStale after: %s\n\nThis is a read-only cache. Compare stale_after with your current clock; a stopped bridge cannot update this banner.\n\n", id, now.Format(time.RFC3339Nano), status.StaleAfter.Format(time.RFC3339Nano))
	if !s.Enabled {
		index.WriteString("Transport disabled.\n\n")
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Card.ID < views[j].Card.ID })
	for _, v := range views {
		if _, ok := s.Binding.Card(v.Card.ID); !ok || !cardIDPattern.MatchString(v.Card.ID) {
			return ErrUnsafe
		}
		stale := !s.Enabled || now.Sub(v.AsOf) >= StaleAfter
		body := fmt.Sprintf("# %s\n\nas_of: %s\nstale_after: %s\nresource_version: %s\nstale: %t\ngeneration: %s\n\nRead-only cache; check stale_after against your clock.\n\n%s\n", v.Card.Ref, v.AsOf.Format(time.RFC3339Nano), v.AsOf.Add(StaleAfter).Format(time.RFC3339Nano), cleanText(v.Version), stale, id, v.Body)
		if e := f.write(build+"/cards/"+v.Card.ID+".md", []byte(body), 0400); e != nil {
			return e
		}
		fmt.Fprintf(&index, "- [%s](cards/%s.md) — as of %s\n", v.Card.Ref, v.Card.ID, v.AsOf.Format(time.RFC3339Nano))
	}
	if e := f.write(build+"/INDEX.md", []byte(index.String()), 0400); e != nil {
		return e
	}
	if e := f.write(build+"/STATUS.json", integrations.JSON(status), 0400); e != nil {
		return e
	}
	if e := f.chmodDir(build+"/cards", 0500); e != nil {
		return e
	}
	if e := f.rename(build, generation); e != nil {
		return e
	}
	clean = false
	if e := f.chmodDir(generation, 0500); e != nil {
		return e
	}
	temp := "mirror/.current-" + id
	if e := f.symlink("generations/"+id, temp); e != nil {
		return e
	}
	if e := f.rename(temp, "mirror/current"); e != nil {
		return e
	}
	// Stable convenience links share the one generation switch. Never replace a
	// producer file: .anx and these paths are owned by the enabled transport.
	for _, name := range []string{"STATUS.json", "INDEX.md", "cards"} {
		target := "mirror/current/" + name
		// Existing owned links can be replaced atomically without following them.
		link := ".link-" + UUID()
		if e := f.symlink(target, link); e != nil {
			return e
		}
		if e := f.rename(link, name); e != nil {
			return e
		}
	}
	names, e := f.names("mirror/generations", MaxRecords)
	if e != nil {
		return e
	}
	// Keep one prior generation for readers spanning a normal swap. Revocation
	// purges every generation before publication; no old path remains reachable.
	for _, old := range names {
		if old == id || !revoked && old == previous {
			continue
		}
		if !ValidUUID(old) {
			return ErrUnsafe
		}
		if e = f.removeTree("mirror/generations/" + old); e != nil {
			return e
		}
	}
	// Incomplete builds never become current. Clean prior crash leftovers too.
	builds, e := f.names("mirror", MaxRecords)
	if e != nil {
		return e
	}
	for _, old := range builds {
		if strings.HasPrefix(old, ".build-") {
			if e = f.removeTree("mirror/" + old); e != nil {
				return e
			}
		}
	}
	return nil
}
func cleanText(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", " "), "\x00", "")
}
func purgeMirror(f *FS) error {
	builds, err := f.names("mirror", MaxRecords)
	if err != nil {
		return err
	}
	for _, name := range builds {
		if strings.HasPrefix(name, ".build-") {
			if err = f.removeTree("mirror/" + name); err != nil {
				return err
			}
		}
	}
	if e := f.remove("mirror/current"); e != nil && !os.IsNotExist(e) {
		return e
	}
	for _, path := range []string{"mirror/generations"} {
		names, e := f.names(path, MaxRecords)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		for _, n := range names {
			if !ValidUUID(n) {
				return ErrUnsafe
			}
			if e = f.removeTree(path + "/" + n); e != nil {
				return e
			}
		}
	}
	return nil
}
