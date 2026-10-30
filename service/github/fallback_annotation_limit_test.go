package github

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-github/v93/github"

	"github.com/reviewdog/reviewdog"
	"github.com/reviewdog/reviewdog/filter"
	"github.com/reviewdog/reviewdog/proto/rdf"
	"github.com/reviewdog/reviewdog/service/github/githubutils"
)

func fallbackComment(path, msg string, inDiffContext bool) *reviewdog.Comment {
	return &reviewdog.Comment{
		Result: &filter.FilteredDiagnostic{
			Diagnostic: &rdf.Diagnostic{
				Location: &rdf.Location{
					Path:  path,
					Range: &rdf.Range{Start: &rdf.Position{Line: 1}},
				},
				Message: msg,
			},
			InDiffFile:    true,
			InDiffContext: inDiffContext,
			ShouldReport:  true,
		},
		ToolName: "tool",
	}
}

func chdirTempGitRepo(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

// TestCheck_fallbackToLog_sharesAnnotationLimit verifies that check runs
// falling back to logging commands use the shared per-run annotation limit
// and that the check level still determines the annotation category.
func TestCheck_fallbackToLog_sharesAnnotationLimit(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/check-runs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	buf := &bytes.Buffer{}
	limit := githubutils.NewAnnotationLimit()
	limit.SetOutput(buf)

	ch, err := NewGitHubCheck(newGitHubClient(t, ts.URL), "o", "r", 1, "sha", "warning", "tool",
		WithCheckAnnotationLimit(limit))
	if err != nil {
		t.Fatal(err)
	}

	// Unspecified severity must be reported with the warning command because
	// the check level is "warning".
	for i := 1; i <= 3; i++ {
		if err := ch.Post(context.Background(), fallbackComment("f.go", "marker-w"+itoa(i), true)); err != nil {
			t.Fatal(err)
		}
	}
	if err := ch.Flush(context.Background()); err != nil {
		t.Fatalf("fallback Flush should not fail: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("annotations must be deferred to the shared limit, got: %s", buf.String())
	}
	if err := limit.Flush(context.Background()); err != nil {
		t.Fatalf("limit Flush should not fail: %v", err)
	}
	out := buf.String()
	if n := strings.Count(out, "::warning"); n != 3 {
		t.Errorf("warning annotations = %d, want 3 (check level must determine category)", n)
	}
	for i := 1; i <= 3; i++ {
		if !strings.Contains(out, "marker-w"+itoa(i)) {
			t.Errorf("output missing marker-w%d: %s", i, out)
		}
	}
	if strings.Contains(out, "Too many results") {
		t.Error("over-limit warning should not be emitted under the limit")
	}
}

// TestCheck_fallbackToLog_multipleInstancesShareLimit simulates a project
// run where each runner's check run falls back to logging, creating separate
// writer instances. Their counts must be combined into a single retained set
// and a single warning.
func TestCheck_fallbackToLog_multipleInstancesShareLimit(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/check-runs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	buf := &bytes.Buffer{}
	limit := githubutils.NewAnnotationLimit()
	limit.SetOutput(buf)

	ch, err := NewGitHubCheck(newGitHubClient(t, ts.URL), "o", "r", 1, "sha", "warning", "tool",
		WithCheckAnnotationLimit(limit))
	if err != nil {
		t.Fatal(err)
	}

	for cycle := 1; cycle <= 2; cycle++ {
		for i := 1; i <= 6; i++ {
			msg := "marker-cycle" + itoa(cycle) + "-w" + itoa(i)
			if err := ch.Post(context.Background(), fallbackComment("f.go", msg, true)); err != nil {
				t.Fatal(err)
			}
		}
		if err := ch.Flush(context.Background()); err != nil {
			t.Fatalf("fallback Flush should not fail: %v", err)
		}
		if buf.Len() != 0 {
			t.Fatalf("annotations must be deferred to the shared limit, got: %s", buf.String())
		}
	}

	if err := limit.Flush(context.Background()); err != nil {
		t.Fatalf("limit Flush should not fail: %v", err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	var annotations, warnings int
	for _, line := range lines {
		if strings.HasPrefix(line, "::warning") {
			annotations++
		}
		if strings.Contains(line, "Too many results") {
			warnings++
		}
	}
	if annotations != 10 {
		t.Errorf("retained annotations across instances = %d, want 10", annotations)
	}
	if warnings != 1 {
		t.Errorf("over-limit warning count = %d, want 1", warnings)
	}
	if !strings.Contains(out, "dropped 2 annotation(s)") {
		t.Errorf("warning must contain combined dropped count: %s", out)
	}
	// Input order within the category is preserved across fallback instances.
	if !strings.Contains(out, "marker-cycle1-w1") || !strings.Contains(out, "marker-cycle2-w4") {
		t.Errorf("retained set should keep early entries in input order: %s", out)
	}
	if strings.Contains(out, "marker-cycle2-w5") || strings.Contains(out, "marker-cycle2-w6") {
		t.Errorf("later entries should be dropped: %s", out)
	}
}

// TestPullRequest_fallbackToLog_sharesAnnotationLimit verifies that review
// comments falling back to logging commands use the shared per-run annotation
// limit with the same semantics as the direct annotation reporter.
func TestPullRequest_fallbackToLog_sharesAnnotationLimit(t *testing.T) {
	chdirTempGitRepo(t)
	t.Setenv("GITHUB_ACTIONS", "true")

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/pulls/14/comments", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode([]*github.PullRequestComment{}); err != nil {
			t.Fatal(err)
		}
	})
	mux.HandleFunc("/repos/o/r/pulls/14/reviews", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/repos/o/r", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(&github.Repository{
			HTMLURL: new("https://test/repo/path"),
		}); err != nil {
			t.Fatal(err)
		}
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	buf := &bytes.Buffer{}
	limit := githubutils.NewAnnotationLimit()
	limit.SetOutput(buf)

	g := NewGitHubPullRequest(newGitHubClient(t, ts.URL), "o", "r", 14, "sha", "warning", "tool",
		WithPullRequestAnnotationLimit(limit))

	// Two flush cycles, as a project run does for different runners. Each
	// creates a fresh bound log writer but they share the run limit.
	for cycle := 1; cycle <= 2; cycle++ {
		for i := 1; i <= 6; i++ {
			msg := "marker-cycle" + itoa(cycle) + "-w" + itoa(i)
			if err := g.Post(context.Background(), fallbackComment(filepath.Join("dir", "f.go"), msg, true)); err != nil {
				t.Fatal(err)
			}
		}
		if err := g.Flush(context.Background()); err != nil {
			t.Fatalf("fallback Flush should not fail: %v", err)
		}
		if buf.Len() != 0 {
			t.Fatalf("annotations must be deferred to the shared limit, got: %s", buf.String())
		}
		g.SetTool("tool", "warning")
	}

	if err := limit.Flush(context.Background()); err != nil {
		t.Fatalf("limit Flush should not fail: %v", err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	var annotations, warnings int
	for _, line := range lines {
		if strings.HasPrefix(line, "::warning") {
			annotations++
		}
		if strings.Contains(line, "Too many results") {
			warnings++
		}
	}
	if annotations != 10 {
		t.Errorf("retained annotations across instances = %d, want 10", annotations)
	}
	if warnings != 1 {
		t.Errorf("over-limit warning count = %d, want 1", warnings)
	}
	if !strings.Contains(out, "dropped 2 annotation(s)") {
		t.Errorf("warning must contain combined dropped count: %s", out)
	}
	if !strings.Contains(out, "marker-cycle1-w1") || !strings.Contains(out, "marker-cycle2-w4") {
		t.Errorf("retained set should keep early entries in input order: %s", out)
	}
	if strings.Contains(out, "marker-cycle2-w5") || strings.Contains(out, "marker-cycle2-w6") {
		t.Errorf("later entries should be dropped: %s", out)
	}
}

func itoa(i int) string { return strconv.Itoa(i) }
