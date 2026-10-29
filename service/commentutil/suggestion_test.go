package commentutil

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

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
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("BuildSuggestionText() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func colSuggestion(text string, startLine, startCol, endLine, endCol int32) *rdf.Suggestion {
	return &rdf.Suggestion{
		Text: text,
		Range: &rdf.Range{
			Start: &rdf.Position{Line: startLine, Column: startCol},
			End:   &rdf.Position{Line: endLine, Column: endCol},
		},
	}
}

func writeWorkspaceFile(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceSource_SuggestionText(t *testing.T) {
	root := t.TempDir()
	// L15-L17 of ws.go (CRLF on purpose to verify line endings are
	// normalized like diff line contents).
	writeWorkspaceFile(t, root, "ws.go",
		strings.Repeat("line\r\n", 14)+"alpha bravo\r\ncharlie delta\r\nnext line\r\n")
	writeWorkspaceFile(t, root, "sub/other.go", "only line\n")

	var openCount atomic.Int32
	ws := NewWorkspaceSource(root)
	ws.openFile = func(path string) (io.ReadCloser, error) {
		openCount.Add(1)
		return os.Open(path)
	}

	t.Run("nil source keeps historical behavior", func(t *testing.T) {
		var nilWS *WorkspaceSource
		if _, err := nilWS.SuggestionText(nil, "ws.go", colSuggestion("X", 15, 1, 16, 1)); err == nil ||
			err.Error() != "source lines are not available" {
			t.Fatalf("nil WorkspaceSource error = %v, want historical error", err)
		}
	})

	t.Run("supplements missing lines from workspace", func(t *testing.T) {
		// Only the start line is known from the diff context; the end line is
		// missing and must be supplemented.
		source := map[int]string{15: "alpha bravo"}
		got, err := ws.SuggestionText(source, "ws.go", colSuggestion("X", 15, 7, 16, 9))
		if err != nil {
			t.Fatalf("SuggestionText() error = %v", err)
		}
		if want := "alpha Xdelta"; got != want {
			t.Errorf("SuggestionText() = %q, want %q", got, want)
		}
	})

	t.Run("diff lines take precedence and no read when complete", func(t *testing.T) {
		before := openCount.Load()
		source := map[int]string{15: "DIFF START", 16: "DIFF END"}
		got, err := ws.SuggestionText(source, "ws.go", colSuggestion("X", 15, 6, 16, 6))
		if err != nil {
			t.Fatalf("SuggestionText() error = %v", err)
		}
		if want := "DIFF XEND"; got != want {
			t.Errorf("SuggestionText() = %q, want %q", got, want)
		}
		if after := openCount.Load(); after != before {
			t.Errorf("workspace file opened %d time(s), want 0 (diff lines must win)", after-before)
		}
	})

	t.Run("same file is read only once per run", func(t *testing.T) {
		before := openCount.Load()
		for i := 0; i < 3; i++ {
			if _, err := ws.SuggestionText(map[int]string{15: "alpha bravo"}, "ws.go",
				colSuggestion("X", 15, 7, 16, 9)); err != nil {
				t.Fatalf("SuggestionText() error = %v", err)
			}
		}
		// ws.go was opened once by an earlier subtest; these calls must hit
		// the cache instead of opening the file again.
		if after := openCount.Load(); after != before {
			t.Errorf("workspace file opened %d time(s), want 0 (cached per run)", after-before)
		}
	})

	t.Run("normalized relative path variants share the cache", func(t *testing.T) {
		before := openCount.Load()
		if _, err := ws.SuggestionText(nil, "./sub/other.go",
			colSuggestion("X", 1, 6, 1, 6)); err != nil {
			t.Fatalf("SuggestionText() error = %v", err)
		}
		if _, err := ws.SuggestionText(nil, "sub/other.go",
			colSuggestion("X", 1, 6, 1, 6)); err != nil {
			t.Fatalf("SuggestionText() error = %v", err)
		}
		if after := openCount.Load(); after-before != 1 {
			t.Errorf("workspace file opened %d time(s), want 1", after-before)
		}
	})

	t.Run("other suggestions are unaffected by a missing file", func(t *testing.T) {
		if _, err := ws.SuggestionText(nil, "missing.go", colSuggestion("X", 1, 1, 1, 2)); err == nil {
			t.Fatal("SuggestionText() error = nil, want missing-file error")
		}
		got, err := ws.SuggestionText(nil, "sub/other.go", colSuggestion("Y", 1, 6, 1, 6))
		if err != nil {
			t.Fatalf("SuggestionText() error = %v", err)
		}
		if want := "only Yline"; got != want {
			t.Errorf("SuggestionText() = %q, want %q", got, want)
		}
	})
}

func TestWorkspaceSource_Errors(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "short.go", "a\nb\n")

	ws := NewWorkspaceSource(root)

	tests := []struct {
		name    string
		path    string
		s       *rdf.Suggestion
		wantErr string
	}{
		{
			name:    "missing file",
			path:    "does-not-exist.go",
			s:       colSuggestion("X", 1, 1, 2, 1),
			wantErr: `source file "does-not-exist.go" is not found in the workspace`,
		},
		{
			name:    "line out of range",
			path:    "short.go",
			s:       colSuggestion("X", 1, 1, 20, 1),
			wantErr: `source line (L=20) is out of range in workspace file "short.go"`,
		},
		{
			name:    "absolute path is not read",
			path:    filepath.Join(root, "short.go"),
			s:       colSuggestion("X", 1, 1, 1, 1),
			wantErr: "source lines are not available",
		},
		{
			name:    "path escaping workspace is not read",
			path:    "../secret.go",
			s:       colSuggestion("X", 1, 1, 1, 1),
			wantErr: "source lines are not available",
		},
		{
			name:    "empty path is not read",
			path:    "",
			s:       colSuggestion("X", 1, 1, 1, 1),
			wantErr: "source lines are not available",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ws.SuggestionText(nil, tt.path, tt.s)
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("SuggestionText() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestWorkspaceSource_MissingLineKeepsHistoricalError(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "f.go", "a\nb\n")
	ws := NewWorkspaceSource(root)

	// A partially populated diff map with a line-0 suggestion range is not a
	// readable workspace interval; the historical line error is kept.
	_, err := ws.SuggestionText(map[int]string{1: "a"}, "f.go",
		&rdf.Suggestion{Range: &rdf.Range{Start: &rdf.Position{Line: 0}}})
	if err == nil || err.Error() != "source line (L=0) is not available for this suggestion" {
		t.Fatalf("SuggestionText() error = %v, want historical line error", err)
	}
}

func TestWorkspaceSource_SymlinkOutsideIsNotRead(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeWorkspaceFile(t, outside, "secret.txt", "secret content\n")
	// Symlink inside the workspace pointing outside it.
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	// Sanity check: the symlink itself exists inside the workspace.
	if _, err := os.Lstat(filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}

	var opened atomic.Int32
	ws := NewWorkspaceSource(root)
	ws.openFile = func(path string) (io.ReadCloser, error) {
		opened.Add(1)
		return os.Open(path)
	}
	_, err := ws.SuggestionText(nil, "link.txt", colSuggestion("X", 1, 1, 1, 2))
	if err == nil || !strings.Contains(err.Error(), "outside the workspace") {
		t.Fatalf("SuggestionText() error = %v, want outside-workspace rejection", err)
	}
	if opened.Load() != 0 {
		t.Errorf("file was opened %d time(s), want 0", opened.Load())
	}
}

func TestWorkspaceSource_SymlinkInsideIsRead(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "real.txt", "hello world\n")
	if err := os.Symlink("real.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	ws := NewWorkspaceSource(root)
	got, err := ws.SuggestionText(nil, "link.txt", colSuggestion("X", 1, 7, 1, 7))
	if err != nil {
		t.Fatalf("SuggestionText() error = %v", err)
	}
	if want := "hello Xworld"; got != want {
		t.Errorf("SuggestionText() = %q, want %q", got, want)
	}
}

func TestWorkspaceSource_FailuresAreNotCached(t *testing.T) {
	root := t.TempDir()
	// A real file is required so path resolution succeeds; the injected
	// opener controls its actual contents.
	writeWorkspaceFile(t, root, "f.go", "placeholder\n")
	ws := NewWorkspaceSource(root)

	var attempts atomic.Int32
	ws.openFile = func(_ string) (io.ReadCloser, error) {
		n := attempts.Add(1)
		if n == 1 {
			// First attempt: the file is too short for the requested range.
			return io.NopCloser(strings.NewReader("a\nb\n")), nil
		}
		// Later attempt: the file now contains the requested lines.
		return io.NopCloser(strings.NewReader("a\nb\nc\nd\n")), nil
	}

	s := colSuggestion("X", 3, 1, 4, 1)
	if _, err := ws.SuggestionText(nil, "f.go", s); err == nil ||
		!strings.Contains(err.Error(), "out of range") {
		t.Fatalf("first SuggestionText() error = %v, want out-of-range error", err)
	}
	if _, err := ws.SuggestionText(nil, "f.go", s); err != nil {
		t.Fatalf("second SuggestionText() error = %v, failure must be retried, not cached", err)
	}
	if attempts.Load() != 2 {
		t.Errorf("open attempts = %d, want 2", attempts.Load())
	}
}

func TestWorkspaceSource_MissingFileIsRetried(t *testing.T) {
	root := t.TempDir()
	ws := NewWorkspaceSource(root)

	var attempts atomic.Int32
	ws.openFile = func(path string) (io.ReadCloser, error) {
		attempts.Add(1)
		return io.NopCloser(strings.NewReader("hello world\n")), nil
	}
	// The file is absent on the first attempt and created before the retry.
	s := colSuggestion("X", 1, 7, 1, 7)
	if _, err := ws.SuggestionText(nil, "f.go", s); err == nil ||
		!strings.Contains(err.Error(), "not found in the workspace") {
		t.Fatalf("first SuggestionText() error = %v, want not-found error", err)
	}
	writeWorkspaceFile(t, root, "f.go", "hello world\n")
	got, err := ws.SuggestionText(nil, "f.go", s)
	if err != nil {
		t.Fatalf("second SuggestionText() error = %v, want nil", err)
	}
	if want := "hello Xworld"; got != want {
		t.Errorf("SuggestionText() = %q, want %q", got, want)
	}
	// The failed attempt is not cached: the retry reaches the opener once.
	if attempts.Load() != 1 {
		t.Errorf("open attempts = %d, want 1", attempts.Load())
	}
}

func TestWorkspaceSource_OpenErrorIsNotMissingFile(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "f.go", "x\n")
	ws := NewWorkspaceSource(root)
	boom := errors.New("boom")
	ws.openFile = func(_ string) (io.ReadCloser, error) { return nil, boom }

	_, err := ws.SuggestionText(nil, "f.go", colSuggestion("X", 1, 1, 1, 1))
	if err == nil || !strings.Contains(err.Error(), "failed to read workspace source file") {
		t.Fatalf("SuggestionText() error = %v, want read-failure error", err)
	}
}
