package app

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-nexus-cli/internal/errnorm"
)

func TestUpdateRejectsMatchedAssetsFromArbitraryRedirectServer(t *testing.T) {
	for _, downgrade := range []bool{false, true} {
		t.Run(map[bool]string{false: "other_https_host", true: "https_downgrade"}[downgrade], func(t *testing.T) {
			a, cfg, path := managedUpdateFixture(t)
			oldBase := updateReleaseBaseURL
			t.Cleanup(func() { updateReleaseBaseURL = oldBase })
			archive, _ := updateArchiveName("v0.12.11")
			content := buildReleaseArchiveForTest(t, archive, []byte("attacker executable"))
			visits := 0
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				visits++
				if strings.HasSuffix(r.URL.Path, "checksums.txt") {
					_, _ = w.Write([]byte(sha256HexForTest(content) + "  " + archive))
				} else {
					_, _ = w.Write(content)
				}
			})
			target := newUpdateTLSServer(t, handler)
			defer target.Close()
			targetURL := target.URL
			if downgrade {
				plain := httptest.NewServer(handler)
				defer plain.Close()
				targetURL = plain.URL
			}
			source := newUpdateTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, targetURL+r.URL.Path, 302) }))
			defer source.Close()
			updateReleaseBaseURL = source.URL + "/releases"
			_, err := a.runUpdate(context.Background(), []string{"--version", "v0.12.11"}, cfg)
			if err == nil {
				t.Fatal("redirected matching archive/checksum pair installed")
			}
			if visits != 0 {
				t.Fatalf("unapproved target contacted %d times", visits)
			}
			b, _ := os.ReadFile(path)
			if string(b) != "original" {
				t.Fatal("healthy binary replaced")
			}
		})
	}
}

func TestReleaseDiscoveryRejectsAPIRedirectToAnotherRepository(t *testing.T) {
	oldAPI := updateReleaseAPIURL
	t.Cleanup(func() { updateReleaseAPIURL = oldAPI })
	source := newUpdateTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/other/repo/releases/latest" {
			t.Error("followed API repository redirect")
		}
		http.Redirect(w, r, "/repos/other/repo/releases/latest", 302)
	}))
	defer source.Close()
	updateReleaseAPIURL = source.URL + "/repos/Git-on-my-level/agent-nexus/releases/latest"
	if _, err := resolveLatestReleaseTag(context.Background(), time.Second); err == nil {
		t.Fatal("API repository redirect accepted")
	}
}

func TestAssetRedirectAllowlistRequiresHTTPSAndOriginalRepositoryPath(t *testing.T) {
	entry := "https://github.com/Git-on-my-level/agent-nexus/releases/download/v0.12.11/anx.tar.gz"
	client, err := updateHTTPClient(time.Second, "asset", entry)
	if err != nil {
		t.Fatal(err)
	}
	for target, accepted := range map[string]bool{
		"https://release-assets.githubusercontent.com/asset":                  true,
		"https://objects.githubusercontent.com/asset":                         true,
		"https://github-releases.githubusercontent.com/asset":                 true,
		"http://release-assets.githubusercontent.com/asset":                   false,
		"https://release-assets.githubusercontent.com.evil.example/asset":     false,
		"https://release-assets.githubusercontent.com:8443/asset":             false,
		"https://user@release-assets.githubusercontent.com/asset":             false,
		"https://github.com/other/repo/releases/download/v0.12.11/anx.tar.gz": false,
	} {
		u, _ := url.Parse(target)
		err := client.CheckRedirect(&http.Request{URL: u}, []*http.Request{{}})
		if (err == nil) != accepted {
			t.Errorf("%s accepted=%t err=%v", target, accepted, err)
		}
	}
}

