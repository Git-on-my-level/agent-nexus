package server

import (
	"agent-nexus-core/internal/testutil/perfguard"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	urlpkg "net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type baselineBudget struct {
	CachePhase     string `json:"cache_phase,omitempty"`
	Case           string `json:"case,omitempty"`
	MaxVMSteps     uint64 `json:"max_vm_steps"`
	Method         string `json:"method"`
	Path           string `json:"path"`
	Principal      string `json:"principal"`
	LatencyMS      int    `json:"latency_ms"`
	MaxQueries     int    `json:"max_queries"`
	MaxRows        int    `json:"max_rows"`
	Issue          string `json:"issue"`
	IssueURL       string `json:"issue_url"`
	Reason         string `json:"reason"`
	CoreSourceHash string `json:"core_source_sha256,omitempty"`
}

// Measured existing-main baselines must expire on runtime, dependency,
// fixture or gate changes. Exclude only release metadata and unrelated tests;
// include assets/module files and the local modules replaced by core/go.mod.
func performanceRuntimeSourceHash(root string) (string, error) {
	var paths []string
	for _, dir := range []string{"core", "tests/channels", "contracts"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() {
				if strings.HasPrefix(e.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if rel == "core/internal/buildinfo/version_generated.go" || rel == "core/internal/server/testdata/performance_budget_allowlist.json" {
				return nil
			}
			name := e.Name()
			fixtureHelper := rel == "core/internal/server/workspace_template_test.go" || rel == "core/internal/server/auth_integration_test.go" || rel == "core/internal/server/stream_privacy_integration_test.go" || rel == "core/internal/server/notifications_integration_test.go"
			if strings.HasSuffix(name, "_test.go") && !fixtureHelper && !strings.HasPrefix(name, "performance_") && name != "resource_access_performance_test.go" && name != "resource_access_prepare_test.go" {
				return nil
			}
			switch filepath.Ext(name) {
			case ".go", ".mod", ".sum", ".json", ".yaml", ".yml", ".sql", ".html", ".js", ".css", ".tmpl":
				paths = append(paths, rel)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	for _, relative := range []string{"scripts/check-performance-shards.py", "scripts/tests/test_performance_shards.py", ".github/workflows/performance.yml"} {
		if _, err := os.Stat(filepath.Join(root, relative)); err == nil {
			paths = append(paths, relative)
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(h, "%s\x00", path)
		h.Write(sum[:])
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// Existing main hazards remain finite, per-route/principal baselines. New routes
// always inherit the standard budget. A baseline does not bypass privacy, status,
// stream sampling or the exact-shape plan gate.
func performanceBaselineBudgets(t *testing.T, routes []routeBudget) map[string]baselineBudget {
	t.Helper()
	raw, err := os.ReadFile("testdata/performance_budget_allowlist.json")
	if err != nil {
		t.Fatal(err)
	}
	var entries []baselineBudget
	if err = json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	out := map[string]baselineBudget{}
	sourceHash := ""
	registered := map[string]bool{}
	for _, b := range routes {
		registered[performanceCaseKey(b.Method, b.Path, b.Case)] = true
	}
	for _, e := range entries {
		key := performanceBaselineKey(e.Method, e.Path, e.Case, e.Principal, e.CachePhase)
		if e.CachePhase != "" && e.CachePhase != "first_read" && e.CachePhase != "post_invalidation" {
			t.Fatalf("invalid cache phase %q", e.CachePhase)
		}
		if !registered[performanceCaseKey(e.Method, e.Path, e.Case)] || (e.Principal != "authorized" && e.Principal != "unauthorized") || e.LatencyMS <= 0 || e.LatencyMS > 1800000 || e.MaxQueries <= 0 || e.MaxQueries > 100000 || e.MaxRows <= 0 || e.MaxRows > 500000 || !reviewedPerformanceException(e.Issue, e.IssueURL, e.Reason) || out[key].Path != "" {
			t.Fatalf("invalid, duplicate or stale performance baseline %s", key)
		}
		if err := validatePerformanceSourceHash(e.CoreSourceHash); err != nil {
			t.Fatalf("invalid source pin for %s: %v", key, err)
		}
		// Freshness belongs to the advisory scale tier. Ordinary CI validates
		// the inventory and mandatory pin format without requiring a scale run
		// and baseline renewal for every production change.
		if os.Getenv("ANX_PERFORMANCE_TEST") == "1" {
			if sourceHash == "" {
				sourceHash, err = performanceRuntimeSourceHash("../../..")
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := validatePerformanceSourcePin(e.CoreSourceHash, sourceHash); err != nil {
				if os.Getenv("ANX_PERFORMANCE_DIAGNOSTIC") == "1" {
					t.Errorf("diagnostic only: expired baseline %s: %v", key, err)
				} else {
					// Expired allowances cannot relax a route's standard budget. Keep
					// measuring the route so advisory CI reports actual regressions
					// instead of dying during setup without a measurement report.
					t.Errorf("runtime/dependency/fixture changed: source-pinned existing-main baseline expired for %s: %v; measuring with standard budget", key, err)
					continue
				}
			}
		}
		if e.MaxVMSteps == 0 || e.MaxVMSteps > 1000000000000 {
			if os.Getenv("ANX_PERFORMANCE_DIAGNOSTIC") == "1" {
				t.Errorf("diagnostic only: missing work baseline %s", key)
				e.MaxVMSteps = 50000
			} else {
				t.Fatalf("invalid/missing finite VM-work baseline %s", key)
			}
		}

		out[key] = e
	}
	return out
}

func TestPerformanceRuntimeSourcePin(t *testing.T) {
	// Serial: performance samples must not compete with parallel fixtures.
	root := t.TempDir()
	for _, dir := range []string{"core/internal/server", "core/internal/buildinfo", "tests/channels", "contracts/visualreport"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	hash := func() string {
		t.Helper()
		h, err := performanceRuntimeSourceHash(root)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	write("core/main.go", "runtime")
	before := hash()
	write("core/internal/buildinfo/version_generated.go", "release")
	write("core/internal/server/ordinary_test.go", "unrelated test")
	if hash() != before {
		t.Fatal("release metadata/unrelated tests expired runtime pin")
	}
	for _, path := range []string{"core/go.mod", "core/go.sum", "tests/channels/new.go", "contracts/visualreport/go.mod", "contracts/anx-schema.yaml", "core/internal/server/auth_integration_test.go", "core/internal/server/workspace_template_test.go", "core/internal/server/stream_privacy_integration_test.go", "core/internal/server/notifications_integration_test.go", "core/internal/server/performance_test.go", "core/internal/server/routes.json", "core/new.go", "scripts/check-performance-shards.py", "scripts/tests/test_performance_shards.py", ".github/workflows/performance.yml"} {
		before = hash()
		write(path, "changed runtime input")
		if hash() == before {
			t.Fatalf("%s did not expire runtime pin", path)
		}
	}
}

func reviewedPerformanceException(issue, link, reason string) bool {
	// Accept a portable tracker link without coupling the core to one tracker.
	u, err := urlpkg.Parse(link)
	return err == nil && u.IsAbs() && u.Host != "" && u.Path != "" && strings.TrimSpace(issue) != "" && len(strings.TrimSpace(reason)) >= 40
}

func performancePlanExceptions(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile("testdata/performance_plan_allowlist.json")
	if err != nil {
		t.Fatal(err)
	}
	var entries []perfguard.PlanException
	if err = json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{}
	for _, e := range entries {
		_, err := hex.DecodeString(e.SQLHash)
		_, planErr := hex.DecodeString(e.PlanHash)
		if err != nil || len(e.SQLHash) != 64 || planErr != nil || len(e.PlanHash) != 64 || len(e.Findings) == 0 || !reviewedPerformanceException(e.Issue, e.IssueURL, e.Reason) {
			t.Fatal("plan exception needs an exact shape hash, finding and linked P1 justification")
		}
		for _, finding := range e.Findings {
			key := e.SQLHash + "\n" + e.PlanHash + "\n" + finding
			if finding == "" || allowed[key] {
				t.Fatal("empty or duplicate exact plan finding")
			}
			allowed[key] = true
		}
	}
	return allowed
}

func TestPerformanceExceptionInventory(t *testing.T) {
	// Serial: performance samples must not compete with parallel fixtures.
	performanceBaselineBudgets(t, performanceBudgets(t))
	performancePlanExceptions(t)
}

func validatePerformanceSourceHash(pin string) error {
	decoded, err := hex.DecodeString(pin)
	if err != nil || len(pin) != 64 || len(decoded) != 32 {
		return fmt.Errorf("every allowance requires a valid SHA-256 source hash")
	}
	return nil
}

func validatePerformanceSourcePin(pin, actual string) error {
	if err := validatePerformanceSourceHash(pin); err != nil {
		return err
	}
	if pin != actual {
		return fmt.Errorf("source hash changed; remeasure and review the linked P1")
	}
	return nil
}
func TestPerformanceEveryAllowanceRequiresSourcePin(t *testing.T) {
	// Serial: performance samples must not compete with parallel fixtures.
	raw, err := os.ReadFile("testdata/performance_budget_allowlist.json")
	if err != nil {
		t.Fatal(err)
	}
	var entries []baselineBudget
	if err = json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		for _, bad := range []string{"", strings.Repeat("z", 64), strings.Repeat("a", 63), strings.Repeat("a", 64)} {
			if err := validatePerformanceSourcePin(bad, e.CoreSourceHash); err == nil {
				t.Fatalf("%s/%s accepted invalid pin", e.Path, e.Principal)
			}
		}
	}
}
