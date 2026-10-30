package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reviewdog/reviewdog/commands"
	"github.com/reviewdog/reviewdog/filter"
)

func TestValidateLevel(t *testing.T) {
	t.Run("unrecognized level errors and points to the flag", func(t *testing.T) {
		opt := &option{level: "loud"}
		err := validateLevel(opt)
		if err == nil {
			t.Fatal("validateLevel() error = nil, want error")
		}
		if !strings.Contains(err.Error(), "-level") {
			t.Errorf("error %q does not identify the -level flag as the source", err)
		}
		if !strings.Contains(err.Error(), "loud") {
			t.Errorf("error %q does not mention the invalid value", err)
		}
	})

	t.Run("recognized and empty levels are canonicalized", func(t *testing.T) {
		for _, tt := range []struct {
			in   string
			want string
		}{
			{"", ""},
			{"   ", ""},
			{"error", "error"},
			{" Warning ", "warning"},
			{"FATAL", "error"},
			{"提示", "info"},
		} {
			opt := &option{level: tt.in}
			if err := validateLevel(opt); err != nil {
				t.Errorf("validateLevel(%q) unexpected error: %v", tt.in, err)
			}
			if opt.level != tt.want {
				t.Errorf("validateLevel(%q) canonicalized to %q, want %q", tt.in, opt.level, tt.want)
			}
		}
	})
}

func TestRun_invalidLevel(t *testing.T) {
	opt := &option{
		efms:       strslice([]string{`%f:%l:%c:%m`}),
		reporter:   "local",
		level:      "loud",
		filterMode: filter.ModeNoFilter,
	}
	err := run(strings.NewReader(""), new(bytes.Buffer), opt)
	if err == nil {
		t.Fatal("run() error = nil, want invalid -level error at startup")
	}
	if !strings.Contains(err.Error(), "-level") {
		t.Errorf("error %q does not identify the -level flag as the source", err)
	}
}

func TestRun_local(t *testing.T) {
	const (
		before = `line1
line2
line3
`
		after = `line1
line2 changed
line3
`
	)

	beforef, err := os.CreateTemp("", "reviewdog-test")
	if err != nil {
		t.Fatal(err)
	}
	defer beforef.Close()
	defer os.Remove(beforef.Name())
	afterf, err := os.CreateTemp("", "reviewdog-test")
	if err != nil {
		t.Fatal(err)
	}
	defer afterf.Close()
	defer os.Remove(afterf.Name())

	beforef.WriteString(before)
	afterf.WriteString(after)

	fname := afterf.Name()

	var (
		stdin = strings.Join([]string{
			fname + "(2,1): message1",
			fname + "(2): message2",
			fname + "(14,1): message3",
		}, "\n")
		want = fname + "(2,1): message1"
	)

	diffCmd := fmt.Sprintf("diff -u %s %s", filepath.ToSlash(beforef.Name()), filepath.ToSlash(afterf.Name()))

	opt := &option{
		diffCmd:   diffCmd,
		efms:      strslice([]string{`%f(%l,%c): %m`}),
		diffStrip: 0,
		reporter:  "local",
	}

	stdout := new(bytes.Buffer)
	if err := run(strings.NewReader(stdin), stdout, opt); err != nil {
		t.Error(err)
	}

	if got := strings.Trim(stdout.String(), "\n"); got != want {
		t.Errorf("raw: got %v, want %v", got, want)
	}

}

