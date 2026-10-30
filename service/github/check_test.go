package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-github/v93/github"
	"github.com/reviewdog/reviewdog"
	"github.com/reviewdog/reviewdog/diff"
	"github.com/reviewdog/reviewdog/filter"
	"github.com/reviewdog/reviewdog/proto/rdf"
)

// newGitHubClient creates a GitHub client for testing purposes.
func newGitHubClient(t *testing.T, serverURL string) *github.Client {
	t.Helper()
	baseURL, err := url.Parse(serverURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	cli, err := github.NewClient(github.WithURLs(new(baseURL.String()), nil))
	if err != nil {
		t.Fatal(err)
	}
	return cli
}

func TestCheck_OK(t *testing.T) {
	const (
		name        = "haya14busa-linter"
		owner       = "haya14busa"
		repo        = "reviewdog"
		prNum       = 14
		sha         = "1414"
		reportURL   = "http://example.com/report_url"
		conclusion  = "neutral"
		level       = "warning"
		wantCheckID = 1414
	)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	err = os.Chdir("../../diff/testdata")
	if err != nil {
		t.Fatal(err)
	}

	comments := []*reviewdog.Comment{
		{
			Result: &filter.FilteredDiagnostic{
				Diagnostic: &rdf.Diagnostic{
					Message: "test message",
					Location: &rdf.Location{
						Path: "sample.new.txt",
						Range: &rdf.Range{
							Start: &rdf.Position{Line: 2, Column: 1},
						},
					},
					OriginalOutput: "raw test message",
				},
				ShouldReport: true,
			},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/haya14busa/reviewdog/check-runs", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(&github.CheckRun{ID: new(int64(wantCheckID))}); err != nil {
			t.Fatal(err)
		}
	})
	mux.HandleFunc("/repos/haya14busa/reviewdog/check-runs/1414", func(w http.ResponseWriter, r *http.Request) {
		var req github.CheckRun
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}

		if req.GetStatus() == "completed" {
			if wantTitle := "reviewdog [haya14busa-linter] report"; req.GetOutput().GetTitle() != wantTitle {
				t.Errorf("title = %s, want %s", req.GetOutput().GetTitle(), wantTitle)
			}
		}
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	cli := newGitHubClient(t, ts.URL)

	check, err := NewGitHubCheck(cli, owner, repo, prNum, sha, level, name)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range comments {
		if c.Result.ShouldReport {
			if err := check.Post(context.Background(), c); err != nil {
				t.Errorf("failed to post: error=%v\ncomment=%v", err, c)
			}
		} else {
			if err := check.PostFiltered(context.Background(), c); err != nil {
				t.Errorf("failed to post: error=%v\ncomment=%v", err, c)
			}
		}
	}
	if err := check.Flush(context.Background()); err != nil {
		t.Error(err)
	}
	if check.GetResult().Conclusion != conclusion {
		t.Errorf("conclusion = %s, want %s", check.GetResult().Conclusion, conclusion)
	}
}

func TestCheck_OK_multiple_update_runs(t *testing.T) {
	const (
		name        = "haya14busa-linter"
		owner       = "haya14busa"
		repo        = "reviewdog"
		prNum       = 14
		sha         = "1414"
		reportURL   = "http://example.com/report_url"
		conclusion  = "neutral"
		level       = "warning"
		wantCheckID = 1414
	)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	err = os.Chdir("../../diff/testdata")
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/haya14busa/reviewdog/check-runs", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(&github.CheckRun{ID: new(int64(wantCheckID))}); err != nil {
			t.Fatal(err)
		}
	})
	mux.HandleFunc("/repos/haya14busa/reviewdog/check-runs/1414", func(w http.ResponseWriter, r *http.Request) {
		var req github.CheckRun
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		switch len(req.GetOutput().Annotations) {
		case 0:
			if req.GetConclusion() != conclusion {
				t.Errorf("UpdateCheckRunOptions.Conclusion = %q, want %q", req.GetConclusion(), conclusion)
			}
		case maxAnnotationsPerRequest, 1: // Expected
		default:
			t.Errorf("UpdateCheckRun: len(annotations) = %d, but it's unexpected", len(req.GetOutput().Annotations))
		}
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	cli := newGitHubClient(t, ts.URL)

	check, err := NewGitHubCheck(cli, owner, repo, prNum, sha, level, name)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 101; i++ {
		if err := check.Post(context.Background(), &reviewdog.Comment{
			Result: &filter.FilteredDiagnostic{
				Diagnostic: &rdf.Diagnostic{
					Message: "test message",
					Location: &rdf.Location{
						Path: "sample.new.txt",
						Range: &rdf.Range{
							Start: &rdf.Position{Line: 2},
						},
					},
					OriginalOutput: "raw test message",
				},
				ShouldReport: true,
			},
		}); err != nil {
			t.Error(err)
		}
	}
	if err := check.Flush(context.Background()); err != nil {
		t.Error(err)
	}
}

func TestCheck_fail_check_with_403_in_GitHub_Actions(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	const (
		name        = "haya14busa-linter"
		owner       = "haya14busa"
		repo        = "reviewdog"
		prNum       = 14
		sha         = "1414"
		reportURL   = "http://example.com/report_url"
		conclusion  = "neutral"
		level       = "warning"
		wantCheckID = 1414
	)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	err = os.Chdir("../../diff/testdata")
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/haya14busa/reviewdog/check-runs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	cli := newGitHubClient(t, ts.URL)

	check, err := NewGitHubCheck(cli, owner, repo, prNum, sha, level, name)
	if err != nil {
		t.Fatal(err)
	}
	if err := check.Flush(context.Background()); err != nil {
		t.Error(err)
	}
}

func TestCheck_summary_too_many_findings_cut_off_correctly(t *testing.T) {
	check := &Check{}
	var comment []*reviewdog.Comment
	for i := 0; i < 1000; i++ {
		comment = append(comment, &reviewdog.Comment{
			Result: &filter.FilteredDiagnostic{
				ShouldReport: true,
				Diagnostic: &rdf.Diagnostic{
					Message: "this is a pretty long test message that will lead to overshooting the maximum allowed size",
				},
			}})
	}
	summaryText := check.summary(comment)
	if len(summaryText) > maxAllowedSize {
		t.Errorf("summary text is %d bytes long, but the maximum allowed size is %d", len(summaryText), maxAllowedSize)
	}
	if !strings.Contains(summaryText, "... (Too many findings. Dropped some findings)\n</details>") {
		t.Error("summary text was not cut off correctly")
	}
}

func TestCheck_setToolNameForEachRun(t *testing.T) {
	const (
		owner       = "haya14busa"
		repo        = "reviewdog"
		prNum       = 14
		sha         = "1414"
		reportURL   = "http://example.com/report_url"
		conclusion  = "neutral"
		wantCheckID = 1414
		toolName1   = "toolName1"
		level1      = "warning"
		toolName2   = "toolName2"
		level2      = ""
	)

	mux := http.NewServeMux()
	checkRunNum := 0
	mux.HandleFunc("/repos/haya14busa/reviewdog/check-runs", func(w http.ResponseWriter, r *http.Request) {
		checkRunNum++
		var req github.CreateCheckRunOptions
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		switch checkRunNum {
		case 1:
			if req.Name != toolName1 {
				t.Errorf("toolName = %s, want %s", req.Name, toolName1)
			}
		case 2:
			if req.Name != toolName2 {
				t.Errorf("toolName = %s, want %s", req.Name, toolName2)
			}
		}
		if err := json.NewEncoder(w).Encode(&github.CheckRun{ID: new(int64(wantCheckID))}); err != nil {
			t.Fatal(err)
		}
	})
	updateCheckRunNum := 0
	mux.HandleFunc("/repos/haya14busa/reviewdog/check-runs/1414", func(w http.ResponseWriter, r *http.Request) {
		var req github.CheckRun
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		switch updateCheckRunNum {
		case 0:
			if req.GetName() != toolName1 {
				t.Errorf("toolName = %s, want %s", req.GetName(), toolName1)
			}
			wantAnnotations := []*github.CheckRunAnnotation{
				{
					Path:            new("sample.new.txt"),
					StartLine:       new(2),
					EndLine:         new(2),
					AnnotationLevel: new("warning"),
					Message:         new("comment 1"),
					Title:           new("[toolName1] sample.new.txt#L2"),
				},
			}
			if req.GetStatus() == "completed" {
				if wantTitle := "reviewdog [toolName1] report"; req.GetOutput().GetTitle() != wantTitle {
					t.Errorf("title = %s, want %s", req.GetOutput().GetTitle(), wantTitle)
				}
			} else {
				if d := cmp.Diff(req.Output.Annotations, wantAnnotations); d != "" {
					t.Errorf("Annotation diff found:\n%s", d)
				}
			}
		case 1:
			if req.GetName() != toolName2 {
				t.Errorf("toolName = %s, want %s", req.GetName(), toolName2)
			}
			wantAnnotations := []*github.CheckRunAnnotation{
				{
					Path:            new("sample.new.txt"),
					StartLine:       new(2),
					EndLine:         new(2),
					AnnotationLevel: new("failure"), // default
					Message:         new("comment 2"),
					Title:           new("[toolName2] sample.new.txt#L2"),
				},
			}
			if req.GetStatus() == "completed" {
				if wantTitle := "reviewdog [toolName2] report"; req.GetOutput().GetTitle() != wantTitle {
					t.Errorf("title = %s, want %s", req.GetOutput().GetTitle(), wantTitle)
				}
			} else {
				if d := cmp.Diff(req.Output.Annotations, wantAnnotations); d != "" {
					t.Errorf("Annotation diff found:\n%s", d)
				}
			}
		}
		if req.GetStatus() == "completed" {
			updateCheckRunNum++
		}
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	cli := newGitHubClient(t, ts.URL)

	check := &Check{
		CLI:      cli,
		Owner:    owner,
		Repo:     repo,
		PR:       prNum,
		SHA:      sha,
		ToolName: "", // empty
		Level:    "", // empty
	}

	check.SetTool(toolName1, level1)
	if err := check.Post(context.Background(), &reviewdog.Comment{
		Result: &filter.FilteredDiagnostic{
			Diagnostic: &rdf.Diagnostic{
				Message: "comment 1",
				Location: &rdf.Location{
					Path: "sample.new.txt",
					Range: &rdf.Range{
						Start: &rdf.Position{Line: 2, Column: 1},
					},
				},
			},
			ShouldReport: true,
		},
	}); err != nil {
		t.Errorf("failed to post: %v", err)
	}
	if err := check.Flush(context.Background()); err != nil {
		t.Error(err)
	}

	check.SetTool(toolName2, level2)
	if err := check.Post(context.Background(), &reviewdog.Comment{
		Result: &filter.FilteredDiagnostic{
			Diagnostic: &rdf.Diagnostic{
				Message: "comment 2",
				Location: &rdf.Location{
					Path: "sample.new.txt",
					Range: &rdf.Range{
						Start: &rdf.Position{Line: 2, Column: 1},
					},
				},
			},
			ShouldReport: true,
		},
	}); err != nil {
		t.Errorf("failed to post: %v", err)
	}
	if err := check.Flush(context.Background()); err != nil {
		t.Error(err)
	}
}

func TestCheck_update_retried_after_404(t *testing.T) {
	defer func(d time.Duration) { checkRunRetryDelay = d }(checkRunRetryDelay)
	checkRunRetryDelay = time.Millisecond

	for _, tc := range []struct {
		name     string
		notFound int
		wantErr  bool
	}{
		{"recovers", 2, false},
		{"gives up", checkRunUpdateAttempts, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var patches int
			mux := http.NewServeMux()
			mux.HandleFunc("/repos/o/r/check-runs", func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewEncoder(w).Encode(&github.CheckRun{ID: new(int64(1414))}); err != nil {
					t.Error(err)
				}
			})
			mux.HandleFunc("/repos/o/r/check-runs/1414", func(w http.ResponseWriter, r *http.Request) {
				patches++
				if patches <= tc.notFound {
					http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
					return
				}
				if err := json.NewEncoder(w).Encode(&github.CheckRun{ID: new(int64(1414))}); err != nil {
					t.Error(err)
				}
			})
			ts := httptest.NewServer(mux)
			defer ts.Close()

			check, err := NewGitHubCheck(newGitHubClient(t, ts.URL), "o", "r", 1, "sha", "", "tool")
			if err != nil {
				t.Fatal(err)
			}
			err = check.Flush(context.Background())
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("Flush() error = %v, want error %v", err, tc.wantErr)
			}
			if want := min(tc.notFound+1, checkRunUpdateAttempts); patches != want {
				t.Errorf("PATCH requests = %d, want %d", patches, want)
			}
		})
	}
}

