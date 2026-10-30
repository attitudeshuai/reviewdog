package rdf

import "testing"

func TestParseSeverity(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Severity
	}{
		// Existing spellings keep mapping to the same level.
		{"error", "error", Severity_ERROR},
		{"ERROR", "ERROR", Severity_ERROR},
		{"Error", "Error", Severity_ERROR},
		{"e", "e", Severity_ERROR},
		{"E", "E", Severity_ERROR},
		{"warning", "warning", Severity_WARNING},
		{"WARNING", "WARNING", Severity_WARNING},
		{"Warning", "Warning", Severity_WARNING},
		{"w", "w", Severity_WARNING},
		{"W", "W", Severity_WARNING},
		{"info", "info", Severity_INFO},
		{"INFO", "INFO", Severity_INFO},
		{"i", "i", Severity_INFO},
		{"note is info", "note", Severity_INFO},
		{"NOTE", "NOTE", Severity_INFO},
		{"n", "n", Severity_INFO},

		// Common synonyms for the error level.
		{"fatal", "fatal", Severity_ERROR},
		{"Fatal", "Fatal", Severity_ERROR},
		{"FATAL", "FATAL", Severity_ERROR},
		{"critical", "critical", Severity_ERROR},
		{"blocker", "blocker", Severity_ERROR},
		{"blocking", "blocking", Severity_ERROR},
		{"failure", "failure", Severity_ERROR},
		{"致命", "致命", Severity_ERROR},
		{"严重", "严重", Severity_ERROR},
		{"阻断", "阻断", Severity_ERROR},

		// Common synonyms for the warning level.
		{"warn", "warn", Severity_WARNING},
		{"警告", "警告", Severity_WARNING},

		// Common synonyms for the info level.
		{"hint", "hint", Severity_INFO},
		{"suggestion", "suggestion", Severity_INFO},
		{"notice", "notice", Severity_INFO},
		{"提示", "提示", Severity_INFO},
		{"建议", "建议", Severity_INFO},

		// Matching ignores leading/trailing whitespace.
		{"spaced error", "  error ", Severity_ERROR},
		{"spaced fatal", "\tFatal\n", Severity_ERROR},
		{"spaced warning", " Warning", Severity_WARNING},
		{"spaced Chinese", "  提示 ", Severity_INFO},

		// Unrecognized words stay unknown and never become errors.
		{"empty", "", Severity_UNKNOWN_SEVERITY},
		{"whitespace only", "   ", Severity_UNKNOWN_SEVERITY},
		{"unknown word", "banana", Severity_UNKNOWN_SEVERITY},
		{"sarif none is not a level", "none", Severity_UNKNOWN_SEVERITY},
		{"typo eror", "eror", Severity_UNKNOWN_SEVERITY},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseSeverity(tt.in); got != tt.want {
				t.Errorf("ParseSeverity(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeLevel(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"   ", ""},
		{"info", "info"},
		{"WARNING", "warning"},
		{" Error ", "error"},
		{"fatal", "error"},
		{"严重", "error"},
		{"warn", "warning"},
		{"  提示 ", "info"},
		{"typo", ""},
	}
	for _, tt := range tests {
		if got := NormalizeLevel(tt.in); got != tt.want {
			t.Errorf("NormalizeLevel(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSeverityName(t *testing.T) {
	if got := SeverityName(Severity_ERROR); got != "error" {
		t.Errorf("SeverityName(ERROR) = %q, want error", got)
	}
	if got := SeverityName(Severity_WARNING); got != "warning" {
		t.Errorf("SeverityName(WARNING) = %q, want warning", got)
	}
	if got := SeverityName(Severity_INFO); got != "info" {
		t.Errorf("SeverityName(INFO) = %q, want info", got)
	}
	if got := SeverityName(Severity_UNKNOWN_SEVERITY); got != "" {
		t.Errorf("SeverityName(UNKNOWN) = %q, want empty", got)
	}
}
