package observation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func isolationRunnerOrSkip(t *testing.T) isolatedExecutor {
	t.Helper()
	runner := NewIsolatedRunner()
	if err := runner.Available(); err != nil {
		if os.Getenv("ANX_OBSERVATION_ISOLATION_TEST") == "1" {
			t.Fatal(err)
		}
		t.Skipf("isolation runner unavailable on this host: %v", err)
	}
	return runner
}

func compileIsolatedFixture(t *testing.T, dir, name, source string) string {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Fatal("fixture compiler unavailable")
	}
	cfile := filepath.Join(dir, name+".c")
	binary := filepath.Join(dir, name)
	if err := os.WriteFile(cfile, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-O2", "-o", binary, cfile}
	if runtime.GOOS == "linux" {
		args = []string{"-static", "-O2", "-o", binary, cfile}
	}
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("build fixture %s: %v: %s", name, err, out)
	}
	resolved, err := filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func requireIsolationError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("forbidden reader succeeded")
	}
	var re *ReadError
	if !errors.As(err, &re) || (re.Kind != ErrIsolation && re.Kind != ErrPolicy && re.Kind != ErrLimit) {
		t.Fatalf("want isolation/policy/limit denial, got %v", err)
	}
}

func TestSeatbeltProfileIsDenyDefault(t *testing.T) {
	profile := seatbeltProfile("/tmp/reader", "/tmp/scratch")
	for _, needle := range []string{"(deny default)", "(deny network*)", "(deny process-fork)", `(deny file-read-data (subpath "/Users")`, "(allow process-exec*", `(literal "/tmp/reader")`, `(subpath "/tmp/scratch")`} {
		if !strings.Contains(profile, needle) {
			t.Fatalf("profile missing %q:\n%s", needle, profile)
		}
	}
}

func TestNewIsolatedRunnerMatchesGOOS(t *testing.T) {
	runner := NewIsolatedRunner()
	switch runtime.GOOS {
	case "darwin":
		if _, ok := runner.(*SeatbeltRunner); !ok {
			t.Fatal("darwin must use SeatbeltRunner")
		}
		if NewBubblewrapRunner().Available() == nil {
			t.Fatal("bubblewrap reported available on darwin")
		}
	case "linux":
		if _, ok := runner.(*BubblewrapRunner); !ok {
			t.Fatal("linux must use BubblewrapRunner")
		}
	default:
		if runner.Available() == nil {
			t.Fatal("unsupported OS reported isolation")
		}
	}
}

