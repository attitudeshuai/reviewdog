package project

import (
	"bytes"
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/reviewdog/reviewdog"
	"github.com/reviewdog/reviewdog/filter"
)

type fakeDiffService struct {
	reviewdog.DiffService
	FakeDiff func() ([]byte, error)
}

func (f *fakeDiffService) Diff(_ context.Context) ([]byte, error) {
	return f.FakeDiff()
}

func (f *fakeDiffService) Strip() int {
	return 0
}

type fakeCommentService struct {
	reviewdog.CommentService
	FakePost func(*reviewdog.Comment) error
}

func (f *fakeCommentService) Post(_ context.Context, c *reviewdog.Comment) error {
	return f.FakePost(c)
}

func (*fakeCommentService) ShouldPrependGitRelDir() bool { return false }

type fakeNamedCommentService struct {
	fakeCommentService
	tools []string
}

func (f *fakeNamedCommentService) SetTool(toolName, _ string) {
	// project.Run reports results sequentially; recording the SetTool calls
	// captures the order in which reports are built, independent of when each
	// command finished.
	f.tools = append(f.tools, toolName)
}

func TestRun(t *testing.T) {
	ctx := context.Background()

	t.Run("empty", func(t *testing.T) {
		conf := &Config{}
		if err := Run(ctx, conf, nil, nil, nil, false, filter.ModeAdded, reviewdog.FailLevelNone); err != nil {
			t.Error(err)
		}
	})

	t.Run("errorformat error", func(t *testing.T) {
		conf := &Config{
			Runner: map[string]*Runner{
				"test": {},
			},
		}
		if err := Run(ctx, conf, nil, nil, nil, false, filter.ModeAdded, reviewdog.FailLevelNone); err == nil {
			t.Error("want error, got nil")
		} else {
			t.Log(err)
		}
	})

	t.Run("diff error", func(t *testing.T) {
		ds := &fakeDiffService{
			FakeDiff: func() ([]byte, error) {
				return nil, errors.New("err!")
			},
		}
		conf := &Config{
			Runner: map[string]*Runner{
				"test": {
					Cmd:         "echo 'hi'",
					Errorformat: []string{`%f:%l:%c:%m`},
				},
			},
		}
		if err := Run(ctx, conf, nil, nil, ds, false, filter.ModeAdded, reviewdog.FailLevelNone); err == nil {
			t.Error("want error, got nil")
		} else {
			t.Log(err)
		}
	})

	t.Run("cmd error with findings (not for reviewdog to exit with error)", func(t *testing.T) {
		buf := new(bytes.Buffer)
		defaultTeeStderr = buf
		ds := &fakeDiffService{
			FakeDiff: func() ([]byte, error) {
				return []byte(""), nil
			},
		}
		cs := &fakeCommentService{
			FakePost: func(c *reviewdog.Comment) error {
				return nil
			},
		}
		conf := &Config{
			Runner: map[string]*Runner{
				"test": {
					Cmd:         "echo 'file:14:14:message'; exit 1",
					Errorformat: []string{`%f:%l:%c:%m`},
				},
			},
		}
		if err := Run(ctx, conf, nil, cs, ds, false, filter.ModeAdded, reviewdog.FailLevelNone); err != nil {
			t.Error(err)
		}
		want := ""
		if got := buf.String(); got != want {
			t.Errorf("got stderr %q, want %q", got, want)
		}
	})

	t.Run("unexpected cmd error (reviewdog exits with error)", func(t *testing.T) {
		buf := new(bytes.Buffer)
		defaultTeeStderr = buf
		ds := &fakeDiffService{
			FakeDiff: func() ([]byte, error) {
				return []byte(""), nil
			},
		}
		cs := &fakeCommentService{
			FakePost: func(c *reviewdog.Comment) error {
				return nil
			},
		}
		conf := &Config{
			Runner: map[string]*Runner{
				"test": {
					Cmd:         "not found",
					Errorformat: []string{`%f:%l:%c:%m`},
				},
			},
		}
		if err := Run(ctx, conf, nil, cs, ds, false, filter.ModeAdded, reviewdog.FailLevelNone); err == nil {
			t.Error("want error, got nil")
		} else {
			t.Log(err)
		}
	})

	t.Run("cmd error with tee", func(t *testing.T) {
		buf := new(bytes.Buffer)
		defaultTeeStderr = buf
		ds := &fakeDiffService{
			FakeDiff: func() ([]byte, error) {
				return []byte(""), nil
			},
		}
		cs := &fakeCommentService{
			FakePost: func(c *reviewdog.Comment) error {
				return nil
			},
		}
		conf := &Config{
			Runner: map[string]*Runner{
				"test": {
					Cmd:         "not-found",
					Errorformat: []string{`%f:%l:%c:%m`},
				},
			},
		}
		if err := Run(ctx, conf, nil, cs, ds, true, filter.ModeAdded, reviewdog.FailLevelNone); err == nil {
			t.Error("want error, got nil")
		} else {
			t.Log(err)
		}
		var want string
		if runtime.GOOS == "darwin" {
			want = "sh: not-found: command not found\n"
		} else {
			want = "sh: 1: not-found: not found\n"
		}
		if got := buf.String(); got != want {
			t.Errorf("got stderr %q, want %q", got, want)
		}
	})

	t.Run("success", func(t *testing.T) {
		ds := &fakeDiffService{
			FakeDiff: func() ([]byte, error) {
				return []byte(""), nil
			},
		}
		cs := &fakeCommentService{
			FakePost: func(c *reviewdog.Comment) error {
				return nil
			},
		}
		conf := &Config{
			Runner: map[string]*Runner{
				"test": {
					Cmd:         "echo 'hi'",
					Errorformat: []string{`%f:%l:%c:%m`},
				},
			},
		}
		if err := Run(ctx, conf, nil, cs, ds, false, filter.ModeAdded, reviewdog.FailLevelNone); err != nil {
			t.Error(err)
		}
	})

	t.Run("success with tee", func(t *testing.T) {
		buf := new(bytes.Buffer)
		defaultTeeStdout = buf
		ds := &fakeDiffService{
			FakeDiff: func() ([]byte, error) {
				return []byte(""), nil
			},
		}
		cs := &fakeCommentService{
			FakePost: func(c *reviewdog.Comment) error {
				return nil
			},
		}
		conf := &Config{
			Runner: map[string]*Runner{
				"test": {
					Cmd:         "echo 'hi'",
					Errorformat: []string{`%f:%l:%c:%m`},
				},
			},
		}
		if err := Run(ctx, conf, nil, cs, ds, true, filter.ModeAdded, reviewdog.FailLevelNone); err != nil {
			t.Error(err)
		}
		want := "hi\n"
		if got := buf.String(); got != want {
			t.Errorf("got stdout %q, want %q", got, want)
		}
	})

	t.Run("runners", func(t *testing.T) {
		called := 0
		ds := &fakeDiffService{
			FakeDiff: func() ([]byte, error) {
				called++
				return []byte(""), nil
			},
		}
		cs := &fakeCommentService{
			FakePost: func(c *reviewdog.Comment) error {
				return nil
			},
		}
		conf := &Config{
			Runner: map[string]*Runner{
				"test1": {
					Name:        "test1",
					Cmd:         "echo 'test1'",
					Errorformat: []string{`%f:%l:%c:%m`},
				},
				"test2": {
					Name:        "test2",
					Cmd:         "echo 'test2'",
					Errorformat: []string{`%f:%l:%c:%m`},
				},
			},
		}
		if err := Run(ctx, conf, map[string]bool{"test2": true}, cs, ds, false, filter.ModeAdded, reviewdog.FailLevelNone); err != nil {
			t.Error(err)
		}
		if called != 1 {
			t.Errorf("Diff service called %d times, want 1 time", called)
		}
	})

	t.Run("unknown runners", func(t *testing.T) {
		ds := &fakeDiffService{
			FakeDiff: func() ([]byte, error) {
				return []byte(""), nil
			},
		}
		cs := &fakeCommentService{
			FakePost: func(c *reviewdog.Comment) error {
				return nil
			},
		}
		conf := &Config{
			Runner: map[string]*Runner{
				"test1": {
					Name:        "test1",
					Cmd:         "echo 'test1'",
					Errorformat: []string{`%f:%l:%c:%m`},
				},
				"test2": {
					Name:        "test2",
					Cmd:         "echo 'test2'",
					Errorformat: []string{`%f:%l:%c:%m`},
				},
			},
		}
		if err := Run(ctx, conf, map[string]bool{"hoge": true}, cs, ds, false, filter.ModeAdded, reviewdog.FailLevelNone); err == nil {
			t.Error("got no error but want runner not found error")
		}
	})

	// #1934
	t.Run("big stderr", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("requires POSIX shell utilities")
		}

		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		conf := &Config{
			Runner: map[string]*Runner{
				"test": {
					Cmd:         "yes x | head -c 131072 >&2; echo 'file:1:1:message'",
					Errorformat: []string{`%f:%l:%c:%m`},
				},
			},
		}

		results, err := RunAndParse(ctx, conf, nil, "", false)
		if err != nil {
			t.Fatalf("RunAndParse failed: %v", err)
		}

		result, err := results.Load("test")
		if err != nil {
			t.Fatalf("failed to load result: %v", err)
		}
		if result.CmdErr != nil {
			t.Fatalf("unexpected command error: %v", result.CmdErr)
		}
		if len(result.Diagnostics) != 1 {
			t.Fatalf("got %d diagnostics, want 1", len(result.Diagnostics))
		}
	})
}

