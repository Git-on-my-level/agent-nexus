package observation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

const forgeFixtureSHA = "aa11bb33cc55dd77ee99aa11bb33cc55dd77ee99"

func forgeTarget(source string) Target {
	return Target{WorkspaceID: "w", ConnectionID: "c", Source: source, Kind: "pull_request", Repository: "acme/widgets", NativeID: "2"}
}

// mustForgeFixture returns only the config; most callers need forgeBase too.
func mustForgeFixture(t *testing.T, merged bool) HTTPConfig {
	cfg, _, _ := forgeFixture(t, merged, nil)
	return cfg
}

// forgeFixture serves the Gitea-compatible v1 API for one pull request on the
// test server's own origin, so returned identity URLs pass the reader's
// registered-origin validation. The merged flag selects the merged/open
// fixture; unexpected endpoints fail.
func forgeFixture(t *testing.T, merged bool, check func(r *http.Request)) (HTTPConfig, string, *ForgejoReader) {
	t.Helper()
	origin := ""
	identity := func(path string) string { return origin + path }
	cfg, server := testHTTPConfig(t, func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		switch r.URL.Path {
		case "/api/v1/repos/acme/widgets/issues/2":
			fmt.Fprintf(w, `{"number":2,"title":"Widgets gate","state":"open","updated_at":"2026-10-13T09:00:00Z","html_url":"%s","comments":1,"assignee":{"login":"octo"},"pull_request":{"merged":%t}}`, identity("/acme/widgets/pulls/2"), merged)
		case "/api/v1/repos/acme/widgets/pulls/2":
			mergedAt := ""
			if merged {
				mergedAt = `,"merged_at":"2026-10-13T10:00:00Z","merge_commit_sha":"ff00112233445566778899aabbccddeeff0011223"`
			}
			fmt.Fprintf(w, `{"number":2,"title":"Widgets gate","state":"%s","html_url":"%s","merged":%t%s,"draft":false,"mergeable":true,"head":{"ref":"gate","sha":"%s"}}`, map[bool]string{true: "closed", false: "open"}[merged], identity("/acme/widgets/pulls/2"), merged, mergedAt, forgeFixtureSHA)
		case "/api/v1/repos/acme/widgets/pulls/2/reviews":
			fmt.Fprint(w, `[{"id":1,"state":"APPROVED","official":true,"commit_id":"`+forgeFixtureSHA+`","user":{"login":"octo"},"pull_request_url":"`+identity("/acme/widgets/pulls/2")+`"}]`)
		case "/api/v1/repos/acme/widgets/commits/" + forgeFixtureSHA + "/status":
			fmt.Fprint(w, `{"sha":"`+forgeFixtureSHA+`","state":"success","total_count":1,"statuses":[{"id":1,"status":"success","context":"ci/build","description":"build ok","target_url":"`+identity("/acme/widgets/actions/runs/9")+`"}]}`)
		case "/api/v1/repos/acme/widgets/issues/2/comments":
			fmt.Fprint(w, `[{"id":5,"html_url":"`+identity("/acme/widgets/pulls/2#issuecomment-5")+`","updated_at":"2026-10-13T09:30:00Z","user":{"login":"octo"}}]`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	origin = server.URL
	reader := forgeReader(t, cfg, nil)
	return cfg, origin, reader
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
		_, origin, reader := forgeFixture(t, merged, func(r *http.Request) {
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
		if report.Title != "Widgets gate" || report.Facts["owner"] != "octo" || report.Facts["head_sha"] != forgeFixtureSHA {
			t.Fatalf("facts %+v", report.Facts)
		}
		if report.URL != origin+"/acme/widgets/pulls/2" {
			t.Fatalf("report URL %q", report.URL)
		}
		references := map[string]string{}
		for _, e := range report.Evidence {
			references[e.Kind] = e.Reference
		}
		if references["pull_request"] != origin+"/acme/widgets/pulls/2" {
			t.Fatalf("PR evidence missing: %+v", report.Evidence)
		}
		if references["review"] == "" || references["check_run"] != origin+"/acme/widgets/actions/runs/9" {
			t.Fatalf("review/check evidence missing: %+v", report.Evidence)
		}
		if report.Coverage.Pages != 4 || !report.Coverage.Complete {
			t.Fatalf("coverage %+v", report.Coverage)
		}
		if check, _ := report.Facts["status_check"].(map[string]any); check["state"] != "success" || check["total_count"] != float64(1) {
			t.Fatalf("combined status %+v", report.Facts["status_check"])
		}
		if report.SourceActivityAt == nil || report.SourceUpdatedAt == nil {
			t.Fatal("source timestamps missing")
		}
	}
}

func TestForgejoIssueCommentsAndGiteaAlias(t *testing.T) {
	origin := ""
	cfg, server := testHTTPConfig(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/acme/widgets/issues/7":
			fmt.Fprintf(w, `{"number":7,"title":"Follow-up","state":"open","updated_at":"2026-10-13T09:00:00Z","html_url":"%s/acme/widgets/issues/7","comments":2}`, origin)
		case "/api/v1/repos/acme/widgets/issues/7/comments":
			// The second comment points somewhere else entirely: it is not
			// evidence of this issue and must be dropped with a limitation.
			fmt.Fprintf(w, `[{"id":3,"html_url":"%s/acme/widgets/issues/7#issuecomment-3","updated_at":"2026-10-13T09:30:00Z"},{"id":4,"html_url":"https://unregistered.example.test/acme/widgets/issues/7#issuecomment-4","updated_at":"2026-10-13T09:31:00Z"}]`, origin)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	origin = server.URL
	reader, err := NewForgejoReader(cfg, "gitea")
	if err != nil {
		t.Fatal(err)
	}
	if caps := reader.Capabilities(); caps.Source != "gitea" || !caps.ReadOne || !caps.TrustedBuiltin {
		t.Fatalf("capabilities %+v", caps)
	}
	report, err := reader.Read(context.Background(), Target{WorkspaceID: "w", ConnectionID: "c", Source: "gitea", Kind: "issue", Repository: "acme/widgets", NativeID: "7"})
	if err != nil {
		t.Fatal(err)
	}
	if report.NativeStatus != "open" || len(report.Evidence) != 2 {
		t.Fatalf("report %+v", report)
	}
	if report.Evidence[1].Kind != "comment" || report.Evidence[1].Reference != origin+"/acme/widgets/issues/7#issuecomment-3" {
		t.Fatalf("comment evidence %+v", report.Evidence)
	}
	if !report.Coverage.Complete || len(report.Coverage.Limitations) != 1 || !strings.Contains(report.Coverage.Limitations[0], "comment references outside this issue omitted") {
		t.Fatalf("dropped foreign comment not reported: %+v", report.Coverage)
	}
}

// The reader rejects returned identity URLs that do not name the registered
// origin, repository and requested number, and compares the issue number
// exactly: a nearby integer that survives float64 rounding must not pass.
func TestForgejoRejectsForeignIdentityURLs(t *testing.T) {
	for _, tt := range []struct {
		name, issue, target string
	}{
		{"foreign origin", `{"number":2,"title":"X","state":"open","html_url":"https://unregistered.example.test/acme/widgets/pulls/2"}`, "2"},
		{"wrong number in URL", `{"number":2,"title":"X","state":"open","html_url":"%BASE%/acme/widgets/pulls/3"}`, "2"},
		{"wrong repository in URL", `{"number":2,"title":"X","state":"open","html_url":"%BASE%/acme/other/pulls/2"}`, "2"},
		{"non-identity path", `{"number":2,"title":"X","state":"open","html_url":"%BASE%/acme/widgets/wiki/2"}`, "2"},
		{"nearby float64 number", `{"number":30000000000000001,"title":"X","state":"open","html_url":"%BASE%/acme/widgets/pulls/30000000000000001"}`, "30000000000000000"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			origin := ""
			cfg, server := testHTTPConfig(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, strings.ReplaceAll(tt.issue, "%BASE%", origin))
			})
			origin = server.URL
			reader, err := NewForgejoReader(cfg, "forgejo")
			if err != nil {
				t.Fatal(err)
			}
			_, err = reader.Read(context.Background(), Target{WorkspaceID: "w", ConnectionID: "c", Source: "forgejo", Kind: "pull_request", Repository: "acme/widgets", NativeID: tt.target})
			var re *ReadError
			if !errors.As(err, &re) || re.Kind != ErrInvalidOutput {
				t.Fatalf("wrong error: %v", err)
			}
		})
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
	if _, err := reader.Read(context.Background(), Target{WorkspaceID: "w", ConnectionID: "other", Source: "forgejo", Kind: "pull_request", Repository: "acme/widgets", NativeID: "2"}); err == nil {
		t.Fatal("accepted foreign connection")
	}
	for _, mutate := range []func(*Target){
		func(t *Target) { t.Kind = "repository" },
		func(t *Target) { t.NativeID = "0" },
		func(t *Target) { t.NativeID = "nope" },
		func(t *Target) { t.Repository = "acme" },
	} {
		broken := target
		mutate(&broken)
		if _, err := reader.Read(context.Background(), broken); err == nil {
			t.Fatal("accepted invalid target")
		}
	}
	// A response whose number differs from the target is rejected.
	cfg, _ := testHTTPConfig(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"number":3,"title":"Other","state":"open","html_url":"https://forge.example.test/acme/widgets/pulls/3"}`)
	})
	mismatch, err := NewForgejoReader(cfg, "forgejo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = mismatch.Read(context.Background(), target); err == nil {
		t.Fatal("accepted mismatched issue identity")
	}
	// Not a pull request: the identity URL is valid for this resource, so the
	// missing pull_request payload is what fails the read.
	origin2 := ""
	cfg2, server2 := testHTTPConfig(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"number":2,"title":"Plain issue","state":"open","html_url":"%s/acme/widgets/issues/2"}`, origin2)
	})
	origin2 = server2.URL
	plain, err := NewForgejoReader(cfg2, "forgejo")
	if err != nil {
		t.Fatal(err)
	}
	_, err = plain.Read(context.Background(), target)
	var re *ReadError
	if !errors.As(err, &re) || re.Kind != ErrInvalidOutput || !strings.Contains(err.Error(), "not a pull request") {
		t.Fatalf("wrong error for non-PR target: %v", err)
	}
	// Reader revision and report shape stay canonical.
	origin3 := ""
	cfg3, server3 := testHTTPConfig(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"number":2,"title":"Widgets gate","state":"open","updated_at":"2026-10-13T09:00:00Z","html_url":"%s/acme/widgets/pulls/2","pull_request":{},"comments":0}`, origin3)
	})
	origin3 = server3.URL
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