func TestIsolationConformance(t *testing.T) {
	runner := isolationRunnerOrSkip(t)
	dir := t.TempDir()
	home := os.Getenv("HOME")
	if home == "" {
		t.Fatal("HOME is required for the home-denial fixture")
	}
	readPath := home
	writePath := filepath.Join(dir, "must-not-write")
	unshareCheck := ""
	if runtime.GOOS == "linux" {
		readPath = filepath.Join(dir, "private-fixture")
		if err := os.WriteFile(readPath, []byte("harmless fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		writePath = "/tmp/fixture"
		unshareCheck = "if (unshare(CLONE_NEWUSER)==0) return 11;\n"
	} else {
		unshareCheck = "if (fork()>=0) return 14;\n"
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
#include <fcntl.h>
#include <errno.h>
#include <sys/socket.h>
#include <netinet/in.h>
#ifdef __linux__
#include <sched.h>
#endif
int main(void) {
 char input[4096]; size_t n=fread(input,1,sizeof(input)-1,stdin); input[n]=0;
 if (strstr(input,"reject")) { puts("{\"error\":\"fixture rejected\"}"); return 0; }
 if (open(%q,O_RDONLY)>=0 || getenv("ANX_FIXTURE_SECRET") || open(%q,O_WRONLY|O_CREAT,0600)>=0) return 10;
 %s
 int fd=socket(AF_INET,SOCK_STREAM,0); struct sockaddr_in a; memset(&a,0,sizeof(a)); a.sin_family=AF_INET; a.sin_port=htons(%d); a.sin_addr.s_addr=htonl(0x7f000001);
 if(fd>=0) { fcntl(fd,F_SETFL,O_NONBLOCK); if(connect(fd,(struct sockaddr*)&a,sizeof(a))==0 || errno==EINPROGRESS) { close(fd); return 12; } close(fd); }
 puts("{\"facts\":{\"fixture_denials_verified\":true},\"uncertainty\":[]}"); return 0;
}
`, readPath, writePath, unshareCheck, port)
	binary := compileIsolatedFixture(t, dir, "fixture-reader", source)
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

func TestIsolationNegativeDenials(t *testing.T) {
	runner := isolationRunnerOrSkip(t)
	dir := t.TempDir()
	home := os.Getenv("HOME")
	if home == "" {
		t.Fatal("HOME is required")
	}
	outside := filepath.Join(dir, "outside")
	limits := fixtureJITPolicy().Limits
	limits.OutputBytes = 1024
	t.Run("home", func(t *testing.T) {
		src := fmt.Sprintf(`#include <fcntl.h>
#include <stdio.h>
int main(void){ int fd=open(%q,O_RDONLY); if(fd>=0){ puts("{\"facts\":{\"home\":true},\"uncertainty\":[]}"); return 0;} return 10;}
`, filepath.Join(home))
		bin := compileIsolatedFixture(t, dir, "read-home", src)
		_, err := runner.Run(context.Background(), bin, []byte(`{}`), limits)
		t.Logf("home denial: %v", err)
		requireIsolationError(t, err)
	})
	t.Run("write-outside-scratch", func(t *testing.T) {
		src := fmt.Sprintf(`#include <stdio.h>
int main(void){ FILE*f=fopen(%q,"w"); if(f){ fputs("x",f); fclose(f); puts("{\"facts\":{\"wrote\":true},\"uncertainty\":[]}"); return 0;} return 11;}
`, outside)
		bin := compileIsolatedFixture(t, dir, "write-outside", src)
		_, err := runner.Run(context.Background(), bin, []byte(`{}`), limits)
		t.Logf("write denial: %v", err)
		requireIsolationError(t, err)
		if _, err := os.Stat(outside); err == nil {
			t.Fatal("write escaped the scratch directory")
		}
	})
	t.Run("tcp", func(t *testing.T) {
		src := `#include <stdio.h>
#include <string.h>
#include <unistd.h>
#include <sys/socket.h>
#include <netinet/in.h>
int main(void){ int fd=socket(AF_INET,SOCK_STREAM,0); struct sockaddr_in a; memset(&a,0,sizeof(a)); a.sin_family=AF_INET; a.sin_port=htons(80); a.sin_addr.s_addr=htonl(0x08080808); if(fd>=0 && connect(fd,(struct sockaddr*)&a,sizeof(a))==0){ puts("{\"facts\":{\"tcp\":true},\"uncertainty\":[]}"); return 0;} return 13;}
`
		bin := compileIsolatedFixture(t, dir, "open-tcp", src)
		_, err := runner.Run(context.Background(), bin, []byte(`{}`), limits)
		t.Logf("tcp denial: %v", err)
		requireIsolationError(t, err)
	})
	t.Run("fork", func(t *testing.T) {
		src := `#include <stdio.h>
#include <unistd.h>
int main(void){ pid_t p=fork(); if(p>=0){ puts("{\"facts\":{\"fork\":true},\"uncertainty\":[]}"); return 0;} return 14;}
`
		bin := compileIsolatedFixture(t, dir, "fork-bomb", src)
		_, err := runner.Run(context.Background(), bin, []byte(`{}`), limits)
		t.Logf("fork denial: %v", err)
		requireIsolationError(t, err)
	})
	t.Run("output-budget", func(t *testing.T) {
		src := `#include <stdio.h>
int main(void){ for(int i=0;i<4096;i++) fputs("{\"facts\":{\"overflow\":true},\"uncertainty\":[]}\n", stdout); return 0;}
`
		bin := compileIsolatedFixture(t, dir, "output-bomb", src)
		_, err := runner.Run(context.Background(), bin, []byte(`{}`), limits)
		t.Logf("output denial: %v", err)
		requireIsolationError(t, err)
	})
}

func TestIsolatedTransformLifecycle(t *testing.T) {
	_ = isolationRunnerOrSkip(t)
	dir := t.TempDir()
	src := `#include <stdio.h>
#include <string.h>
int main(void){
 char input[65536]; size_t n=fread(input,1,sizeof(input)-1,stdin); input[n]=0;
 if (strstr(input,"\"reject\":true")) { puts("{\"error\":\"fixture rejected\"}"); return 0; }
 puts("{\"facts\":{\"transform\":\"handwritten\"},\"uncertainty\":[\"hand-written artifact\"],\"evidence\":[]}");
 return 0;
}
`
	binary := compileIsolatedFixture(t, dir, "transform", src)
	artifact, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewJITManager(filepath.Join(dir, "managed"), fixtureJITPolicy())
	if err != nil {
		t.Fatal(err)
	}
	limits := fixtureJITPolicy().Limits
	version, err := manager.Stage(Manifest{AdapterID: "fixture", Target: fixtureTarget(), Limits: limits}, artifact)
	if err != nil {
		t.Fatal(err)
	}
	cases := []ValidationCase{{Name: "happy", Input: []byte(`{}`), WantValid: true}, {Name: "rejected", Input: []byte(`{"reject":true}`), WantValid: false}}
	if err := manager.Validate(context.Background(), "fixture", version.Revision, cases); err != nil {
		t.Fatal(err)
	}
	source := trustedFixtureReader{fixtureReader{read: func(_ context.Context, target Target) (Report, error) {
		return finishReport(newReport(target, "builtin:fixture"))
	}}}
	if _, err := manager.Canary(context.Background(), "fixture", version.Revision, source); err != nil {
		t.Fatal(err)
	}
	if err := manager.Activate("fixture", version.Revision); err != nil {
		t.Fatal(err)
	}
	report, err := manager.Read(context.Background(), "fixture", source)
	if err != nil {
		t.Fatal(err)
	}
	findings, _ := report.Facts["generated_findings"].(map[string]any)
	if findings["transform"] != "handwritten" {
		t.Fatalf("generated findings missing: %+v", report.Facts)
	}
}
