package githubutils

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/reviewdog/reviewdog"
	"github.com/reviewdog/reviewdog/filter"
	"github.com/reviewdog/reviewdog/proto/rdf"
)

func mustExecGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newComment(toolName, path, msg string, severity rdf.Severity) *reviewdog.Comment {
	return &reviewdog.Comment{
		ToolName: toolName,
		Result: &filter.FilteredDiagnostic{
			Diagnostic: &rdf.Diagnostic{
				Location: &rdf.Location{Path: path},
				Message:  msg,
				Severity: severity,
			},
			ShouldReport: true,
		},
	}
}

// Logging command lines encode newlines as %0A, so message markers are
// extracted like: %0A(marker-w1)%0A.
var markerRe = regexp.MustCompile(`%0A(marker-[we]\d+)%0A`)

// lineMarkers extracts markers from every output line in line order.
func lineMarkers(out string) []string {
	var got []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		m := markerRe.FindStringSubmatch(line)
		if m != nil {
			got = append(got, m[1])
		}
	}
	return got
}

func countLines(out, prefix string) int {
	n := 0
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.HasPrefix(line, prefix) {
			n++
		}
	}
	return n
}

func TestGitHubActionLogWriter_underLimit_preservesSequence(t *testing.T) {
	buf := &bytes.Buffer{}
	lw := NewGitHubActionLogWriter("", WithOutput(buf))

	comments := []*reviewdog.Comment{
		newComment("tool", "f1", "m1", rdf.Severity_ERROR),
		newComment("tool", "f2", "m2", rdf.Severity_WARNING),
		newComment("tool", "f3", "m3", rdf.Severity_INFO),
	}
	for _, c := range comments {
		if err := lw.Post(context.Background(), c); err != nil {
			t.Fatal(err)
		}
	}
	if err := lw.Flush(context.Background()); err != nil {
		t.Fatalf("Flush should not fail: %v", err)
	}

	// The output must be byte identical to the previous logging command format
	// and the input order must be preserved when under the limit.
	want := strings.Join([]string{
		`::error file=f1::[tool] reported by reviewdog 🐶%0Am1%0A%0ARaw Output:%0A`,
		`::warning file=f2::[tool] reported by reviewdog 🐶%0Am2%0A%0ARaw Output:%0A`,
		`::warning file=f3::[tool] reported by reviewdog 🐶%0Am3%0A%0ARaw Output:%0A`,
	}, "\n") + "\n"
	if buf.String() != want {
		t.Errorf("output diff:\n got: %q\nwant: %q", buf.String(), want)
	}
	if strings.Contains(buf.String(), "Too many results") {
		t.Error("over-limit warning should not be emitted under the limit")
	}
}

