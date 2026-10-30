// Package project provides utility for reviewdog execution based on project
// config.
package project

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/reviewdog/reviewdog/proto/rdf"
)

// Config represents reviewdog config.
type Config struct {
	Runner map[string]*Runner
}

// Runner represents config for a runner.
type Runner struct {
	// Runner command. (e.g. `golint ./...`)
	Cmd string
	// tool name in review comment. (e.g. `golint`)
	Name string
	// errorformat name. (e.g. `checkstyle`)
	Format string
	// errorformat. (e.g. `%f:%l:%c:%m`, `%-G%.%#`)
	Errorformat []string
	// Report Level for this runner. ("info", "warning", "error")
	Level string
}

// Parse parses reviewdog config in yaml format.
func Parse(yml []byte) (*Config, error) {
	out := &Config{}
	if err := yaml.Unmarshal(yml, out); err != nil {
		return nil, err
	}
	// Insert `Name` field if it's empty.
	for name, runner := range out.Runner {
		if runner == nil {
			return nil, fmt.Errorf("runner %q must be a mapping, not null", name)
		}
		if runner.Name == "" {
			runner.Name = name
		}
		// Validate the report level while loading the config so invalid
		// values fail at startup instead of being silently downgraded (or
		// rendered as an "unknown level") by reporters. The runner name is
		// reported so users can tell which checker is wrong.
		if l := runner.Level; l != "" && rdf.ParseSeverity(l) == rdf.Severity_UNKNOWN_SEVERITY {
			return nil, fmt.Errorf("invalid level %q for runner %q in reviewdog config: "+
				"supported levels are %q, %q and %q (case-insensitive, common synonyms such as "+
				"fatal/critical/blocker, warn, note/hint are also accepted)",
				l, runner.Name, "info", "warning", "error")
		}
	}
	return out, nil
}
