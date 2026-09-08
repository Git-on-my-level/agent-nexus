package observation

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// This is real runtime enforcement against harmless test-owned fixtures. It is
// opt-in because it requires an unprivileged Linux host, cc with static libc,
// Bubblewrap/user namespaces and prlimit. A skip is NOT security qualification.
func TestLinuxIsolationEnforcement(t *testing.T) {
	if os.Getenv("ANX_OBSERVATION_ISOLATION_TEST") != "1" {
		t.Skip("set ANX_OBSERVATION_ISOLATION_TEST=1 on approved Linux runner; not runtime-qualified on this host")
	}
	runner := NewBubblewrapRunner()
	if err := runner.Available(); err != nil {
		t.Fatal(err)
	}
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Fatal("static fixture compiler unavailable")
	}
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "private-fixture")
	if err := os.WriteFile(sentinel, []byte("harmless fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	source := fmt.Sprintf(`#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <string.h>
#include <sched.h>
#include <sys/socket.h>
#include <netinet/in.h>
int main(void) {
 char input[4096]; size_t n=fread(input,1,sizeof(input)-1,stdin); input[n]=0;
 if (strstr(input,"reject")) { puts("{\"error\":\"fixture rejected\"}"); return 0; }
 if (fopen(%q,"r") || getenv("ANX_FIXTURE_SECRET") || fopen("/reader","w") || fopen("/tmp/fixture","w")) return 10;
 if (unshare(CLONE_NEWUSER)==0) return 11;
 int fd=socket(AF_INET,SOCK_STREAM,0); struct sockaddr_in a={0}; a.sin_family=AF_INET; a.sin_port=htons(%d); a.sin_addr.s_addr=htonl(0x7f000001);
 if(fd>=0 && connect(fd,(struct sockaddr*)&a,sizeof(a))==0) return 12;
 if(fd>=0) close(fd);
 puts("{\"facts\":{\"fixture_denials_verified\":true},\"uncertainty\":[]}"); return 0;
}
`, sentinel, port)
	cfile := filepath.Join(dir, "fixture.c")
	binary := filepath.Join(dir, "fixture-reader")
	if err := os.WriteFile(cfile, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(cc, "-static", "-O2", "-o", binary, cfile).CombinedOutput(); err != nil {
		t.Fatalf("build harmless static fixture: %v: %s", err, out)
	}
	t.Setenv("ANX_FIXTURE_SECRET", "harmless-not-a-real-secret")
	limits := fixtureJITPolicy().Limits
	raw, err := runner.Run(context.Background(), binary, []byte(`{}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	var output TransformOutput
	if err := json.Unmarshal(raw, &output); err != nil || output.Facts["fixture_denials_verified"] != true {
		t.Fatalf("runtime isolation not verified: %s %v", raw, err)
	}
	manager, err := NewJITManager(filepath.Join(dir, "managed"), fixtureJITPolicy())
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	version, err := manager.Stage(Manifest{AdapterID: "fixture", Target: fixtureTarget(), Limits: limits}, artifact)
	if err != nil {
		t.Fatal(err)
	}
	cases := []ValidationCase{{Name: "enforced-denials", Input: []byte(`{}`), WantValid: true}, {Name: "schema-rejection", Input: []byte(`{"reject":true}`), WantValid: false}}
	if err := manager.Validate(context.Background(), "fixture", version.Revision, cases); err != nil {
		t.Fatal(err)
	}
}