func TestCheck_toCheckRunAnnotation_columns(t *testing.T) {
	ch := &Check{ToolName: "tool"}
	tests := []struct {
		name          string
		sourceLines   map[int]string
		rng           *rdf.Range
		wantStartLine int
		wantEndLine   int
		wantStartCol  *int
		wantEndCol    *int
	}{
		{
			name:        "in-bounds ascii columns attached",
			sourceLines: map[int]string{1: "hello world"},
			rng: &rdf.Range{
				Start: &rdf.Position{Line: 1, Column: 7},
				End:   &rdf.Position{Line: 1, Column: 12},
			},
			wantStartLine: 1, wantEndLine: 1,
			wantStartCol: new(7), wantEndCol: new(12),
		},
		{
			name:        "out-of-bounds columns clamp to line end",
			sourceLines: map[int]string{1: "abc"},
			rng: &rdf.Range{
				Start: &rdf.Position{Line: 1, Column: 100},
				End:   &rdf.Position{Line: 1, Column: 200},
			},
			wantStartLine: 1, wantEndLine: 1,
			// "abc" is 3 bytes: both columns land at the line-end cursor (4).
			wantStartCol: new(4), wantEndCol: new(4),
		},
		{
			name:        "columns inside multi-byte runes expand outward",
			sourceLines: map[int]string{1: "あいう"},
			rng: &rdf.Range{
				Start: &rdf.Position{Line: 1, Column: 2},
				End:   &rdf.Position{Line: 1, Column: 5},
			},
			wantStartLine: 1, wantEndLine: 1,
			wantStartCol: new(1), wantEndCol: new(7),
		},
		{
			name:        "no source content falls back to line-level",
			sourceLines: map[int]string{},
			rng: &rdf.Range{
				Start: &rdf.Position{Line: 1, Column: 100},
				End:   &rdf.Position{Line: 1, Column: 200},
			},
			wantStartLine: 1, wantEndLine: 1,
			wantStartCol: nil, wantEndCol: nil,
		},
		{
			name:        "start column only is line-level",
			sourceLines: map[int]string{1: "abc"},
			rng: &rdf.Range{
				Start: &rdf.Position{Line: 1, Column: 2},
			},
			wantStartLine: 1, wantEndLine: 1,
			wantStartCol: nil, wantEndCol: nil,
		},
		{
			name:        "multi-line range is line-level",
			sourceLines: map[int]string{1: "abc", 2: "def"},
			rng: &rdf.Range{
				Start: &rdf.Position{Line: 1, Column: 1},
				End:   &rdf.Position{Line: 2, Column: 4},
			},
			wantStartLine: 1, wantEndLine: 2,
			wantStartCol: nil, wantEndCol: nil,
		},
		{
			name:        "inverted interval is line-level",
			sourceLines: map[int]string{1: "hello"},
			rng: &rdf.Range{
				Start: &rdf.Position{Line: 1, Column: 5},
				End:   &rdf.Position{Line: 1, Column: 2},
			},
			wantStartLine: 1, wantEndLine: 1,
			wantStartCol: nil, wantEndCol: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fd := &filter.FilteredDiagnostic{
				SourceLines: tt.sourceLines,
				Diagnostic: &rdf.Diagnostic{
					Message:  "msg",
					Location: &rdf.Location{Path: "f.txt", Range: tt.rng},
				},
			}
			a := ch.toCheckRunAnnotation(fd)
			if got := a.GetStartLine(); got != tt.wantStartLine {
				t.Errorf("StartLine = %d, want %d", got, tt.wantStartLine)
			}
			if got := a.GetEndLine(); got != tt.wantEndLine {
				t.Errorf("EndLine = %d, want %d", got, tt.wantEndLine)
			}
			if got := a.StartColumn; !ptrEqual(got, tt.wantStartCol) {
				t.Errorf("StartColumn = %v, want %v", got, tt.wantStartCol)
			}
			if got := a.EndColumn; !ptrEqual(got, tt.wantEndCol) {
				t.Errorf("EndColumn = %v, want %v", got, tt.wantEndCol)
			}
		})
	}
}

