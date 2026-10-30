package githubutils

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/reviewdog/reviewdog/proto/rdf"
)

// captureStdout captures everything written to os.Stdout while f runs
// (the GitHub Actions toolkit emits logging commands to os.Stdout).
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	f()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func unknownDiagnostic() *rdf.Diagnostic {
	return &rdf.Diagnostic{
		Message: "msg",
		Location: &rdf.Location{
			Path:  "f.go",
			Range: &rdf.Range{Start: &rdf.Position{Line: 1}},
		},
	}
}

func TestReportAsGitHubActionsLog_DefaultLevel(t *testing.T) {
	// A diagnostic without an explicit severity takes the configured default
	// report level, resolved through the same severity word table as the
	// GitHub Check reporter: equivalent levels map to equivalent command
	// categories in both exits. Invalid levels are rejected at startup, so no
	// "Unknown level" annotation must ever be emitted.
	for _, tc := range []struct {
		level   string
		wantCmd string
	}{
		{"", "::error"}, // historical default for no level
		{"error", "::error"},
		{" Error ", "::error"},
		{"fatal", "::error"},
		{"CRITICAL", "::error"},
		{"阻断", "::error"},
		{"warning", "::warning"},
		{" Warn ", "::warning"},
		{"警告", "::warning"},
		{"info", "::warning"}, // no info command with location data: info -> warning
		{"hint", "::warning"},
		{"提示", "::warning"},
		{"loud", "::error"}, // defensive: cannot happen after startup validation
	} {
		t.Run(tc.level, func(t *testing.T) {
			out := captureStdout(t, func() {
				ReportAsGitHubActionsLog("tool", tc.level, unknownDiagnostic())
			})
			if !strings.HasPrefix(out, tc.wantCmd) {
				t.Errorf("level %q emitted %q, want prefix %q", tc.level, out, tc.wantCmd)
			}
			if strings.Contains(out, "Unknown level") {
				t.Errorf("level %q emitted an Unknown level annotation: %q", tc.level, out)
			}
		})
	}
}

func TestReportAsGitHubActionsLog_DiagnosticSeverityWins(t *testing.T) {
	for _, tc := range []struct {
		name     string
		severity rdf.Severity
		level    string
		wantCmd  string
	}{
		{"explicit error ignores info default", rdf.Severity_ERROR, "info", "::error"},
		{"explicit warning with no default", rdf.Severity_WARNING, "", "::warning"},
		{"explicit info with no default", rdf.Severity_INFO, "", "::warning"},
		{"unknown severity uses default", rdf.Severity_UNKNOWN_SEVERITY, "warning", "::warning"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := unknownDiagnostic()
			d.Severity = tc.severity
			out := captureStdout(t, func() {
				ReportAsGitHubActionsLog("tool", tc.level, d)
			})
			if !strings.HasPrefix(out, tc.wantCmd) {
				t.Errorf("emitted %q, want prefix %q", out, tc.wantCmd)
			}
		})
	}
}
