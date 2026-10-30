package filter

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"github.com/reviewdog/reviewdog/diff"
	"github.com/reviewdog/reviewdog/proto/rdf"
)

func TestStartByteOffset(t *testing.T) {
	const s = "aébあ" // a(1) é(2) b(1) あ(3) => 7 bytes

	tests := []struct {
		name   string
		column int
		want   int
	}{
		{"omitted column", 0, 0},
		{"negative column", -3, 0},
		{"first byte", 1, 0},
		{"start of two-byte rune", 2, 1},
		{"middle of two-byte rune expands left", 3, 1},
		{"start of three-byte rune", 5, 4},
		{"middle of three-byte rune expands left", 6, 4},
		{"past line end clamps to line end", 100, len(s)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StartByteOffset(s, tt.column); got != tt.want {
				t.Errorf("StartByteOffset(%q, %d) = %d, want %d", s, tt.column, got, tt.want)
			}
		})
	}
}

func TestEndByteOffset(t *testing.T) {
	const s = "aébあ" // a(1) é(2) b(1) あ(3) => 7 bytes

	tests := []struct {
		name   string
		column int
		want   int
	}{
		{"omitted column means line end", 0, len(s)},
		{"negative column means line end", -1, len(s)},
		{"start of two-byte rune is a boundary", 2, 1},
		{"middle of two-byte rune expands right", 3, 3},
		{"middle of three-byte rune expands right", 6, 7},
		{"past line end clamps to line end", 100, len(s)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EndByteOffset(s, tt.column); got != tt.want {
				t.Errorf("EndByteOffset(%q, %d) = %d, want %d", s, tt.column, got, tt.want)
			}
		})
	}
}

func TestNormalizeLocationColumns(t *testing.T) {
	tests := []struct {
		name        string
		location    *rdf.Location
		sourceLines map[int]string
		want        *rdf.Location
	}{
		{
			name: "in-bounds ascii columns unchanged",
			location: &rdf.Location{Range: &rdf.Range{
				Start: &rdf.Position{Line: 1, Column: 2},
				End:   &rdf.Position{Line: 1, Column: 5},
			}},
			sourceLines: map[int]string{1: "hello"},
			want: &rdf.Location{Range: &rdf.Range{
				Start: &rdf.Position{Line: 1, Column: 2},
				End:   &rdf.Position{Line: 1, Column: 5},
			}},
		},
		{
			name: "out-of-bounds columns clamp to line end",
			location: &rdf.Location{Range: &rdf.Range{
				Start: &rdf.Position{Line: 1, Column: 100},
				End:   &rdf.Position{Line: 1, Column: 200},
			}},
			sourceLines: map[int]string{1: "abc"},
			want: &rdf.Location{Range: &rdf.Range{
				Start: &rdf.Position{Line: 1, Column: 4},
				End:   &rdf.Position{Line: 1, Column: 4},
			}},
		},
		{
			name: "columns inside multi-byte runes expand outward",
			// "あいう": each rune is 3 bytes. Start col 2 is inside the first
			// rune (-> col 1), end col 5 is inside the second rune (-> col 7).
			location: &rdf.Location{Range: &rdf.Range{
				Start: &rdf.Position{Line: 1, Column: 2},
				End:   &rdf.Position{Line: 1, Column: 5},
			}},
			sourceLines: map[int]string{1: "あいう"},
			want: &rdf.Location{Range: &rdf.Range{
				Start: &rdf.Position{Line: 1, Column: 1},
				End:   &rdf.Position{Line: 1, Column: 7},
			}},
		},
		{
			name: "end line omitted resolves against start line",
			location: &rdf.Location{Range: &rdf.Range{
				Start: &rdf.Position{Line: 3, Column: 2},
				End:   &rdf.Position{Column: 100},
			}},
			sourceLines: map[int]string{3: "abc"},
			want: &rdf.Location{Range: &rdf.Range{
				Start: &rdf.Position{Line: 3, Column: 2},
				End:   &rdf.Position{Column: 4},
			}},
		},
		{
			name: "omitted columns stay omitted",
			location: &rdf.Location{Range: &rdf.Range{
				Start: &rdf.Position{Line: 1},
				End:   &rdf.Position{Line: 1},
			}},
			sourceLines: map[int]string{1: "abc"},
			want: &rdf.Location{Range: &rdf.Range{
				Start: &rdf.Position{Line: 1},
				End:   &rdf.Position{Line: 1},
			}},
		},
		{
			name: "unavailable source lines keep columns untouched",
			location: &rdf.Location{Range: &rdf.Range{
				Start: &rdf.Position{Line: 1, Column: 100},
				End:   &rdf.Position{Line: 1, Column: 200},
			}},
			sourceLines: map[int]string{},
			want: &rdf.Location{Range: &rdf.Range{
				Start: &rdf.Position{Line: 1, Column: 100},
				End:   &rdf.Position{Line: 1, Column: 200},
			}},
		},
		{
			name: "partially available lines normalize only available ones",
			location: &rdf.Location{Range: &rdf.Range{
				Start: &rdf.Position{Line: 1, Column: 100},
				End:   &rdf.Position{Line: 2, Column: 200},
			}},
			sourceLines: map[int]string{1: "abc"},
			want: &rdf.Location{Range: &rdf.Range{
				Start: &rdf.Position{Line: 1, Column: 4},
				End:   &rdf.Position{Line: 2, Column: 200},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			NormalizeLocationColumns(tt.location, tt.sourceLines)
			if diff := cmp.Diff(tt.want, tt.location, protocmp.Transform()); diff != "" {
				t.Errorf("NormalizeLocationColumns() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFilterCheckNormalizesColumnsFromSourceLines(t *testing.T) {
	filediffs, _ := diff.ParseMultiFile(strings.NewReader(diffContent))
	results := []*rdf.Diagnostic{
		{
			Location: &rdf.Location{
				Path: "sample.new.txt",
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 2, Column: 100},
					End:   &rdf.Position{Line: 2, Column: 200},
				},
			},
		},
	}
	checks := FilterCheck(results, filediffs, 0, "", ModeAdded)
	if len(checks) != 1 {
		t.Fatalf("len(checks) = %d, want 1", len(checks))
	}
	rng := checks[0].Diagnostic.GetLocation().GetRange()
	// "added line" is 10 bytes, so columns past its end clamp to 11.
	if got := rng.GetStart().GetColumn(); got != 11 {
		t.Errorf("start column = %d, want 11 (line end)", got)
	}
	if got := rng.GetEnd().GetColumn(); got != 11 {
		t.Errorf("end column = %d, want 11 (line end)", got)
	}
}

func TestFilterCheckKeepsColumnsWhenSourceUnavailable(t *testing.T) {
	// No diff at all: source line content cannot be resolved, so columns must
	// be passed through unchanged.
	results := []*rdf.Diagnostic{
		{
			Location: &rdf.Location{
				Path: "sample.new.txt",
				Range: &rdf.Range{
					Start: &rdf.Position{Line: 2, Column: 100},
				},
			},
		},
	}
	checks := FilterCheck(results, []*diff.FileDiff(nil), 0, "", ModeNoFilter)
	if got := checks[0].Diagnostic.GetLocation().GetRange().GetStart().GetColumn(); got != 100 {
		t.Errorf("start column = %d, want 100 (unchanged without source lines)", got)
	}
}