func ptrEqual(got, want *int) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

// TestCheck_annotationColumnsConsistentWithDiagnostic ensures that when source
// line content is available, the columns sent on a check-run annotation are
// exactly the normalized columns the diagnostic carries (i.e. the same
// columns the local summary and structured outputs report).
func TestCheck_annotationColumnsConsistentWithDiagnostic(t *testing.T) {
	filediffs, _ := diff.ParseMultiFile(strings.NewReader(diffContentForCheck))
	diagnostic := &rdf.Diagnostic{
		Message: "msg",
		Location: &rdf.Location{
			Path: "sample.new.txt",
			Range: &rdf.Range{
				Start: &rdf.Position{Line: 2, Column: 50},
				End:   &rdf.Position{Line: 2, Column: 60},
			},
		},
	}
	checks := filter.FilterCheck([]*rdf.Diagnostic{diagnostic}, filediffs, 0, "", filter.ModeAdded)
	fd := checks[0]

	a := (&Check{ToolName: "tool"}).toCheckRunAnnotation(fd)
	rng := fd.Diagnostic.GetLocation().GetRange()
	if a.GetStartColumn() != int(rng.GetStart().GetColumn()) {
		t.Errorf("annotation start column = %d, diagnostic column = %d",
			a.GetStartColumn(), rng.GetStart().GetColumn())
	}
	if a.GetEndColumn() != int(rng.GetEnd().GetColumn()) {
		t.Errorf("annotation end column = %d, diagnostic column = %d",
			a.GetEndColumn(), rng.GetEnd().GetColumn())
	}
	// "added line" is 10 bytes, so columns must be in bounds: 11 is the
	// line-end cursor.
	if a.GetStartColumn() != 11 || a.GetEndColumn() != 11 {
		t.Errorf("columns = (%d,%d), want (11,11)", a.GetStartColumn(), a.GetEndColumn())
	}
}

const diffContentForCheck = `--- sample.old.txt	2016-10-13 05:09:35.820791185 +0900
+++ sample.new.txt	2016-10-13 05:15:26.839245048 +0900
@@ -1,3 +1,4 @@
 unchanged, contextual line
-deleted line
+added line
+added line
 unchanged, contextual line
`
