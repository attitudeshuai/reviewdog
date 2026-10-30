package parser

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/reviewdog/reviewdog/proto/rdf"
)

func TestNewParser(t *testing.T) {
	tests := []struct {
		in      *Option
		typ     Parser
		wantErr bool
	}{
		{
			in: &Option{
				FormatName: "checkstyle",
			},
			typ: &CheckStyleParser{},
		},
		{
			in: &Option{
				FormatName: "rdjsonl",
			},
			typ: &RDJSONLParser{},
		},
		{
			in: &Option{
				FormatName: "golint",
			},
			typ: &ErrorformatParser{},
		},
		{
			in: &Option{
				Errorformat: []string{`%f:%l:%c:%m`},
			},
			typ: &ErrorformatParser{},
		},
		{
			in: &Option{
				FormatName: "sarif",
			},
			typ: &SarifParser{},
		},
		{ // empty
			in:      &Option{},
			wantErr: true,
		},
		{ // both
			in: &Option{
				FormatName:  "checkstyle",
				Errorformat: []string{`%f:%l:%c:%m`},
			},
			wantErr: true,
		},
		{ // unsupported
			in: &Option{
				FormatName: "unsupported format",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		p, err := New(tt.in)
		if tt.wantErr && err != nil {
			continue
		}
		if err != nil {
			t.Error(err)
			continue
		}
		if got, want := reflect.TypeOf(p), reflect.TypeOf(tt.typ); got != want {
			t.Errorf("typ: got %v, want %v", got, want)
		}
	}
}

// TestSeverityConsistentAcrossTextInputs ensures the three severity levels and
// their common synonyms resolve to the same rdf.Severity whether they arrive as
// checkstyle XML severity attributes (which support multi-char synonyms and
// surrounding whitespace) or as single-char errorformat %t types, and that
// unrecognized words stay unknown in both inputs.
func TestSeverityConsistentAcrossTextInputs(t *testing.T) {
	checkstyleSeverity := func(t *testing.T, word string) rdf.Severity {
		t.Helper()
		xml := fmt.Sprintf(
			`<checkstyle><file name="f"><error line="1" column="2" severity=%q message="m" source="s"/></file></checkstyle>`,
			word)
		ds, err := NewCheckStyleParser().Parse(strings.NewReader(xml))
		if err != nil {
			t.Fatalf("checkstyle parse(%q): %v", word, err)
		}
		if len(ds) != 1 {
			t.Fatalf("checkstyle parse(%q): got %d diagnostics, want 1", word, len(ds))
		}
		return ds[0].GetSeverity()
	}

	errorformatSeverity := func(t *testing.T, typ string) rdf.Severity {
		t.Helper()
		p, err := NewErrorformatParserString([]string{`%f:%l:%c:%t:%m`})
		if err != nil {
			t.Fatal(err)
		}
		ds, err := p.Parse(strings.NewReader(fmt.Sprintf("f:1:2:%s:m", typ)))
		if err != nil {
			t.Fatalf("errorformat parse(type=%q): %v", typ, err)
		}
		if len(ds) != 1 {
			t.Fatalf("errorformat parse(type=%q): got %d diagnostics, want 1", typ, len(ds))
		}
		return ds[0].GetSeverity()
	}

	for _, tt := range []struct {
		name string
		word string // checkstyle severity attribute
		want rdf.Severity
	}{
		{"error existing", "error", rdf.Severity_ERROR},
		{"error synonym fatal", "Fatal", rdf.Severity_ERROR},
		{"error synonym critical with spaces", " critical ", rdf.Severity_ERROR},
		{"error synonym blocker", "blocker", rdf.Severity_ERROR},
		{"error synonym Chinese", "致命", rdf.Severity_ERROR},
		{"warning existing", "warning", rdf.Severity_WARNING},
		{"warning synonym warn with spaces", " Warn ", rdf.Severity_WARNING},
		{"warning synonym Chinese", "警告", rdf.Severity_WARNING},
		{"info existing", "info", rdf.Severity_INFO},
		{"note stays info", "note", rdf.Severity_INFO},
		{"info synonym hint", "hint", rdf.Severity_INFO},
		{"info synonym suggestion with spaces", " Suggestion ", rdf.Severity_INFO},
		{"info synonym Chinese", "提示", rdf.Severity_INFO},
		{"unknown stays unknown", "banana", rdf.Severity_UNKNOWN_SEVERITY},
		{"whitespace-only stays unknown", "   ", rdf.Severity_UNKNOWN_SEVERITY},
	} {
		t.Run("checkstyle/"+tt.name, func(t *testing.T) {
			if got := checkstyleSeverity(t, tt.word); got != tt.want {
				t.Errorf("checkstyle severity %q = %v, want %v", tt.word, got, tt.want)
			}
		})
	}

	// errorformat %t is a single character, so exercise the one-letter spellings.
	for _, tt := range []struct {
		typ  string
		want rdf.Severity
	}{
		{"E", rdf.Severity_ERROR},
		{"e", rdf.Severity_ERROR},
		{"W", rdf.Severity_WARNING},
		{"w", rdf.Severity_WARNING},
		{"I", rdf.Severity_INFO},
		{"N", rdf.Severity_INFO}, // note is treated as info
		{"X", rdf.Severity_UNKNOWN_SEVERITY},
	} {
		t.Run("errorformat/"+tt.typ, func(t *testing.T) {
			if got := errorformatSeverity(t, tt.typ); got != tt.want {
				t.Errorf("errorformat type %q = %v, want %v", tt.typ, got, tt.want)
			}
		})
	}
}

// TestCheckStyleSeverityKeepsOriginalOutput ensures severity normalization
// only changes the structured severity and leaves the raw output untouched.
func TestCheckStyleSeverityKeepsOriginalOutput(t *testing.T) {
	const xml = `<checkstyle><file name="f.go"><error line="1" column="2" severity=" Fatal " message="boom" source="rule.x"/></file></checkstyle>`
	ds, err := NewCheckStyleParser().Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatal(err)
	}
	d := ds[0]
	if d.GetSeverity() != rdf.Severity_ERROR {
		t.Errorf("severity = %v, want ERROR", d.GetSeverity())
	}
	if got, want := d.GetOriginalOutput(), "f.go:1:2:  Fatal : boom (rule.x)"; got != want {
		t.Errorf("originalOutput = %q, want %q", got, want)
	}
	if d.GetCode().GetValue() != "rule.x" {
		t.Errorf("code = %q, want rule.x", d.GetCode().GetValue())
	}
}