func TestParsedDryRunsAndDiagnosticsNeverTriggerUpdates(t *testing.T) {
	a, cfg, _ := managedUpdateFixture(t)
	a.startUpdateWorker = func(string, string) error { t.Fatal("dry run started update worker"); return nil }
	home, _ := a.UserHomeDir()
	writeDerivedAgentFixture(t, home, "agent-update-preview", `{"agent":"agent-update-preview","actor_id":"actor_preview","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	for _, value := range []string{"1", "t", "TRUE", "yes", "on", "y"} {
		args := []string{"cards", "archive", "card:test", "--dry-run=" + value}
		if updateInvocationEligible("cards archive", args) {
			t.Errorf("eligible dry-run=%s", value)
		}
		preview := dryRunResult("cards archive", "cards.archive", nil, nil, nil)
		a.maybeScheduleUpdate("cards archive", args, cfg, preview)
		// Exercise the actual parser and post-command scheduler, with a managed
		// installation and unreachable API: a preview must remain offline.
		var stdout, stderr bytes.Buffer
		a.Stdout, a.Stderr = &stdout, &stderr
		parsedArgs := []string{"--json", "--base-url", "http://127.0.0.1:9", "--as", "agent-update-preview", "cards", "archive", "card-preview", "--dry-run=" + value}
		if code := a.Run(parsedArgs); code != 0 {
			t.Fatalf("dry-run=%s code=%d stdout=%s stderr=%s", value, code, stdout.String(), stderr.String())
		}
		payload := assertEnvelopeOK(t, stdout.String())
		if !asBool(asMap(payload["result"])["dry_run"]) {
			t.Fatalf("parsed dry-run=%s did not return a preview: %s", value, stdout.String())
		}
	}
	for _, command := range []string{"bridge doctor", "doctor", "host status", "orient"} {
		if updateInvocationEligible(command, strings.Fields(command)) {
			t.Errorf("eligible diagnostic=%s", command)
		}
	}
	// Repeated flag values follow the parser, and an actual preview result wins
	// even if an argv projection omits flags entirely.
	if updateInvocationEligible("cards archive", []string{"--dry-run=off", "--dry-run=on"}) {
		t.Fatal("final true flag ignored")
	}
	if !updateInvocationEligible("cards archive", []string{"--dry-run=on", "--dry-run=off"}) {
		t.Fatal("final false flag ignored")
	}
	if updateInvocationEligible("cards archive", nil, dryRunResult("cards archive", "cards.archive", nil, nil, nil)) {
		t.Fatal("parsed preview ignored")
	}
}

func TestUpdateCrashBeforeReceiptPersistsBackupAndRecovers(t *testing.T) {
	a, cfg, path := managedUpdateFixture(t)
	var old updateInstallRecord
	_ = readUpdateJSON(installRecordPath(path), &old)
	originalRename := updateRename
	t.Cleanup(func() { updateRename = originalRename })
	updateRename = func(from, to string) error {
		if err := os.Rename(from, to); err != nil {
			return err
		}
		if to == path {
			panic("simulated crash after binary rename, before receipt commit")
		}
		return nil
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("crash point not reached")
			}
		}()
		_, _, _ = replaceManagedExecutable(context.Background(), path, []byte("replacement"), 0755, "v0.12.11", old)
	}()
	updateRename = originalRename
	tx, err := readUpdateTransaction(path)
	if err != nil {
		t.Fatal(err)
	}
	if tx.Phase != "prepared" {
		t.Fatalf("tx=%+v", tx)
	}
	if _, err := os.Stat(tx.BackupPath); err != nil {
		t.Fatal("no recoverable backup", err)
	}
	status, err := a.runUpdateStatus(cfg)
	if err != nil {
		t.Fatal(err)
	}
	state := asMap(status.Data)["state"].(updateState)
	if state.Rollback != "pending" || state.BackupPath == "" {
		t.Fatalf("offline status concealed transaction: %+v", state)
	}
	// Production entry point recovers BEFORE refusing a digest mismatch.
	result, err := a.runUpdate(context.Background(), []string{"now"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !asBool(asMap(result.Data)["managed"]) {
		t.Fatal("installation stayed unmanaged after recovery")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "original" {
		t.Fatal("original not restored")
	}
	var restored updateInstallRecord
	_ = readUpdateJSON(installRecordPath(path), &restored)
	if restored != old {
		t.Fatal("receipt not restored")
	}
	if _, err := os.Stat(updateTransactionPath(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("recovery journal not resolved")
	}
}

func TestUpdateRecoveryIsIdempotentAfterBackupRename(t *testing.T) {
	_, _, path := managedUpdateFixture(t)
	var old updateInstallRecord
	_ = readUpdateJSON(installRecordPath(path), &old)
	backup := filepath.Join(filepath.Dir(path), ".anx-rollback-test")
	_ = os.WriteFile(backup, []byte("original"), 0755)
	tx := updateTransaction{SchemaVersion: 1, Phase: "prepared", BackupPath: backup, OldSHA256: old.SHA256, OldRecord: old, OldRecordExists: true, NewRecord: updateInstallRecord{ManagedBy: "anx", Version: "v0.12.11", SHA256: sha256HexForTest([]byte("replacement")), InstalledAt: "2026-10-05T00:00:00Z"}}
	_ = writeUpdateJSON(updateTransactionPath(path), tx)
	// Recovery already restored the binary, then died before restoring receipt.
	_ = os.Remove(backup)
	_ = writeUpdateJSON(installRecordPath(path), updateInstallRecord{ManagedBy: "anx", Version: "wrong"})
	outcome, _, err := recoverUpdateTransaction(path)
	if err != nil || outcome != "succeeded" {
		t.Fatalf("%s %v", outcome, err)
	}
	var restored updateInstallRecord
	_ = readUpdateJSON(installRecordPath(path), &restored)
	if restored != old {
		t.Fatal("interrupted recovery lost receipt")
	}
}

func TestUpdateDurabilityJournalPrecedesBinaryReplacement(t *testing.T) {
	_, _, path := managedUpdateFixture(t)
	var record updateInstallRecord
	_ = readUpdateJSON(installRecordPath(path), &record)
	oldRename := updateRename
	t.Cleanup(func() { updateRename = oldRename })
	updateRename = func(from, to string) error {
		if to == path {
			tx, err := readUpdateTransaction(path)
			if err != nil {
				t.Fatal("binary renamed without journal", err)
			}
			old, err := os.ReadFile(tx.BackupPath)
			if err != nil || sha256HexForTest(old) != record.SHA256 {
				t.Fatal("journal has no durable original")
			}
			if info, err := os.Stat(from); err != nil || info.Mode().Perm() != 0755 {
				t.Fatal("candidate not ready before replacement")
			}
		}
		return os.Rename(from, to)
	}
	if _, _, err := replaceManagedExecutable(context.Background(), path, []byte("new"), 0755, "v0.12.11", record); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateTarSkippedContentCountsTowardAggregateBudget(t *testing.T) {
	var archive bytes.Buffer
	gzipWriter, err := gzip.NewWriterLevel(&archive, gzip.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(gzipWriter)
	chunk := make([]byte, 1<<20)
	for entry, size := range []int{64, 65} {
		_ = writer.WriteHeader(&tar.Header{Name: fmt.Sprintf("ignored-%d", entry), Mode: 0644, Size: int64(size) << 20, Typeflag: tar.TypeReg})
		for i := 0; i < size; i++ {
			if _, err := writer.Write(chunk); err != nil {
				t.Fatal(err)
			}
		}
	}
	_ = writer.WriteHeader(&tar.Header{Name: "anx", Mode: 0755, Size: 4, Typeflag: tar.TypeReg})
	_, _ = writer.Write([]byte("fake"))
	_ = writer.Close()
	_ = gzipWriter.Close()
	if archive.Len() > 1<<20 {
		t.Fatal("regression fixture did not compress sufficiently")
	}
	if _, _, err := extractReleaseBinaryContext(context.Background(), "anx.tar.gz", archive.Bytes()); err == nil {
		t.Fatal("129 MiB ignored entry accepted")
	}
}

func TestUpdateTarHiddenMetadataCountsTowardEntryLimit(t *testing.T) {
	var raw bytes.Buffer
	w := tar.NewWriter(&raw)
	_ = w.WriteHeader(&tar.Header{Name: "metadata", Mode: 0644, Size: 4, Typeflag: tar.TypeReg})
	_, _ = w.Write([]byte("anx\x00"))
	_ = w.Close()
	entry := append([]byte(nil), raw.Bytes()[:1024]...)
	entry[156] = tar.TypeGNULongName
	copy(entry[148:156], "        ")
	sum := 0
	for _, b := range entry[:512] {
		sum += int(b)
	}
	copy(entry[148:156], fmt.Sprintf("%06o\x00 ", sum))
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	_, _ = gz.Write(bytes.Repeat(entry, releaseEntryLimit+1))
	_, _ = gz.Write(raw.Bytes())
	_ = gz.Close()
	if _, _, err := extractReleaseBinary("anx.tar.gz", compressed.Bytes()); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("hidden metadata escaped entry limit: %v", err)
	}
}

func TestUpdateReleaseTagsCannotEscapeRepositoryAssetPath(t *testing.T) {
	for _, tag := range []string{"v0.12.11-../../other/repo", "v0.12.11?redirect=evil", "v0.12.11%2fother", "v0.12.11#fragment"} {
		if _, err := updateArchiveName(tag); err == nil {
			t.Errorf("unsafe tag accepted: %s", tag)
		}
	}
}

func TestUpdateArchiveEntryCountAndCancellation(t *testing.T) {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tarWriter := tar.NewWriter(gz)
	for i := 0; i < releaseEntryLimit+1; i++ {
		_ = tarWriter.WriteHeader(&tar.Header{Name: "ignored", Mode: 0644, Typeflag: tar.TypeReg})
	}
	_ = tarWriter.Close()
	_ = gz.Close()
	if _, _, err := extractReleaseBinaryContext(context.Background(), "anx.tar.gz", b.Bytes()); err == nil {
		t.Fatal("entry count limit ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := extractReleaseBinaryContext(ctx, "anx.tar.gz", b.Bytes()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
	budget := &releaseBudgetReader{ctx: ctx, reader: bytes.NewReader(make([]byte, 1024)), remaining: 1024}
	if _, err := io.Copy(io.Discard, budget); !errors.Is(err, context.Canceled) {
		t.Fatal("budget reader ignores cancellation")
	}
}

func TestUpdateDriftedSkillsRequireAttentionWithoutModifyingCopy(t *testing.T) {
	if updateGOOS == "windows" {
		t.Skip("Unix executable fixture")
	}
	path := filepath.Join(t.TempDir(), "anx")
	edited := filepath.Join(t.TempDir(), "SKILL.md")
	_ = os.WriteFile(edited, []byte("user edits"), 0600)
	script := "#!/bin/sh\nprintf '%s\\n' '{\"ok\":true,\"result\":{\"skills\":[{\"state\":\"drifted\"}]}}'\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	if err := updateSyncSkills(context.Background(), path, ""); err == nil {
		t.Fatal("drifted skill reported synchronized")
	}
	b, _ := os.ReadFile(edited)
	if string(b) != "user edits" {
		t.Fatal("edited copy changed")
	}
	a, cfg, target := managedUpdateFixture(t)
	savedSync := updateSyncSkills
	t.Cleanup(func() { updateSyncSkills = savedSync })
	updateSyncSkills = func(ctx context.Context, _ string, _ string) error { return savedSync(ctx, path, "") }
	oldBase := updateReleaseBaseURL
	t.Cleanup(func() { updateReleaseBaseURL = oldBase })
	archive, _ := updateArchiveName("v0.12.11")
	content := buildReleaseArchiveForTest(t, archive, []byte("new"))
	server := newUpdateTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "checksums.txt") {
			_, _ = w.Write([]byte(sha256HexForTest(content) + "  " + archive))
		} else {
			_, _ = w.Write(content)
		}
	}))
	defer server.Close()
	updateReleaseBaseURL = server.URL + "/releases"
	_, err := a.runUpdate(context.Background(), []string{"--version", "v0.12.11"}, cfg)
	if errnorm.Normalize(err).Code != "update_skills_failed" {
		t.Fatalf("err=%v", err)
	}
	dir, _ := a.updateDirectory(cfg)
	state, _ := readUpdateState(dir)
	if state.SkillsSynced || state.FailureStage != "skills_sync" {
		t.Fatalf("state=%+v", state)
	}
	updated, _ := os.ReadFile(target)
	if string(updated) != "new" {
		t.Fatal("healthy updated binary rolled back")
	}
}
