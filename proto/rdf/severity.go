package rdf

import "strings"

// ParseSeverity converts a checker-specific severity name to rdf.Severity.
//
// It accepts common synonyms used by compilers, linters and reviewdog configs,
// and the match is case-insensitive and ignores leading/trailing whitespace,
// so e.g. "Fatal", " warning " and "ERROR" are all recognized.
//
// Unrecognized names, including the empty string, resolve to
// UNKNOWN_SEVERITY. An unrecognized name is never promoted to ERROR.
func ParseSeverity(name string) Severity {
	switch strings.ToLower(strings.TrimSpace(name)) {
	// Existing spellings plus common synonyms.
	// 致命 / 严重 / 阻断 are treated as errors.
	case "error", "e", "fatal", "critical", "blocker", "blocking", "failure",
		"致命", "严重", "阻断":
		return Severity_ERROR
	// 警告.
	case "warning", "w", "warn", "警告":
		return Severity_WARNING
	// note is historically treated as info. 提示 / 建议 too.
	case "info", "i", "note", "n", "hint", "suggestion", "notice",
		"提示", "建议":
		return Severity_INFO
	default:
		return Severity_UNKNOWN_SEVERITY
	}
}

// SeverityName returns the canonical report-level name ("error", "warning" or
// "info") for the given severity. It returns an empty string for
// UNKNOWN_SEVERITY.
func SeverityName(s Severity) string {
	switch s {
	case Severity_ERROR:
		return "error"
	case Severity_WARNING:
		return "warning"
	case Severity_INFO:
		return "info"
	default:
		return ""
	}
}

// NormalizeLevel canonicalizes a report-level name. It resolves synonyms,
// ignores case and leading/trailing whitespace. It returns an empty string
// for an empty or unrecognized level; callers are responsible for reporting
// unrecognized non-empty levels as errors.
func NormalizeLevel(name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	return SeverityName(ParseSeverity(name))
}
