package gitea

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reviewdog/reviewdog"
	"github.com/reviewdog/reviewdog/filter"
	"github.com/reviewdog/reviewdog/proto/rdf"
	"github.com/reviewdog/reviewdog/service/commentutil"
)

func workspaceSuggestionFixture(t *testing.T) *commentutil.WorkspaceSource {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ws.go"),
		[]byte(strings.Repeat("line\n", 14)+"alpha bravo\ncharlie delta\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return commentutil.NewWorkspaceSource(root)
}

func nonLineComment(t *testing.T, path string, source map[int]string) *reviewdog.Comment {
	t.Helper()
	return &reviewdog.Comment{Result: &filter.FilteredDiagnostic{
		SourceLines: source,
		Diagnostic: &rdf.Diagnostic{
			Location: &rdf.Location{Path: path},
			Suggestions: []*rdf.Suggestion{{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 15, Column: 7},
					End:   &rdf.Position{Line: 16, Column: 9},
				},
				Text: "X",
			}},
		},
	}}
}

func TestGiteaBuildNonLineBasedSuggestion_SupplementedFromWorkspace(t *testing.T) {
	ws := workspaceSuggestionFixture(t)
	c := nonLineComment(t, "ws.go", map[int]string{15: "alpha bravo"})

	got, err := buildNonLineBasedSuggestion(c, c.Result.Diagnostic.GetSuggestions()[0], ws)
	if err != nil {
		t.Fatalf("buildNonLineBasedSuggestion() error = %v", err)
	}
	if want := "```suggestion\nalpha Xdelta\n```"; got != want {
		t.Fatalf("buildNonLineBasedSuggestion() = %q, want %q", got, want)
	}
}

func TestGiteaBuildSuggestions_WorkspaceMissingFile(t *testing.T) {
	ws := workspaceSuggestionFixture(t)
	c := &reviewdog.Comment{Result: &filter.FilteredDiagnostic{
		Diagnostic: &rdf.Diagnostic{
			Location: &rdf.Location{
				Path: "missing.go",
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 1},
					End:   &rdf.Position{Line: 1},
				},
			},
			Suggestions: []*rdf.Suggestion{{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 1, Column: 1},
					End:   &rdf.Position{Line: 1, Column: 2},
				},
			}},
		},
	}}

	got := buildSuggestions(c, ws)
	want := invalidSuggestionPre + `source file "missing.go" is not found in the workspace` + invalidSuggestionPost + "\n"
	if got != want {
		t.Fatalf("buildSuggestions() = %q, want %q", got, want)
	}
}

func TestGiteaBuildSuggestions_LineOutOfRange(t *testing.T) {
	ws := workspaceSuggestionFixture(t)
	c := &reviewdog.Comment{Result: &filter.FilteredDiagnostic{
		Diagnostic: &rdf.Diagnostic{
			Location: &rdf.Location{
				Path: "ws.go",
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 100},
					End:   &rdf.Position{Line: 101},
				},
			},
			Suggestions: []*rdf.Suggestion{{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 100, Column: 1},
					End:   &rdf.Position{Line: 101, Column: 1},
				},
				Text: "X",
			}},
		},
	}}

	got := buildSuggestions(c, ws)
	want := invalidSuggestionPre + `source line (L=101) is out of range in workspace file "ws.go"` + invalidSuggestionPost + "\n"
	if got != want {
		t.Fatalf("buildSuggestions() = %q, want %q", got, want)
	}
}

func TestGiteaBuildSingleSuggestion_LineBasedDoesNotReadWorkspace(t *testing.T) {
	ws := workspaceSuggestionFixture(t)
	// This file does not exist in the workspace; line-based suggestions must
	// not read source lines.
	c := &reviewdog.Comment{Result: &filter.FilteredDiagnostic{
		Diagnostic: &rdf.Diagnostic{
			Location: &rdf.Location{
				Path:  "missing.go",
				Range: &rdf.Range{Start: &rdf.Position{Line: 15}},
			},
			Suggestions: []*rdf.Suggestion{{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 15},
					End:   &rdf.Position{Line: 15},
				},
				Text: "replacement",
			}},
		},
	}}

	got, err := buildSingleSuggestion(c, c.Result.Diagnostic.GetSuggestions()[0], ws)
	if err != nil {
		t.Fatalf("buildSingleSuggestion() error = %v", err)
	}
	if want := "```suggestion\nreplacement\n```"; got != want {
		t.Fatalf("buildSingleSuggestion() = %q, want %q", got, want)
	}
}
