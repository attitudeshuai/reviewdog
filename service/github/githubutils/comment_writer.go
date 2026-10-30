package githubutils

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/haya14busa/go-actions-toolkit/core"

	"github.com/reviewdog/reviewdog"
	"github.com/reviewdog/reviewdog/proto/rdf"
)

const MaxLoggingAnnotationsPerStep = 10

var _ reviewdog.CommentService = &GitHubActionLogWriter{}

// queuedAnnotation is a buffered logging-command annotation with its resolved
// annotation category.
type queuedAnnotation struct {
	toolName     string
	defaultLevel string
	d            *rdf.Diagnostic
	// errorLevel is true when the annotation is emitted with the "error"
	// logging command and false when it is emitted with the "warning" one.
	errorLevel bool
}

func newQueuedAnnotation(toolName, defaultLevel string, d *rdf.Diagnostic) queuedAnnotation {
	return queuedAnnotation{
		toolName:     toolName,
		defaultLevel: defaultLevel,
		d:            d,
		errorLevel:   isErrorAnnotationLevel(resolveAnnotationLevel(defaultLevel, d.GetSeverity())),
	}
}

// AnnotationLimit enforces the GitHub Actions per-step limit for annotations
// created by logging commands.
//
// One AnnotationLimit is shared by every GitHubActionLogWriter of a single
// reviewdog run, so the retained annotations, the total number of dropped
// annotations and the over-limit warning are computed once over the whole
// run (e.g. the direct github-annotations reporter and the writers created
// while github-check/github-pr-review reporters fall back to logging).
//
// Create a new AnnotationLimit for each run. It must not be shared across
// different runs. It is safe for concurrent use.
type AnnotationLimit struct {
	mu sync.Mutex

	out   io.Writer
	limit int

	entries []queuedAnnotation
	warned  bool
	flushed bool
}

// NewAnnotationLimit returns a new AnnotationLimit with the default per-step
// limit which writes logging commands to os.Stdout.
func NewAnnotationLimit() *AnnotationLimit {
	return &AnnotationLimit{
		out:   os.Stdout,
		limit: MaxLoggingAnnotationsPerStep,
	}
}

// SetOutput sets the writer logging commands are written to.
func (l *AnnotationLimit) SetOutput(out io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.out = out
}

func (l *AnnotationLimit) add(toolName, defaultLevel string, d *rdf.Diagnostic) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, newQueuedAnnotation(toolName, defaultLevel, d))
}

// Flush reports the retained annotations once. Annotations over the per-step
// limit are silently dropped (error category first, then warning category,
// input order within a category) and, if the limit was reached, a single
// warning containing the dropped count is emitted.
//
// The annotation limit itself is never reported as an error, but write errors
// from the underlying writer are. Flush is idempotent and safe to call more
// than once.
func (l *AnnotationLimit) Flush(_ context.Context) error {
	l.mu.Lock()
	if l.flushed {
		l.mu.Unlock()
		return nil
	}
	l.flushed = true
	entries := l.entries
	out := l.out
	warned := l.warned
	l.mu.Unlock()

	newWarned, err := emitAnnotations(out, entries, l.limit, warned)

	l.mu.Lock()
	l.warned = newWarned
	l.mu.Unlock()
	return err
}

// GitHubActionLogWriter reports results via logging command to create
// annotations.
// https://help.github.com/en/actions/automating-your-workflow-with-github-actions/development-tools-for-github-actions#example-5
type GitHubActionLogWriter struct {
	level string

	// limit, when set, is the shared per-run limit. Buffered annotations are
	// reported by AnnotationLimit.Flush instead of Flush so that the limit is
	// enforced once over the whole run across all writer instances.
	limit *AnnotationLimit

	// Following fields are used only in standalone mode (limit == nil).
	mu      sync.Mutex
	out     io.Writer
	entries []queuedAnnotation
	warned  bool
}

// Option configures a GitHubActionLogWriter.
type Option func(*GitHubActionLogWriter)

// WithAnnotationLimit attaches the writer to the shared per-run annotation
// limit. Multiple writers attached to the same limit share the dropped count
// and emit a single over-limit warning per run.
func WithAnnotationLimit(l *AnnotationLimit) Option {
	return func(w *GitHubActionLogWriter) {
		w.limit = l
	}
}