func TestFilteredEnviron(t *testing.T) {
	names := [...]string{
		"REVIEWDOG_GITHUB_API_TOKEN",
		"REVIEWDOG_GITLAB_API_TOKEN",
		"REVIEWDOG_TOKEN",
	}

	for _, name := range names {
		defer func(name, value string) {
			os.Setenv(name, value)
		}(name, os.Getenv(name))
		os.Setenv(name, "value")
	}

	filtered := filteredEnviron()
	if len(filtered) != len(os.Environ())-len(names) {
		t.Errorf("len(filtered) != len(os.Environ())-%d, %v != %v-%d", len(names), len(filtered), len(os.Environ()), len(names))
	}

	for _, kv := range filtered {
		for _, name := range names {
			if strings.HasPrefix(kv, name) && kv != name+"=" {
				t.Errorf("filtered: %v, want %v=", kv, name)
			}
		}
	}

	for _, kv := range os.Environ() {
		for _, name := range names {
			if strings.HasPrefix(kv, name) && kv != name+"=value" {
				t.Errorf("envs: %v, want %v=value", kv, name)
			}
		}
	}
}

func TestRunAndParseDuplicateRunnerName(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		runners map[string]*Runner
		wantErr string
	}{
		{
			name: "explicit name collision",
			runners: map[string]*Runner{
				// Give the config keys in reverse message order to prove the
				// error names them deterministically.
				"key-b": {Name: "dup", Cmd: "echo b", Errorformat: []string{`%f:%l:%c:%m`}},
				"key-a": {Name: "dup", Cmd: "echo a", Errorformat: []string{`%f:%l:%c:%m`}},
			},
			wantErr: `duplicate runner name "dup": config keys "key-a" and "key-b" resolve to the same name`,
		},
		{
			name: "explicit name collides with another config key",
			runners: map[string]*Runner{
				"other": {Name: "alpha", Cmd: "echo o", Errorformat: []string{`%f:%l:%c:%m`}},
				"alpha": {Cmd: "echo a", Errorformat: []string{`%f:%l:%c:%m`}},
			},
			wantErr: `duplicate runner name "alpha": config keys "alpha" and "other" resolve to the same name`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := &Config{Runner: tt.runners}
			_, err := RunAndParse(ctx, conf, nil, "", false)
			if err == nil {
				t.Fatal("want duplicate runner name error, got nil")
			}
			if err.Error() != tt.wantErr {
				t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestRunAndParseDuplicateRunnerNameFailsBeforeStartingCommands(t *testing.T) {
	ctx := context.Background()
	conf := &Config{Runner: map[string]*Runner{
		// Commands that would hang if they were ever started.
		"key-a": {Name: "dup", Cmd: "cat", Errorformat: []string{`%f:%l:%c:%m`}},
		"key-b": {Name: "dup", Cmd: "cat", Errorformat: []string{`%f:%l:%c:%m`}},
	}}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := RunAndParse(ctx, conf, nil, "", false)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "duplicate runner name") {
			t.Fatalf("want duplicate runner name error before commands start, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunAndParse hung; commands must not start when runner names conflict")
	}
}

func TestCheckUnknownRunnerSorted(t *testing.T) {
	err := checkUnknownRunner(map[string]bool{"c": true, "a": true, "b": true}, nil)
	if err == nil {
		t.Fatal("want runner not found error, got nil")
	}
	if got, want := err.Error(), "runner not found: [a,b,c]"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}

	if err := checkUnknownRunner(map[string]bool{"a": true}, []string{"a"}); err != nil {
		t.Errorf("all specified runners used: got %v, want nil", err)
	}
	if err := checkUnknownRunner(nil, nil); err != nil {
		t.Errorf("no specified runners: got %v, want nil", err)
	}
}

func TestRunAndParseResultOrderIndependentOfCompletionOrder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX sleep utility")
	}

	conf := &Config{Runner: map[string]*Runner{
		// "alpha" finishes well after "zeta" when jobs run in parallel.
		"slow-config-key": {
			Name:        "alpha",
			Cmd:         "sleep 0.3; echo 'slow:1:1:done'",
			Errorformat: []string{`%f:%l:%c:%m`},
		},
		"fast-config-key": {
			Name:        "zeta",
			Cmd:         "echo 'fast:1:1:done'",
			Errorformat: []string{`%f:%l:%c:%m`},
		},
	}}

	want := []string{"alpha", "zeta"}
	// Running the same input twice must always yield the same name order.
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		results, err := RunAndParse(ctx, conf, nil, "", false)
		cancel()
		if err != nil {
			t.Fatalf("RunAndParse failed: %v", err)
		}
		var got []string
		results.Range(func(name string, _ *reviewdog.Result) {
			got = append(got, name)
		})
		if len(got) != len(want) {
			t.Fatalf("got %d results %v, want %d %v", len(got), got, len(want), want)
		}
		if !stringsEqual(got, want) {
			t.Errorf("run %d: result order = %v, want %v (must not depend on completion order)", i+1, got, want)
		}
	}
}

func TestRunReportOrderIndependentOfCompletionOrder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX sleep utility")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ds := &fakeDiffService{
		FakeDiff: func() ([]byte, error) {
			return []byte(""), nil
		},
	}
	cs := &fakeNamedCommentService{
		fakeCommentService: fakeCommentService{
			FakePost: func(_ *reviewdog.Comment) error { return nil },
		},
	}
	conf := &Config{Runner: map[string]*Runner{
		"slow-config-key": {
			Name:        "alpha",
			Cmd:         "sleep 0.3",
			Errorformat: []string{`%f:%l:%c:%m`},
		},
		"fast-config-key": {
			Name:        "zeta",
			Cmd:         "echo done",
			Errorformat: []string{`%f:%l:%c:%m`},
		},
	}}

	if err := Run(ctx, conf, nil, cs, ds, false, filter.ModeAdded, reviewdog.FailLevelNone); err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "zeta"}
	if !stringsEqual(cs.tools, want) {
		t.Errorf("report order = %v, want %v (must be sorted by name, not completion time)", cs.tools, want)
	}
}

func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
