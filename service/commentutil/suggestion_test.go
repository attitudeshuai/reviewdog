package commentutil

import (
	"testing"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"
	"github.com/reviewdog/reviewdog/proto/rdf"
)

func TestBuildSuggestionText(t *testing.T) {
	tests := []struct {
		name        string
		sourceLines map[int]string
		suggestion  *rdf.Suggestion
		want        string
		wantErr     string
	}{
		{
			name:        "same line",
			sourceLines: map[int]string{1: "hello world"},
			suggestion: &rdf.Suggestion{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 1, Column: 7},
					End:   &rdf.Position{Line: 1, Column: 12},
				},
				Text: "there",
			},
			want: "hello there",
		},
		{
			name:        "multiple lines",
			sourceLines: map[int]string{1: "old prefix", 2: "tail text"},
			suggestion: &rdf.Suggestion{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 1, Column: 5},
					End:   &rdf.Position{Line: 2, Column: 5},
				},
				Text: "NEW",
			},
			want: "old NEW text",
		},
		{
			name:        "end line omitted",
			sourceLines: map[int]string{1: "hello world"},
			suggestion: &rdf.Suggestion{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 1, Column: 7},
					End:   &rdf.Position{Column: 12},
				},
				Text: "there",
			},
			want: "hello there",
		},
		{
			name:        "end omitted",
			sourceLines: map[int]string{1: "hello"},
			suggestion: &rdf.Suggestion{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 1, Column: 6},
				},
				Text: " there",
			},
			want: "hello there",
		},
		{
			name: "no source lines",
			suggestion: &rdf.Suggestion{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 1},
					End:   &rdf.Position{Line: 1},
				},
			},
			wantErr: "source lines are not available",
		},
		{
			name:        "missing source line",
			sourceLines: map[int]string{1: "hello"},
			suggestion: &rdf.Suggestion{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 1},
					End:   &rdf.Position{Line: 2},
				},
			},
			wantErr: "source line (L=2) is not available for this suggestion",
		},
		{
			// Start and end columns both land inside the first 3-byte rune.
			// They expand outward to the rune boundary.
			name:        "multi-byte columns expand to rune boundary",
			sourceLines: map[int]string{1: "あいう"},
			suggestion: &rdf.Suggestion{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 1, Column: 2},
					End:   &rdf.Position{Line: 1, Column: 3},
				},
				Text: "X",
			},
			want: "Xいう",
		},
		{
			name:        "multi-byte end column expands to end of rune",
			sourceLines: map[int]string{1: "あいう"},
			suggestion: &rdf.Suggestion{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 1, Column: 1},
					End:   &rdf.Position{Line: 1, Column: 5},
				},
				Text: "X",
			},
			want: "Xう",
		},
		{
			name:        "columns past line end clamp to line end without panic",
			sourceLines: map[int]string{1: "abc"},
			suggestion: &rdf.Suggestion{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 1, Column: 100},
					End:   &rdf.Position{Line: 1, Column: 200},
				},
				Text: "X",
			},
			want: "abcX",
		},
		{
			name:        "start column omitted on multi-byte line starts at line head",
			sourceLines: map[int]string{1: "あいう"},
			suggestion: &rdf.Suggestion{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 1},
					End:   &rdf.Position{Line: 1, Column: 4},
				},
				Text: "X",
			},
			want: "Xいう",
		},
		{
			// End line is given without a column: the whole end line is
			// covered, so no end-line content must be retained.
			name:        "multi-line end column omitted drops end line",
			sourceLines: map[int]string{1: "head line", 2: "tail line"},
			suggestion: &rdf.Suggestion{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 1, Column: 5},
					End:   &rdf.Position{Line: 2},
				},
				Text: "NEW",
			},
			want: "headNEW",
		},
		{
			// Only start line prefix and end line suffix survive; the middle
			// line content is replaced wholesale by the suggestion text.
			name:        "multi-line keeps only start prefix and end suffix",
			sourceLines: map[int]string{1: "old prefix", 2: "middle", 3: "suffix end"},
			suggestion: &rdf.Suggestion{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 1, Column: 4},
					End:   &rdf.Position{Line: 3, Column: 7},
				},
				Text: "NEW",
			},
			want: "oldNEW end",
		},
		{
			// A missing start column is the line head: the start-line prefix
			// is empty and the whole start line belongs to the range.
			name:        "multi-line start column omitted starts at line head",
			sourceLines: map[int]string{1: "abc", 2: "def"},
			suggestion: &rdf.Suggestion{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 1},
					End:   &rdf.Position{Line: 2, Column: 2},
				},
				Text: "X",
			},
			want: "Xef",
		},
		{
			name:        "multi-line columns past line end clamp to line ends",
			sourceLines: map[int]string{1: "abc", 2: "def"},
			suggestion: &rdf.Suggestion{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 1, Column: 100},
					End:   &rdf.Position{Line: 2, Column: 200},
				},
				Text: "X",
			},
			want: "abcX",
		},
		{
			name:        "end line before start line is not renderable",
			sourceLines: map[int]string{1: "abc", 2: "def"},
			suggestion: &rdf.Suggestion{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 2, Column: 1},
					End:   &rdf.Position{Line: 1, Column: 1},
				},
			},
			wantErr: "suggestion range end line (L=1) precedes its start line (L=2)",
		},
		{
			name:        "same-line end column before start column is not renderable",
			sourceLines: map[int]string{1: "hello"},
			suggestion: &rdf.Suggestion{
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 1, Column: 5},
					End:   &rdf.Position{Line: 1, Column: 2},
				},
			},
			wantErr: "suggestion range end column precedes its start column on line 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildSuggestionText(tt.sourceLines, tt.suggestion)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("BuildSuggestionText() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildSuggestionText() error = %v", err)
			}
			if !utf8.ValidString(got) {
				t.Errorf("BuildSuggestionText() produced invalid UTF-8: %q", got)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("BuildSuggestionText() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
