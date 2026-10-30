package project

import (
	"strings"
	"testing"

	"github.com/kylelemons/godebug/pretty"
)

func TestParseNullRunner(t *testing.T) {
	for _, value := range []string{"", "null", "~"} {
		t.Run(value, func(t *testing.T) {
			_, err := Parse([]byte("runner:\n  lint: " + value + "\n"))
			if err == nil || !strings.Contains(err.Error(), `runner "lint"`) {
				t.Fatalf("Parse() error = %v, want named runner error", err)
			}
		})
	}
}

func TestParse(t *testing.T) {
	const yml = `
# reviewdog.yml

runner:
  golint:
    cmd: golint ./...
    level: info
    errorformat:
      - "%f:%l:%c: %m"
  govet:
    cmd: go tool vet -all -shadowstrict .
    format: govet
    level: warning
  namekey:
    cmd: echo 'name'
    name: nameoverwritten
    format: checkstyle
    level: error
`

	want := &Config{
		Runner: map[string]*Runner{
			"golint": {
				Cmd:         "golint ./...",
				Errorformat: []string{`%f:%l:%c: %m`},
				Name:        "golint",
				Level:       "info",
			},
			"govet": {
				Cmd:    "go tool vet -all -shadowstrict .",
				Format: "govet",
				Name:   "govet",
				Level:  "warning",
			},
			"namekey": {
				Cmd:    "echo 'name'",
				Format: "checkstyle",
				Name:   "nameoverwritten",
				Level:  "error",
			},
		},
	}

	got, err := Parse([]byte(yml))
	if err != nil {
		t.Fatal(err)
	}
	if diff := pretty.Compare(got, want); diff != "" {
		t.Errorf("Parse() diff: (-got +want)\n%s", diff)
	}

}

func TestParseLevel(t *testing.T) {
	t.Run("synonyms and surrounding whitespace are accepted", func(t *testing.T) {
		const yml = `
runner:
  fatalcheck:
    cmd: echo fatal
    level: Fatal
  spacedcheck:
    cmd: echo spaced
    level: " warning "
  hintcheck:
    cmd: echo hint
    level: 提示
  nolevel:
    cmd: echo none
`
		conf, err := Parse([]byte(yml))
		if err != nil {
			t.Fatalf("Parse() unexpected error: %v", err)
		}
		if got := conf.Runner["fatalcheck"].Level; got != "Fatal" {
			t.Errorf("raw level preserved as %q, want %q", got, "Fatal")
		}
		if got := conf.Runner["spacedcheck"].Level; got != " warning " {
			t.Errorf("raw level preserved as %q, want %q", got, " warning ")
		}
	})

	t.Run("invalid level errors and names the runner", func(t *testing.T) {
		const yml = `
runner:
  badcheck:
    cmd: echo bad
    level: urgencies-are-hard
`
		_, err := Parse([]byte(yml))
		if err == nil {
			t.Fatal("Parse() error = nil, want invalid level error")
		}
		if !strings.Contains(err.Error(), `urgencies-are-hard`) {
			t.Errorf("error %q does not mention the invalid level value", err)
		}
		if !strings.Contains(err.Error(), `badcheck`) {
			t.Errorf("error %q does not identify the runner (checker) source", err)
		}
	})

	t.Run("empty level is accepted", func(t *testing.T) {
		const yml = `
runner:
  ok:
    cmd: echo ok
    level: ""
`
		if _, err := Parse([]byte(yml)); err != nil {
			t.Fatalf("Parse() unexpected error: %v", err)
		}
	})
}
