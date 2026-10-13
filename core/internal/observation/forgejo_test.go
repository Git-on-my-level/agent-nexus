package observation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

const forgeFixtureSHA = "aa11bb33cc55dd77ee99aa11bb33cc55dd77ee99"

func forgeTarget(source string) Target {
	return Target{WorkspaceID: "w", ConnectionID: "c", Source: source, Kind: "pull_request", Repository: "hermes/hub", NativeID: "2"}
}

// forgeFixture serves the Gitea-compatible v1 API for one pull request. The
// merged flag selects the merged/open fixture; unexpected endpoints fail.
func mustForgeFixture(t *testing.T, merged bool) HTTPConfig {
	cfg, _ := forgeFixture(t, merged, nil)
	return cfg
}

func forgeFixture(t *testing.T, merged bool, check func(r *http.Request)) (HTTPConfig, *ForgejoReader) {
	t.Helper()
	cfg, _ := testHTTPConfig(t, func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		switch r.URL.Path {
		case "/api/v1/repos/hermes/hub/issues/2":
			fmt.Fprintf(w, `{"number":2,"title":"Hub gate","state":"open","updated_at":"2026-10-13T09:00:00Z","html_url":"https://forge.example.test/hermes/hub/pulls/2","comments":1,"assignee":{"login":"david"},"pull_request":{"merged":%t}}`, merged)
		case "/api/v1/repos/hermes/hub/pulls/2":
			mergedAt := ""
			if merged {
				mergedAt = `,"merged_at":"2026-10-13T10:00:00Z","merge_commit_sha":"ff00112233445566778899aabbccddeeff0011223"`
			}
			fmt.Fprintf(w, `{"number":2,"title":"Hub gate","state":"%s","html_url":"https://forge.example.test/hermes/hub/pulls/2","merged":%t%s,"draft":false,"mergeable":true,"head":{"ref":"gate","sha":"%s"}}`, map[bool]string{true: "closed", false: "open"}[merged], merged, mergedAt, forgeFixtureSHA)
		case "/api/v1/repos/hermes/hub/pulls/2/reviews":
			fmt.Fprint(w, `[{"id":1,"state":"APPROVED","official":true,"commit_id":"`+forgeFixtureSHA+`","user":{"login":"david"},"pull_request_url":"https://forge.example.test/hermes/hub/pulls/2"}]`)
		case "/api/v1/repos/hermes/hub/commits/" + forgeFixtureSHA + "/status":
			fmt.Fprint(w, `{"sha":"`+forgeFixtureSHA+`","state":"success","total_count":1,"statuses":[{"id":1,"status":"success","context":"ci/build","description":"build ok","target_url":"https://forge.example.test/hermes/hub/actions/runs/9"}]}`)
		case "/api/v1/repos/hermes/hub/issues/2/comments":
			fmt.Fprint(w, `[{"id":5,"html_url":"https://forge.example.test/hermes/hub/pulls/2#issuecomment-5","updated_at":"2026-10-13T09:30:00Z","user":{"login":"david"}}]`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	reader, err := NewForgejoReader(cfg, "forgejo")
	if err != nil {
		t.Fatal(err)
	}
	return cfg, reader
}

func forgeReader(t *testing.T, cfg HTTPConfig, check func(r *http.Request)) *ForgejoReader {
	t.Helper()
	cfg.MaxPages = 6
	cfg.CredentialHandle = "forge-read-only"
	cfg.ResolveCredential = func(_ context.Context, handle string) (string, error) {
		if handle != "forge-read-only" {
			return "", errors.New("unapproved handle")
		}
		return "fixture-token", nil
	}
	reader, err := NewForgejoReader(cfg, "forgejo")
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

func TestForgejoPullRequestOpenAndMerged(t *testing.T) {
	for _, merged := range []bool{false, true} {
		reader := forgeReader(t, mustForgeFixture(t, merged), func(r *http.Request) {
			if token := r.Header.Get("Authorization"); token != "token fixture-token" {
				t.Errorf("forgejo credential scheme lost: %q", token)
			}
		})
		report, err := reader.Read(context.Background(), forgeTarget("forgejo"))
		if err != nil {
			t.Fatal(err)
		}
		wantStatus := "open"
		if merged {
			wantStatus = "merged"
		}
		if report.NativeStatus != wantStatus || report.Facts["phase"] != phase(wantStatus) {
			t.Fatalf("status %q facts %+v", report.NativeStatus, report.Facts)
		}
		if report.Title != "Hub gate" || report.Facts["owner"] != "david" || report.Facts["head_sha"] != forgeFixtureSHA {
			t.Fatalf("facts %+v", report.Facts)
		}
		if check, _ := report.Facts["status_check"].(map[string]any); check["state"] != "success" || check["total_count"] != float64(1) {
			t.Fatalf("combined status %+v", report.Facts["status_check"])
		}
		if report.URL != "https://forge.example.test/hermes/hub/pulls/2" {
			t.Fatalf("report URL %q", report.URL)
		}
		references := map[string]string{}
		for _, e := range report.Evidence {
			references[e.Kind] = e.Reference
		}
		if references["pull_request"] != "https://forge.example.test/hermes/hub/pulls/2" {
			t.Fatalf("PR evidence missing: %+v", report.Evidence)
		}
		if references["review"] == "" || references["check_run"] != "https://forge.example.test/hermes/hub/actions/runs/9" {
			t.Fatalf("review/check evidence missing: %+v", report.Evidence)
		}
		if report.Coverage.Pages != 4 || !report.Coverage.Complete {
			t.Fatalf("coverage %+v", report.Coverage)
		}
		if report.SourceActivityAt == nil || report.SourceUpdatedAt == nil {
			t.Fatal("source timestamps missing")
		}
	}
}

func TestForgejoIssueCommentsAndGiteaAlias(t *testing.T) {
	cfg, _ := testHTTPConfig(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/hermes/hub/issues/7":
			fmt.Fprint(w, `{"number":7,"title":"Follow-up","state":"open","updated_at":"2026-10-13T09:00:00Z","html_url":"https://forge.example.test/hermes/hub/issues/7","comments":1}`)
		case "/api/v1/repos/hermes/hub/issues/7/comments":
			fmt.Fprint(w, `[{"id":3,"html_url":"https://forge.example.test/hermes/hub/issues/7#issuecomment-3","updated_at":"2026-10-13T09:30:00Z"}]`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	reader, err := NewForgejoReader(cfg, "gitea")
	if err != nil {
		t.Fatal(err)
	}
	if caps := reader.Capabilities(); caps.Source != "gitea" || !caps.ReadOne || !caps.TrustedBuiltin {
		t.Fatalf("capabilities %+v", caps)
	}
	report, err := reader.Read(context.Background(), Target{WorkspaceID: "w", ConnectionID: "c", Source: "gitea", Kind: "issue", Repository: "hermes/hub", NativeID: "7"})
	if err != nil {
		t.Fatal(err)
	}
	if report.NativeStatus != "open" || len(report.Evidence) != 2 {
		t.Fatalf("report %+v", report)
	}
	if report.Evidence[1].Kind != "comment" || report.Evidence[1].Reference != "https://forge.example.test/hermes/hub/issues/7#issuecomment-3" {
		t.Fatalf("comment evidence %+v", report.Evidence)
	}
}

func TestForgejoFailsClosed(t *testing.T) {
	if _, err := NewForgejoReader(HTTPConfig{BaseURL: "https://forge.example.test"}, "github"); err == nil {
		t.Fatal("accepted foreign source kind")
	}
	if _, err := NewForgejoReader(HTTPConfig{}, "forgejo"); err == nil {
		t.Fatal("accepted missing base URL")
	}
	reader := forgeReader(t, mustForgeFixture(t, false), nil)
	target := forgeTarget("forgejo")
	if _, err := reader.Read(context.Background(), Target{WorkspaceID: "w", ConnectionID: "other", Source: "forgejo", Kind: "pull_request", Repository: "hermes/hub", NativeID: "2"}); err == nil {
		t.Fatal("accepted foreign connection")
	}
	for _, mutate := range []func(*Target){
		func(t *Target) { t.Kind = "repository" },
		func(t *Target) { t.NativeID = "0" },
		func(t *Target) { t.NativeID = "nope" },
		func(t *Target) { t.Repository = "hermes" },
	} {
		broken := target
		mutate(&broken)
		if _, err := reader.Read(context.Background(), broken); err == nil {
			t.Fatal("accepted invalid target")
		}
	}
	// A response whose number differs from the target is rejected.
	cfg, _ := testHTTPConfig(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"number":3,"title":"Other","state":"open","html_url":"https://forge.example.test/hermes/hub/pulls/3"}`)
	})
	mismatch, err := NewForgejoReader(cfg, "forgejo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = mismatch.Read(context.Background(), target); err == nil {
		t.Fatal("accepted mismatched issue identity")
	}
	// Not a pull request.
	cfg2, _ := testHTTPConfig(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"number":2,"title":"Plain issue","state":"open","html_url":"https://forge.example.test/hermes/hub/issues/2"}`)
	})
	plain, err := NewForgejoReader(cfg2, "forgejo")
	if err != nil {
		t.Fatal(err)
	}
	_, err = plain.Read(context.Background(), target)
	var re *ReadError
	if !errors.As(err, &re) || re.Kind != ErrInvalidOutput {
		t.Fatalf("wrong error for non-PR target: %v", err)
	}
	// Reader revision and report shape stay canonical.
	cfg3, _ := testHTTPConfig(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"number":2,"title":"Hub gate","state":"open","updated_at":"2026-10-13T09:00:00Z","html_url":"https://forge.example.test/hermes/hub/pulls/2","pull_request":{},"comments":0}`)
	})
	minimal, err := NewForgejoReader(cfg3, "forgejo")
	if err != nil {
		t.Fatal(err)
	}
	report, err := minimal.Read(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if report.ReaderID != "builtin:forgejo" || report.ReaderRevision != ReaderRevision || report.Knowledge != "reported" {
		t.Fatalf("report identity %+v", report)
	}
	if report.ObservedAt.After(time.Now().Add(time.Minute)) {
		t.Fatal("observed clock drifted")
	}
}
