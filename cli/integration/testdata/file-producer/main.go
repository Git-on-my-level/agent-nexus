// A deliberately unprivileged acceptance-test producer. It receives only JSON,
// an outbox directory, and denial-probe addresses. It does not link the CLI.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func fail(e any) { fmt.Fprintln(os.Stderr, e); os.Exit(1) }
func main() {
	if len(os.Args) != 6 {
		fail("expected outbox, UUID, network address, inaccessible credential, inaccessible mirror")
	}
	for _, s := range os.Environ() {
		if strings.HasPrefix(s, "ANX_") || strings.Contains(strings.SplitN(s, "=", 2)[0], "TOKEN") {
			fail("producer inherited credential environment")
		}
	}
	if c, e := net.DialTimeout("tcp", os.Args[3], time.Second); e == nil {
		c.Close()
		fail("network was available")
	}
	for _, path := range os.Args[4:] {
		if b, e := os.ReadFile(path); e == nil {
			fail(fmt.Sprintf("outside mount was readable (%d bytes)", len(b)))
		}
	}
	raw, e := readStdinBytes()
	if e != nil {
		fail(e)
	}
	var entry map[string]any
	if e = json.Unmarshal(raw, &entry); e != nil || entry["id"] != os.Args[2] {
		fail("bad input")
	}
	path := filepath.Join(os.Args[1], ".producer-temp")
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		fail(e)
	}
	if _, e = f.Write(raw); e != nil {
		fail(e)
	}
	if e = f.Sync(); e != nil {
		fail(e)
	}
	if e = f.Close(); e != nil {
		fail(e)
	}
	if e = os.Rename(path, filepath.Join(os.Args[1], os.Args[2]+".json")); e != nil {
		fail(e)
	}
	d, e := os.Open(os.Args[1])
	if e != nil {
		fail(e)
	}
	if e = d.Sync(); e != nil {
		fail(e)
	}
	d.Close()
	fmt.Println("isolated: network denied; credentials and mirror denied; outbox published")
}

func readStdinBytes() ([]byte, error) { return io.ReadAll(io.LimitReader(os.Stdin, 65537)) }