func TestRun_local_nofilter(t *testing.T) {
	var (
		stdin = strings.Join([]string{
			"/path/to/file(2,1): message1",
			"/path/to/file(2): message2",
			"/path/to/file(14,1): message3",
		}, "\n")
		want = `/path/to/file(2,1): message1
/path/to/file(14,1): message3`
	)

	opt := &option{
		diffCmd:    "", // empty
		efms:       strslice([]string{`%f(%l,%c): %m`}),
		diffStrip:  0,
		reporter:   "local",
		filterMode: filter.ModeAdded,
	}

	stdout := new(bytes.Buffer)
	if err := run(strings.NewReader(stdin), stdout, opt); err == nil {
		t.Errorf("got no error, but want error")
	}

	opt.filterMode = filter.ModeNoFilter
	if err := run(strings.NewReader(stdin), stdout, opt); err != nil {
		t.Error(err)
	}

	if got := strings.Trim(stdout.String(), "\n"); got != want {
		t.Errorf("got:\n%v\n want\n%v", got, want)
	}
}

func TestRun_local_tee(t *testing.T) {
	stdin := "tee test"
	opt := &option{
		diffCmd:   "git diff",
		efms:      strslice([]string{`%f(%l,%c): %m`}),
		diffStrip: 0,
		reporter:  "local",
		tee:       true,
	}

	stdout := new(bytes.Buffer)
	if err := run(strings.NewReader(stdin), stdout, opt); err != nil {
		t.Error(err)
	}

	if got := strings.Trim(stdout.String(), "\n"); got != stdin {
		t.Errorf("raw: got %v, want %v", got, stdin)
	}
}

func TestRun_project(t *testing.T) {
	t.Run("diff command is empty", func(t *testing.T) {
		opt := &option{
			conf:     "reviewdog.yml",
			reporter: "local",
		}
		stdout := new(bytes.Buffer)
		if err := run(nil, stdout, opt); err == nil {
			t.Error("want err, got nil")
		} else {
			t.Log(err)
		}
	})

	t.Run("config not found", func(t *testing.T) {
		opt := &option{
			conf:     "reviewdog.notfound.yml",
			diffCmd:  "echo ''",
			reporter: "local",
		}
		stdout := new(bytes.Buffer)
		if err := run(nil, stdout, opt); err == nil {
			t.Error("want err, got nil")
		} else {
			t.Log(err)
		}
	})

	t.Run("invalid config", func(t *testing.T) {
		conffile, err := os.CreateTemp("", "reviewdog-test")
		if err != nil {
			t.Fatal(err)
		}
		defer conffile.Close()
		defer os.Remove(conffile.Name())
		conffile.WriteString("invalid yaml")
		opt := &option{
			conf:     conffile.Name(),
			diffCmd:  "echo ''",
			reporter: "local",
		}
		stdout := new(bytes.Buffer)
		if err := run(nil, stdout, opt); err == nil {
			t.Error("want err, got nil")
		} else {
			t.Log(err)
		}
	})

	t.Run("ok", func(t *testing.T) {
		conffile, err := os.CreateTemp("", "reviewdog-test")
		if err != nil {
			t.Fatal(err)
		}
		defer conffile.Close()
		defer os.Remove(conffile.Name())
		conffile.WriteString("") // empty
		opt := &option{
			conf:     conffile.Name(),
			diffCmd:  "echo ''",
			reporter: "local",
		}
		stdout := new(bytes.Buffer)
		if err := run(nil, stdout, opt); err != nil {
			t.Errorf("got unexpected err: %v", err)
		}
	})

	t.Run("conffile allows to be prefixed with '.' and '.yaml' file extension", func(t *testing.T) {
		for _, n := range []string{".reviewdog.yml", "reviewdog.yaml"} {
			f, err := os.OpenFile(n, os.O_RDONLY|os.O_CREATE, 0666)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			defer os.Remove(n)
			if _, err := readConf(n); err != nil {
				t.Errorf("readConf(%q) got unexpected err: %v", n, err)
			}
		}
	})
}

func TestRun_version(t *testing.T) {
	stdout := new(bytes.Buffer)
	if err := run(nil, stdout, &option{version: true}); err != nil {
		t.Error(err)
	}
	if got := strings.TrimRight(stdout.String(), "\n"); got != commands.Version {
		t.Errorf("version = %v, want %v", got, commands.Version)
	}
}
