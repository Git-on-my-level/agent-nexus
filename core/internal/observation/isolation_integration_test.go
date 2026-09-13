package observation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

// isolationWorkDir is the compile/stage root for real sandbox tests. This
// host's TMPDIR is /Volumes/scratch/tmp, which the Seatbelt profile denies
// for content reads. Under full-suite load, path aliasing of that volume
// made sandbox-exec fail even after the runner copied the artifact.
func isolationWorkDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "darwin" {
		return t.TempDir()
	}
	dir, err := os.MkdirTemp(seatbeltTempRoot(), "anx-obs-test-")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func compileIsolatedFixture(t *testing.T, dir, name, source string) string {
	t.Helper()
	_ = dir
	outDir := isolationWorkDir(t)
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Fatal("fixture compiler unavailable")
	}
	cfile := filepath.Join(outDir, name+".c")
	binary := filepath.Join(outDir, name)
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
	for _, needle := range []string{"(deny default)", "(deny network*)", "(deny process-fork)", "(allow process-exec*", `(literal "/tmp/reader")`, `(subpath "/tmp/scratch")`} {
		if !strings.Contains(profile, needle) {
			t.Fatalf("profile missing %q:\n%s", needle, profile)
		}
	}

	for _, name := range []string{"hw.ncpu", "hw.pagesize", "kern.osrelease", "kern.version", "hw.memsize", "sysctl.proc_translated", "hw.optional.armv8_1_atomics", "hw.optional.armv8_crc32", "hw.optional.armv8_2_sha512", "hw.optional.armv8_2_sha3", "hw.optional.arm.FEAT_DIT"} {
		if !strings.Contains(profile, `(allow sysctl-read (sysctl-name "`+name+`"))`) {
			t.Fatalf("missing named sysctl %s", name)
		}
	}
	for _, broad := range []string{"(allow sysctl-read)", "(sysctl-name-prefix", "kern.procargs2", "kern.proc.", "(allow file-read*)", "(allow file-map-executable)", "(allow mach-lookup)", "(allow mach-priv-host-port)", "(allow ipc-posix-shm)"} {
		if strings.Contains(profile, broad) {
			t.Fatalf("ambient permission %s", broad)
		}
	}
	for _, path := range []string{"/Users", "/Volumes", "/Applications", "/opt", "/private/etc", "/private/var", "/Library", "/tmp"} {
		if strings.Contains(profile, `(subpath "`+path+`")`) {
			t.Fatalf("broad read root %s", path)
		}
	}
	for _, bad := range []string{"relative", "/tmp/reader\"injection", "/tmp/new\nline"} {
		if got := seatbeltProfile(bad, "/tmp/scratch"); got != "(version 1)(deny default)" {
			t.Fatalf("unsafe artifact allowed %q", bad)
		}
		if got := seatbeltProfile("/tmp/reader", bad); got != "(version 1)(deny default)" {
			t.Fatalf("unsafe scratch allowed %q", bad)
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
	dir := isolationWorkDir(t)
	home := os.Getenv("HOME")
	if home == "" {
		t.Fatal("HOME is required for the home-denial fixture")
	}
	// Denied-path probes must be non-blocking. connect()/fork() in the success
	// path hung or killed the fixture when sandboxd was slow under make check
	// load; those denials stay in TestIsolationNegativeDenials.
	readPath := home
	writePath := filepath.Join(t.TempDir(), "must-not-write")
	unshareCheck := ""
	if runtime.GOOS == "linux" {
		readPath = filepath.Join(t.TempDir(), "private-fixture")
		if err := os.WriteFile(readPath, []byte("harmless fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		writePath = "/tmp/fixture"
		unshareCheck = "if (unshare(CLONE_NEWUSER)==0) return 11;\n"
	}
	source := fmt.Sprintf(`#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <string.h>
#include <fcntl.h>
#ifdef __linux__
#include <sched.h>
#endif
int main(void) {
 char input[4096]; size_t n=fread(input,1,sizeof(input)-1,stdin); input[n]=0;
 if (strstr(input,"reject")) { puts("{\"error\":\"fixture rejected\"}"); return 0; }
 if (open(%q,O_RDONLY)>=0 || getenv("ANX_FIXTURE_SECRET") || open(%q,O_WRONLY|O_CREAT,0600)>=0) return 10;
 %s
 puts("{\"facts\":{\"fixture_denials_verified\":true},\"uncertainty\":[]}"); return 0;
}
`, readPath, writePath, unshareCheck)
	binary := compileIsolatedFixture(t, dir, "fixture-reader", source)
	t.Setenv("ANX_FIXTURE_SECRET", "harmless-not-a-real-secret")
	limits := isolationTestPolicy().Limits
	raw, err := runner.Run(context.Background(), binary, []byte(`{}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	var output TransformOutput
	if err := json.Unmarshal(raw, &output); err != nil || output.Facts["fixture_denials_verified"] != true {
		t.Fatalf("runtime isolation not verified: %s %v", raw, err)
	}
	manager, err := NewJITManager(filepath.Join(dir, "managed"), isolationTestPolicy())
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
	dir := isolationWorkDir(t)
	home := os.Getenv("HOME")
	if home == "" {
		t.Fatal("HOME is required")
	}
	outside := filepath.Join(dir, "outside")
	limits := isolationTestPolicy().Limits
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
		src := `#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>
#include <sys/socket.h>
#include <netinet/in.h>
int main(void){ int fd=socket(AF_INET,SOCK_STREAM,0); if(fd>=0) fcntl(fd,F_SETFL,O_NONBLOCK); struct sockaddr_in a; memset(&a,0,sizeof(a)); a.sin_family=AF_INET; a.sin_port=htons(80); a.sin_addr.s_addr=htonl(0x08080808); if(fd>=0 && connect(fd,(struct sockaddr*)&a,sizeof(a))==0){ puts("{\"facts\":{\"tcp\":true},\"uncertainty\":[]}"); return 0;} return 13;}
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
	dir := isolationWorkDir(t)
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
	manager, err := NewJITManager(filepath.Join(dir, "managed"), isolationTestPolicy())
	if err != nil {
		t.Fatal(err)
	}
	limits := isolationTestPolicy().Limits
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

const canaryFailTransform = `#include <stdio.h>
#include <string.h>
int main(void){
 char input[65536]; size_t n=fread(input,1,sizeof(input)-1,stdin); input[n]=0;
 if (strstr(input,"\"reject\":true")) { puts("{\"error\":\"fixture rejected\"}"); return 0; }
 if (n>200 || strstr(input,"github.com") || strstr(input,"multica")) { puts("{\"error\":\"snapshot not handled\"}"); return 0; }
 puts("{\"facts\":{\"transform\":\"narrow-fixture\"},\"uncertainty\":[],\"evidence\":[]}");
 return 0;
}
`

const policyTripTransform = `#include <stdio.h>
#include <string.h>
int main(void){
 char input[65536]; size_t n=fread(input,1,sizeof(input)-1,stdin); input[n]=0;
 if (strstr(input,"\"reject\":true")) { puts("{\"error\":\"fixture rejected\"}"); return 0; }
 if (strstr(input,"\"trip\":true")) {
  puts("{\"facts\":{\"trip\":true},\"uncertainty\":[],\"evidence\":[{\"kind\":\"issue\",\"reference\":\"https://unapproved.invalid/invented\",\"knowledge\":\"reported\"}]}");
  return 0;
 }
 puts("{\"facts\":{\"transform\":\"policy-trip\"},\"uncertainty\":[],\"evidence\":[]}");
 return 0;
}
`

func TestRealCanaryFailureDoesNotActivate(t *testing.T) {
	_ = isolationRunnerOrSkip(t)
	dir := isolationWorkDir(t)
	policy := isolationTestPolicy()
	manager, err := NewJITManager(filepath.Join(dir, "managed"), policy)
	if err != nil {
		t.Fatal(err)
	}
	limits := policy.Limits
	goodBin := compileIsolatedFixture(t, dir, "good-transform", `#include <stdio.h>
#include <string.h>
int main(void){
 char input[65536]; size_t n=fread(input,1,sizeof(input)-1,stdin); input[n]=0;
 if (strstr(input,"\"reject\":true")) { puts("{\"error\":\"fixture rejected\"}"); return 0; }
 puts("{\"facts\":{\"transform\":\"good\"},\"uncertainty\":[],\"evidence\":[]}");
 return 0;
}
`)
	goodBytes, err := os.ReadFile(goodBin)
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{AdapterID: "fixture", Target: fixtureTarget(), Limits: limits}
	good, err := manager.Stage(manifest, goodBytes)
	if err != nil {
		t.Fatal(err)
	}
	cases := []ValidationCase{{Name: "happy", Input: []byte(`{"title":"ok"}`), WantValid: true}, {Name: "rejected", Input: []byte(`{"reject":true}`), WantValid: false}}
	if err := manager.Validate(context.Background(), "fixture", good.Revision, cases); err != nil {
		t.Fatal(err)
	}
	source := trustedFixtureReader{fixtureReader{read: func(_ context.Context, target Target) (Report, error) {
		return finishReport(newReport(target, "builtin:fixture"))
	}}}
	if _, err := manager.Canary(context.Background(), "fixture", good.Revision, source); err != nil {
		t.Fatal(err)
	}
	if err := manager.Activate("fixture", good.Revision); err != nil {
		t.Fatal(err)
	}
	badBin := compileIsolatedFixture(t, dir, "bad-transform", canaryFailTransform)
	badBytes, err := os.ReadFile(badBin)
	if err != nil {
		t.Fatal(err)
	}
	bad, err := manager.Stage(manifest, badBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Validate(context.Background(), "fixture", bad.Revision, cases); err != nil {
		t.Fatal(err)
	}
	wide := trustedFixtureReader{fixtureReader{read: func(_ context.Context, target Target) (Report, error) {
		r := newReport(target, "builtin:fixture")
		r.Title = "https://github.com/Git-on-my-level/agent-nexus"
		r.URL = "https://github.com/Git-on-my-level/agent-nexus/issues/208"
		r.Evidence = []Evidence{{Kind: "issue", Reference: r.URL, Knowledge: "reported"}}
		return finishReport(r)
	}}}
	if _, err := manager.Canary(context.Background(), "fixture", bad.Revision, wide); err == nil {
		t.Fatal("failed canary activated a path")
	}
	if err := manager.Activate("fixture", bad.Revision); err == nil {
		t.Fatal("revision that failed canary was activated")
	}
	state, err := manager.Status("fixture")
	if err != nil {
		t.Fatal(err)
	}
	if state.Active != good.Revision || state.Versions[bad.Revision].State == "active" {
		t.Fatalf("failed canary mutated active revision: %+v", state)
	}
}

func TestRealPolicyViolationKeepsLastGood(t *testing.T) {
	_ = isolationRunnerOrSkip(t)
	dir := isolationWorkDir(t)
	policy := isolationTestPolicy()
	manager, err := NewJITManager(filepath.Join(dir, "managed"), policy)
	if err != nil {
		t.Fatal(err)
	}
	bin := compileIsolatedFixture(t, dir, "policy-trip", policyTripTransform)
	artifact, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	version, err := manager.Stage(Manifest{AdapterID: "fixture", Target: fixtureTarget(), Limits: policy.Limits}, artifact)
	if err != nil {
		t.Fatal(err)
	}
	cases := []ValidationCase{{Name: "happy", Input: []byte(`{"title":"ok"}`), WantValid: true}, {Name: "rejected", Input: []byte(`{"reject":true}`), WantValid: false}}
	if err := manager.Validate(context.Background(), "fixture", version.Revision, cases); err != nil {
		t.Fatal(err)
	}
	clean := trustedFixtureReader{fixtureReader{read: func(_ context.Context, target Target) (Report, error) {
		return finishReport(newReport(target, "builtin:fixture"))
	}}}
	if _, err := manager.Canary(context.Background(), "fixture", version.Revision, clean); err != nil {
		t.Fatal(err)
	}
	if err := manager.Activate("fixture", version.Revision); err != nil {
		t.Fatal(err)
	}
	scheduler, err := NewScheduler(1)
	if err != nil {
		t.Fatal(err)
	}
	reader := jitBoundTestReader{manager: manager, id: "fixture", source: clean}
	refresh := fixturePolicy()
	refresh.Timeout = 20 * time.Second
	first, err := scheduler.Refresh(context.Background(), "fixture", refresh, reader, fixtureTarget(), true)
	if err != nil || first.Health.LastGood == nil {
		t.Fatalf("good read: %v %+v", err, first)
	}
	observed := first.Health.LastGood.ObservedAt
	trip := trustedFixtureReader{fixtureReader{read: func(_ context.Context, target Target) (Report, error) {
		r := newReport(target, "builtin:fixture")
		r.Facts["trip"] = true
		return finishReport(r)
	}}}
	reader.source = trip
	second, err := scheduler.Refresh(context.Background(), "fixture", refresh, reader, fixtureTarget(), true)
	if err == nil {
		t.Fatal("policy-violating read succeeded")
	}
	state, _ := manager.Status("fixture")
	if state.Active != "" || state.Versions[version.Revision].State != "suspended" {
		t.Fatalf("policy violation did not suspend: %+v", state)
	}
	if second.Health.LastGood == nil || second.Health.LastGood.ObservedAt != observed {
		t.Fatalf("last good observation was dropped: %+v", second.Health)
	}
	if second.Health.LastGood.Facts["generated_findings"] == nil {
		t.Fatal("last good lost generated findings")
	}
	age := scheduler.now().UTC().Sub(second.Health.LastGood.ObservedAt)
	if age < 0 {
		t.Fatalf("last good age is negative: %s", age)
	}
	t.Logf("last good retained age=%s observed_at=%s", age, second.Health.LastGood.ObservedAt)
}

type jitBoundTestReader struct {
	manager *JITManager
	id      string
	source  Reader
}

func (r jitBoundTestReader) Capabilities() Capabilities {
	return Capabilities{ReadOne: true, TrustedBuiltin: true}
}
func (r jitBoundTestReader) Read(ctx context.Context, target Target) (Report, error) {
	_ = target
	return r.manager.Read(ctx, r.id, r.source)
}

// A harmless fixture outside every allowlisted root must be unreadable, even
// under /private/tmp which was exposed by the old file-read* plus denylist.
func TestSeatbeltDeniesUnlistedReadAndAllowsScratch(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Seatbelt is macOS-only")
	}
	runner := isolationRunnerOrSkip(t)
	dir := isolationWorkDir(t)
	secret := filepath.Join(dir, "outside-reader")
	if err := os.WriteFile(secret, []byte("harmless denial fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	binary := compileIsolatedFixture(t, dir, "allowlist-reader", fmt.Sprintf(`
#include <fcntl.h>
#include <stdio.h>
#include <unistd.h>
int main(void) {
 if (open(%q,O_RDONLY)>=0) return 10;
 int fd=open("scratch-test",O_RDWR|O_CREAT,0600);
 if(fd<0 || write(fd,"x",1)!=1) return 11;
 close(fd);
 fd=open("scratch-test",O_RDONLY);
 if(fd<0) return 12;
 close(fd);puts("{}");return 0;
}
`, secret))
	if _, err := runner.Run(context.Background(), binary, []byte("{}"), isolationTestPolicy().Limits); err != nil {
		t.Fatal(err)
	}
}

// Use only a controlled same-uid fixture process, with a synthetic environment.
func TestSeatbeltSysctlAllowlistAndProcessArgumentsDenied(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Seatbelt is macOS-only")
	}
	runner := isolationRunnerOrSkip(t)
	child := exec.Command("/bin/sleep", "30")
	child.Env = []string{"ANX_SYNTHETIC_FIXTURE=not-a-secret"}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	binary := compileIsolatedFixture(t, isolationWorkDir(t), "sysctl-reader", `
#include <sys/types.h>
#include <sys/sysctl.h>
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
int main(int argc, char **argv) {
 int pid;
 if(scanf("%d", &pid)!=1) return 9;
 char buf[65536]; size_t n=sizeof(buf);
 int mib[3]={CTL_KERN,KERN_PROCARGS2,pid};
 int result=sysctl(mib,3,buf,&n,NULL,0);
 // Prove the controlled process is readable without the profile.
 if(argc>1) return result==0 ? 0 : 10;
 if(result==0 || (errno!=EPERM && errno!=EACCES)) return 11;
 const char *names[]={"hw.ncpu","hw.pagesize","kern.osrelease","kern.version","hw.memsize"};
 for(int i=0;i<5;i++) {n=sizeof(buf);if(sysctlbyname(names[i],buf,&n,NULL,0)!=0) return 20+i;}
 puts("{}");return 0;
}`)
	input := []byte(fmt.Sprint(child.Process.Pid))
	baseline := exec.Command(binary, "baseline")
	baseline.Stdin = strings.NewReader(string(input))
	if err := baseline.Run(); err != nil {
		t.Fatalf("controlled process baseline: %v", err)
	}
	if _, err := runner.Run(context.Background(), binary, input, isolationTestPolicy().Limits); err != nil {
		t.Fatal(err)
	}
}

func TestSeatbeltGoRuntimeWithNamedSysctls(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Seatbelt is macOS-only")
	}
	runner := isolationRunnerOrSkip(t)
	dir := isolationWorkDir(t)
	source := filepath.Join(dir, "runtime-probe.go")
	if err := os.WriteFile(source, []byte(`package main
import ("fmt"; "runtime"; "syscall")
func main(){
 if runtime.NumCPU()<1 || syscall.Getpagesize()<1 {panic("runtime sizing")}
 ch:=make(chan int);go func(){ch<-1}();<-ch
 fmt.Println("{}")
}`), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "runtime-probe")
	build := exec.Command("go", "build", "-o", binary, source)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Go probe: %v %s", err, out)
	}
	if _, err := runner.Run(context.Background(), binary, []byte("{}"), isolationTestPolicy().Limits); err != nil {
		t.Fatal(err)
	}
}