// WithOutput sets the writer logging commands are written to in standalone
// mode. It has no effect when the writer is attached to an AnnotationLimit
// (use AnnotationLimit.SetOutput instead).
func WithOutput(out io.Writer) Option {
	return func(w *GitHubActionLogWriter) {
		w.out = out
	}
}

// NewGitHubActionLogWriter returns new GitHubActionLogWriter.
func NewGitHubActionLogWriter(level string, opts ...Option) *GitHubActionLogWriter {
	lw := &GitHubActionLogWriter{
		level: level,
		out:   os.Stdout,
	}
	for _, opt := range opts {
		opt(lw)
	}
	return lw
}

func (lw *GitHubActionLogWriter) Post(_ context.Context, c *reviewdog.Comment) error {
	if lw.limit != nil {
		lw.limit.add(c.ToolName, lw.level, c.Result.Diagnostic)
		return nil
	}
	lw.mu.Lock()
	defer lw.mu.Unlock()
	lw.entries = append(lw.entries, newQueuedAnnotation(c.ToolName, lw.level, c.Result.Diagnostic))
	return nil
}

func (*GitHubActionLogWriter) ShouldPrependGitRelDir() bool { return true }

// Flush reports buffered annotations. When the writer is attached to a shared
// AnnotationLimit, it does nothing; AnnotationLimit.Flush reports every
// annotation of the run at once.
//
// The annotation limit is never reported as an error, but write errors from
// the underlying writer are returned.
func (lw *GitHubActionLogWriter) Flush(_ context.Context) error {
	if lw.limit != nil {
		return nil
	}
	lw.mu.Lock()
	entries := lw.entries
	lw.entries = nil
	out := lw.out
	warned := lw.warned
	lw.mu.Unlock()

	newWarned, err := emitAnnotations(out, entries, MaxLoggingAnnotationsPerStep, warned)

	lw.mu.Lock()
	lw.warned = newWarned
	lw.mu.Unlock()
	return err
}

// emitAnnotations writes up to limit annotations (error category first,
// warning category next, input order within each category) and, when the
// number of annotations reaches the limit, emits a single warning containing
// the number of dropped annotations and the limit. It returns whether the
// over-limit warning was emitted.
func emitAnnotations(out io.Writer, entries []queuedAnnotation, limit int, warned bool) (bool, error) {
	retained := selectAnnotations(entries, limit)
	for _, e := range retained {
		if err := writeAnnotation(out, e); err != nil {
			return warned, fmt.Errorf("GitHubActionLogWriter: failed to write annotation %s: %w", e.identifier(), err)
		}
	}
	dropped := len(entries) - len(retained)
	if len(entries) >= limit && !warned {
		if err := writeTooManyAnnotations(out, dropped, limit); err != nil {
			return warned, fmt.Errorf("GitHubActionLogWriter: failed to write over-limit warning: %w", err)
		}
		return true, nil
	}
	return warned, nil
}

// selectAnnotations keeps up to limit entries: error category first, warning
// category next, input order within each category. The result is deterministic
// so the retained set is stable across runs for the same input.
func selectAnnotations(entries []queuedAnnotation, limit int) []queuedAnnotation {
	if len(entries) <= limit {
		// When the limit is not exceeded, the input order is preserved exactly.
		return entries
	}
	retained := make([]queuedAnnotation, 0, limit)
	for _, e := range entries {
		if e.errorLevel {
			retained = append(retained, e)
			if len(retained) == limit {
				return retained
			}
		}
	}
	for _, e := range entries {
		if !e.errorLevel {
			retained = append(retained, e)
			if len(retained) == limit {
				break
			}
		}
	}
	return retained
}

// identifier identifies an annotation in error messages.
func (e queuedAnnotation) identifier() string {
	s := fmt.Sprintf("[%s] %s", e.toolName, e.d.GetLocation().GetPath())
	start := e.d.GetLocation().GetRange().GetStart()
	if line := start.GetLine(); line > 0 {
		s += fmt.Sprintf(":%d", line)
		if col := start.GetColumn(); col > 0 {
			s += fmt.Sprintf(":%d", col)
		}
	}
	return s
}