func TestGitHubActionLogWriter_overLimit_dropsByCategoryAndWarnsOnce(t *testing.T) {
	buf := &bytes.Buffer{}
	lw := NewGitHubActionLogWriter("warning", WithOutput(buf))

	// 8 warnings (input first) followed by 5 errors.
	for i := 1; i <= 8; i++ {
		if err := lw.Post(context.Background(),
			newComment("tool", "f", "marker-w"+itoa(i), rdf.Severity_WARNING)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 5; i++ {
		if err := lw.Post(context.Background(),
			newComment("tool", "f", "marker-e"+itoa(i), rdf.Severity_ERROR)); err != nil {
			t.Fatal(err)
		}
	}
	if err := lw.Flush(context.Background()); err != nil {
		t.Fatalf("over-limit Flush must not fail: %v", err)
	}

	out := buf.String()
	wantOrder := []string{
		"marker-e1", "marker-e2", "marker-e3", "marker-e4", "marker-e5",
		"marker-w1", "marker-w2", "marker-w3", "marker-w4", "marker-w5",
	}
	if got := lineMarkers(out); strings.Join(got, ",") != strings.Join(wantOrder, ",") {
		t.Errorf("retained order = %v, want %v", got, wantOrder)
	}
	if n := countLines(out, "::error"); n != 5+1 { // 5 errors + one over-limit warning
		t.Errorf("error command lines = %d, want %d", n, 6)
	}
	if n := countLines(out, "::warning"); n != 5 {
		t.Errorf("warning command lines = %d, want 5", n)
	}
	if n := strings.Count(out, "Too many results"); n != 1 {
		t.Errorf("over-limit warning count = %d, want 1", n)
	}
	if !strings.Contains(out, "dropped 3 annotation(s)") {
		t.Errorf("warning must contain total dropped count: %s", out)
	}
	if !strings.Contains(out, "per-step limit of 10") {
		t.Errorf("warning must contain the limit: %s", out)
	}
}

func TestGitHubActionLogWriter_atLimit_warnsWithZeroDropped(t *testing.T) {
	buf := &bytes.Buffer{}
	lw := NewGitHubActionLogWriter("warning", WithOutput(buf))
	for i := 0; i < MaxLoggingAnnotationsPerStep; i++ {
		if err := lw.Post(context.Background(),
			newComment("tool", "f", "marker-w"+itoa(i+1), rdf.Severity_WARNING)); err != nil {
			t.Fatal(err)
		}
	}
	if err := lw.Flush(context.Background()); err != nil {
		t.Fatalf("Flush should not fail: %v", err)
	}
	out := buf.String()
	if len(lineMarkers(out)) != 10 {
		t.Errorf("retained = %d, want 10", len(lineMarkers(out)))
	}
	if n := strings.Count(out, "Too many results"); n != 1 {
		t.Errorf("warning count at limit = %d, want 1", n)
	}
	if !strings.Contains(out, "dropped 0 annotation(s)") {
		t.Errorf("warning at limit should report 0 dropped: %s", out)
	}
}

func TestGitHubActionLogWriter_standalone_warnsAtMostOnceAcrossFlushes(t *testing.T) {
	buf := &bytes.Buffer{}
	lw := NewGitHubActionLogWriter("warning", WithOutput(buf))
	post := func(markers ...string) {
		for _, m := range markers {
			if err := lw.Post(context.Background(),
				newComment("tool", "f", "marker-"+m, rdf.Severity_WARNING)); err != nil {
				t.Fatal(err)
			}
		}
		if err := lw.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	post("w1", "w2", "w3", "w4", "w5", "w6", "w7", "w8", "w9", "w10")
	post("w11", "w12")
	out := buf.String()
	if n := strings.Count(out, "Too many results"); n != 1 {
		t.Errorf("over-limit warning count = %d, want 1", n)
	}
}

func TestAnnotationLimit_sharedAcrossWriters(t *testing.T) {
	buf := &bytes.Buffer{}
	limit := NewAnnotationLimit()
	limit.SetOutput(buf)

	// Writer A: default error level with unspecified severity -> error category.
	lwA := NewGitHubActionLogWriter("", WithAnnotationLimit(limit))
	for i := 1; i <= 6; i++ {
		if err := lwA.Post(context.Background(),
			newComment("toolA", "f", "marker-e"+itoa(i), rdf.Severity_UNKNOWN_SEVERITY)); err != nil {
			t.Fatal(err)
		}
	}
	// Writer B: warning level with unspecified severity -> warning category.
	lwB := NewGitHubActionLogWriter("warning", WithAnnotationLimit(limit))
	for i := 1; i <= 6; i++ {
		if err := lwB.Post(context.Background(),
			newComment("toolB", "f", "marker-w"+itoa(i), rdf.Severity_UNKNOWN_SEVERITY)); err != nil {
			t.Fatal(err)
		}
	}

	// Bound writers defer reporting to the shared limit; Flush emits nothing.
	if err := lwA.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := lwB.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("bound writers should not emit at Flush, got: %s", buf.String())
	}

	if err := limit.Flush(context.Background()); err != nil {
		t.Fatalf("over-limit Flush must not fail: %v", err)
	}
	out := buf.String()
	wantOrder := []string{
		"marker-e1", "marker-e2", "marker-e3", "marker-e4", "marker-e5", "marker-e6",
		"marker-w1", "marker-w2", "marker-w3", "marker-w4",
	}
	if got := lineMarkers(out); strings.Join(got, ",") != strings.Join(wantOrder, ",") {
		t.Errorf("retained order = %v, want %v", got, wantOrder)
	}
	if n := strings.Count(out, "Too many results"); n != 1 {
		t.Errorf("over-limit warning count = %d, want 1", n)
	}
	if !strings.Contains(out, "dropped 2 annotation(s)") {
		t.Errorf("warning must contain combined dropped count: %s", out)
	}

	// Idempotent: a second Flush emits nothing more.
	buf.Reset()
	if err := limit.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("second Flush should emit nothing, got: %s", buf.String())
	}
}

func TestAnnotationLimit_isolatedBetweenRuns(t *testing.T) {
	for run := 0; run < 2; run++ {
		buf := &bytes.Buffer{}
		// A fresh limit per run must not inherit the warned/dropped state of
		// previous runs.
		limit := NewAnnotationLimit()
		limit.SetOutput(buf)
		lw := NewGitHubActionLogWriter("warning", WithAnnotationLimit(limit))
		for i := 0; i < 9; i++ {
			if err := lw.Post(context.Background(),
				newComment("tool", "f", "marker-w"+itoa(i+1), rdf.Severity_WARNING)); err != nil {
				t.Fatal(err)
			}
		}
		if err := limit.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(buf.String(), "Too many results") {
			t.Fatalf("run %d should not have an over-limit warning", run)
		}
	}
}

func TestGitHubActionLogWriter_stableRetainedSetAcrossRuns(t *testing.T) {
	run := func() string {
		buf := &bytes.Buffer{}
		limit := NewAnnotationLimit()
		limit.SetOutput(buf)
		lw := NewGitHubActionLogWriter("warning", WithAnnotationLimit(limit))
		// Deterministic interleaving of warnings and errors. Path-only
		// locations produce deterministic logging command lines.
		sev := []rdf.Severity{
			rdf.Severity_WARNING, rdf.Severity_ERROR, rdf.Severity_WARNING,
			rdf.Severity_ERROR, rdf.Severity_WARNING, rdf.Severity_ERROR,
			rdf.Severity_WARNING, rdf.Severity_ERROR, rdf.Severity_WARNING,
			rdf.Severity_ERROR, rdf.Severity_WARNING, rdf.Severity_ERROR,
		}
		for i, s := range sev {
			m := "marker-w"
			if s == rdf.Severity_ERROR {
				m = "marker-e"
			}
			if err := lw.Post(context.Background(),
				newComment("tool", "f", m+itoa(i+1), s)); err != nil {
				t.Fatal(err)
			}
		}
		if err := limit.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	first, second := run(), run()
	if first != second {
		t.Errorf("retained set must be stable across runs:\n first: %q\nsecond: %q", first, second)
	}
	wantOrder := []string{
		"marker-e2", "marker-e4", "marker-e6", "marker-e8", "marker-e10", "marker-e12",
		"marker-w1", "marker-w3", "marker-w5", "marker-w7",
	}
	if got := lineMarkers(first); strings.Join(got, ",") != strings.Join(wantOrder, ",") {
		t.Errorf("retained order = %v, want %v", got, wantOrder)
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestGitHubActionLogWriter_writeFailureIsReturnedWithIdentifier(t *testing.T) {
	lw := NewGitHubActionLogWriter("", WithOutput(errWriter{}))
	if err := lw.Post(context.Background(),
		newComment("mytool", "dir/file.go", "m", rdf.Severity_ERROR)); err != nil {
		t.Fatal(err)
	}
	err := lw.Flush(context.Background())
	if err == nil {
		t.Fatal("write failure must be returned")
	}
	if !strings.Contains(err.Error(), "[mytool] dir/file.go") {
		t.Errorf("error must identify the annotation entry, got: %v", err)
	}
}

func TestAnnotationLimit_writeFailureIsReturnedWithIdentifier(t *testing.T) {
	limit := NewAnnotationLimit()
	limit.SetOutput(errWriter{})
	lw := NewGitHubActionLogWriter("", WithAnnotationLimit(limit))
	if err := lw.Post(context.Background(),
		newComment("mytool", "dir/file.go", "m", rdf.Severity_ERROR)); err != nil {
		t.Fatal(err)
	}
	if err := limit.Flush(context.Background()); err == nil {
		t.Fatal("write failure must be returned")
	} else if !strings.Contains(err.Error(), "[mytool] dir/file.go") {
		t.Errorf("error must identify the annotation entry, got: %v", err)
	}
}

func TestAnnotationLevelMapping(t *testing.T) {
	tests := []struct {
		name         string
		defaultLevel string
		severity     rdf.Severity
		wantPrefix   string
	}{
		{"unspecified defaults to error", "", rdf.Severity_UNKNOWN_SEVERITY, "::error"},
		{"unspecified uses check level warning", "warning", rdf.Severity_UNKNOWN_SEVERITY, "::warning"},
		{"unspecified uses check level info", "info", rdf.Severity_UNKNOWN_SEVERITY, "::warning"},
		{"explicit error overrides warning level", "warning", rdf.Severity_ERROR, "::error"},
		{"explicit warning overrides error level", "error", rdf.Severity_WARNING, "::warning"},
		{"explicit info", "", rdf.Severity_INFO, "::warning"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			lw := NewGitHubActionLogWriter(tt.defaultLevel, WithOutput(buf))
			if err := lw.Post(context.Background(),
				newComment("tool", "f", "m", tt.severity)); err != nil {
				t.Fatal(err)
			}
			if err := lw.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			line := strings.TrimRight(buf.String(), "\n")
			if !strings.HasPrefix(line, tt.wantPrefix) {
				t.Errorf("got %q, want prefix %q", line, tt.wantPrefix)
			}
		})
	}
}

func TestAnnotationLimit_endToEndRun_overLimitSucceeds(t *testing.T) {
	// RunFromResult resolves git relative paths when ShouldPrependGitRelDir is
	// true, so run it inside a temporary git repository.
	repo := t.TempDir()
	mustExecGit(t, repo, "init")
	mustExecGit(t, repo, "config", "user.email", "test@example.com")
	mustExecGit(t, repo, "config", "user.name", "test")
	prevWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWd) })

	buf := &bytes.Buffer{}
	limit := NewAnnotationLimit()
	limit.SetOutput(buf)
	lw := NewGitHubActionLogWriter("warning", WithAnnotationLimit(limit))

	diags := make([]*rdf.Diagnostic, 0, 12)
	for i := 0; i < 12; i++ {
		diags = append(diags, &rdf.Diagnostic{
			Location: &rdf.Location{Path: "f"},
			Message:  "marker-w" + itoa(i+1),
			Severity: rdf.Severity_WARNING,
		})
	}
	// Over-limit input must not make the run fail.
	err = reviewdog.RunFromResult(context.Background(), lw, diags, nil, 0,
		"tool", filter.ModeNoFilter, reviewdog.FailLevelDefault)
	if err != nil {
		t.Fatalf("RunFromResult should succeed with over-limit input: %v", err)
	}
	if err := limit.Flush(context.Background()); err != nil {
		t.Fatalf("limit Flush should not fail: %v", err)
	}
	out := buf.String()
	if len(lineMarkers(out)) != 10 {
		t.Errorf("retained = %d, want 10", len(lineMarkers(out)))
	}
	if n := strings.Count(out, "Too many results"); n != 1 {
		t.Errorf("over-limit warning count = %d, want 1", n)
	}
	if !strings.Contains(out, "dropped 2 annotation(s)") {
		t.Errorf("warning must contain combined dropped count: %s", out)
	}
}

func itoa(i int) string { return strconv.Itoa(i) }