// writeAnnotation writes a single logging command annotation. The output
// format is identical to the previous ReportAsGitHubActionsLog behavior.
func writeAnnotation(out io.Writer, e queuedAnnotation) error {
	mes := fmt.Sprintf("[%s] reported by reviewdog 🐶\n%s\n\nRaw Output:\n%s",
		e.toolName, e.d.GetMessage(), e.d.GetOriginalOutput())
	loc := e.d.GetLocation()
	start := loc.GetRange().GetStart()
	props := core.NewCommandProperties()
	if file := loc.GetPath(); file != "" {
		props.AddFile(file)
	}
	if line := int(start.GetLine()); line != 0 {
		props.AddLine(line)
	}
	if col := int(start.GetColumn()); col != 0 {
		props.AddCol(col)
	}

	level := resolveAnnotationLevel(e.defaultLevel, e.d.GetSeverity())
	commands := make([]string, 0, 2)
	switch level {
	// no info command with location data.
	case "warning", "info":
		commands = append(commands, core.NewCommand("warning", props, mes).String())
	case "error", "":
		commands = append(commands, core.NewCommand("error", props, mes).String())
	default:
		commands = append(commands,
			core.NewCommand("error", core.NewCommandProperties(), fmt.Sprintf("Unknown level: %s", level)).String(),
			core.NewCommand("error", props, mes).String())
	}
	for _, command := range commands {
		if _, err := fmt.Fprintln(out, command); err != nil {
			return err
		}
	}
	return nil
}

// resolveAnnotationLevel resolves the logging command level from the check
// result severity, falling back to the reporter default level. This keeps the
// existing severity mapping, so in fallback paths the check item level still
// determines the annotation category when severity is unspecified.
func resolveAnnotationLevel(defaultLevel string, s rdf.Severity) string {
	switch s {
	case rdf.Severity_ERROR:
		return "error"
	case rdf.Severity_INFO, rdf.Severity_WARNING:
		return "warning"
	}
	return defaultLevel
}

func isErrorAnnotationLevel(level string) bool {
	// "warning"/"info" use the warning command; "error", empty and unknown
	// levels use the error command.
	return level != "warning" && level != "info"
}

func writeTooManyAnnotations(out io.Writer, dropped, limit int) error {
	_, err := fmt.Fprintln(out, core.NewCommand("error", nil, tooManyAnnotationsMessage(dropped, limit)).String())
	return err
}

func tooManyAnnotationsMessage(dropped, limit int) string {
	return fmt.Sprintf(`reviewdog: Too many results (annotations) in diff.
reviewdog dropped %d annotation(s) because the annotation count reached the per-step limit of %d.
You may miss some annotations due to GitHub limitation for annotation created by logging command.
Please check GitHub Actions log console to see all results.

Limitation:
- 10 warning annotations and 10 error annotations per step
- 50 annotations per job (sum of annotations from all the steps)
- 50 annotations per run (separate from the job annotations, these annotations aren't created by users)

Source: https://github.com/orgs/community/discussions/26680#discussioncomment-3252835`, dropped, limit)
}

// ReportAsGitHubActionsLog reports results via logging command to create
// annotations.
// https://help.github.com/en/actions/automating-your-workflow-with-github-actions/development-tools-for-github-actions#example-5
//
// Deprecated: use GitHubActionLogWriter, which enforces the per-step
// annotation limit and returns write errors.
func ReportAsGitHubActionsLog(toolName, defaultLevel string, d *rdf.Diagnostic) {
	_ = writeAnnotation(os.Stdout, newQueuedAnnotation(toolName, defaultLevel, d))
}

// WarnTooManyAnnotationOnce emits the over-limit warning at most once for
// backwards compatibility. The per-run GitHubActionLogWriter/AnnotationLimit
// flow emits the warning automatically with the total dropped count.
//
// Deprecated: use AnnotationLimit (or GitHubActionLogWriter) instead.
func WarnTooManyAnnotationOnce() {
	warnTooManyAnnotationOnce.Do(func() {
		_ = writeTooManyAnnotations(os.Stdout, 0, MaxLoggingAnnotationsPerStep)
	})
}

var warnTooManyAnnotationOnce sync.Once
