package commentutil

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/reviewdog/reviewdog/proto/rdf"
)

// BuildSuggestionText applies a suggestion's replacement text to the source lines it covers.
func BuildSuggestionText(sourceLines map[int]string, s *rdf.Suggestion) (string, error) {
	if len(sourceLines) == 0 {
		return "", errors.New("source lines are not available")
	}
	start := s.GetRange().GetStart()
	end := s.GetRange().GetEnd()
	if end == nil {
		end = start
	} else if end.GetLine() == 0 {
		end = &rdf.Position{Line: start.GetLine(), Column: end.GetColumn()}
	}
	startLineContent, err := getSourceLine(sourceLines, int(start.GetLine()))
	if err != nil {
		return "", err
	}
	endLineContent, err := getSourceLine(sourceLines, int(end.GetLine()))
	if err != nil {
		return "", err
	}

	return startLineContent[:max(start.GetColumn()-1, 0)] + s.GetText() + endLineContent[max(end.GetColumn()-1, 0):], nil
}

func getSourceLine(sourceLines map[int]string, line int) (string, error) {
	lineContent, ok := sourceLines[line]
	if !ok {
		return "", fmt.Errorf("source line (L=%d) is not available for this suggestion", line)
	}
	return lineContent, nil
}

// suggestionLineRange returns the 1-based line interval a suggestion covers.
// A missing range end defaults to the start line, mirroring
// BuildSuggestionText's end normalization.
func suggestionLineRange(s *rdf.Suggestion) (startLine, endLine int) {
	start := s.GetRange().GetStart()
	end := s.GetRange().GetEnd()
	startLine = int(start.GetLine())
	endLine = int(end.GetLine())
	if endLine == 0 {
		endLine = startLine
	}
	return startLine, endLine
}

// WorkspaceSource supplements source lines that are not available in the diff
// context with lines read from files in the local workspace.
//
// It reads only the line interval covered by a suggestion (never the whole
// file), prefers lines collected from the diff context, and caches
// successfully read files for the duration of one run so the same file is not
// read more than once. Failed reads are not cached and are retried on the
// next suggestion.
//
// A nil *WorkspaceSource performs no workspace supplementation, so callers
// without workspace information keep the historical behavior.
type WorkspaceSource struct {
	rootDir string

	mu        sync.Mutex
	fileLines map[string]map[int]string // normalized rel path -> lines read from workspace (successes only)

	// openFile opens a file for reading. It defaults to os.Open and is
	// overwritten in tests.
	openFile func(path string) (io.ReadCloser, error)
}

// NewWorkspaceSource creates a WorkspaceSource rooted at rootDir. Paths in
// diagnostics must be normalized relative to rootDir.
func NewWorkspaceSource(rootDir string) *WorkspaceSource {
	return &WorkspaceSource{
		rootDir:   rootDir,
		fileLines: make(map[string]map[int]string),
		openFile: func(path string) (io.ReadCloser, error) {
			return os.Open(path)
		},
	}
}

// SuggestionText is BuildSuggestionText with workspace supplementation: when
// sourceLines (collected from the diff context) miss lines covered by s, the
// missing lines are read from relPath in the workspace. Lines present in
// sourceLines always take precedence and no file is read when sourceLines
// already cover the suggestion, so output stays identical when the diff
// context contains all required lines.
func (w *WorkspaceSource) SuggestionText(sourceLines map[int]string, relPath string, s *rdf.Suggestion) (string, error) {
	if w == nil {
		return BuildSuggestionText(sourceLines, s)
	}
	startLine, endLine := suggestionLineRange(s)
	if _, ok := sourceLines[startLine]; ok {
		if _, ok := sourceLines[endLine]; ok {
			// Every line this assembly needs is already known from the diff.
			// Do not touch the workspace at all.
			return BuildSuggestionText(sourceLines, s)
		}
	}
	if startLine < 1 || endLine < startLine || !isSafeRelativePath(relPath) {
		// Unusable line numbers or an ineligible path (empty, absolute, or
		// escaping the workspace). Do not read anything and report the same
		// errors as before.
		return BuildSuggestionText(sourceLines, s)
	}
	merged, err := w.supplement(relPath, sourceLines, startLine, endLine)
	if err != nil {
		return "", err
	}
	return BuildSuggestionText(merged, s)
}

// supplement fills the [startLine, endLine] interval not covered by diffLines
// from the workspace and returns a merged line map (diff lines win).
func (w *WorkspaceSource) supplement(relPath string, diffLines map[int]string, startLine, endLine int) (map[int]string, error) {
	key := normalizeRelPathKey(relPath)
	absPath, err := w.resolve(key)
	if err != nil {
		return nil, err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	cached := w.fileLines[key]
	missing := false
	for l := startLine; l <= endLine; l++ {
		if _, ok := diffLines[l]; ok {
			continue
		}
		if _, ok := cached[l]; !ok {
			missing = true
			break
		}
	}
	if missing {
		// Read only the interval this suggestion covers. The result is cached
		// only when the read succeeds, so failures are retried later.
		readLines, err := readWorkspaceInterval(w.openFile, key, absPath, startLine, endLine)
		if err != nil {
			return nil, err
		}
		if cached == nil {
			cached = make(map[int]string, len(readLines))
		}
		for l, text := range readLines {
			cached[l] = text
		}
		w.fileLines[key] = cached
	}

	merged := make(map[int]string, len(diffLines)+len(cached))
	for l, text := range cached {
		merged[l] = text
	}
	for l, text := range diffLines {
		merged[l] = text // diff context always takes precedence.
	}
	return merged, nil
}

// readWorkspaceInterval opens absPath and collects only lines in
// [startLine, endLine]. Line numbers are 1-based and line terminators are
// stripped, matching unified-diff line contents.
func readWorkspaceInterval(openFile func(string) (io.ReadCloser, error), key, absPath string, startLine, endLine int) (map[int]string, error) {
	f, err := openFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("source file %q is not found in the workspace", key)
		}
		return nil, fmt.Errorf("failed to read workspace source file %q: %v", key, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	lines := make(map[int]string)
	n := 0
	for n < endLine && scanner.Scan() {
		n++
		if n >= startLine {
			lines[n] = scanner.Text()
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read workspace source file %q: %v", key, err)
	}
	if n < endLine {
		return nil, fmt.Errorf("source line (L=%d) is out of range in workspace file %q", endLine, key)
	}
	return lines, nil
}

// isSafeRelativePath reports whether relPath is a relative path that does not
// lexically escape its base directory. Absolute paths and ".." components are
// rejected. Symlink escapes are checked separately by resolve.
func isSafeRelativePath(relPath string) bool {
	if relPath == "" {
		return false
	}
	p := filepath.FromSlash(relPath)
	if filepath.IsAbs(p) {
		return false
	}
	cleaned := filepath.Clean(p)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

func normalizeRelPathKey(relPath string) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(relPath)))
}

// resolve maps a normalized relative path to an absolute path inside the
// workspace root. A nonexistent path is reported as a missing file. Existing
// symlinks are followed, but any path component that resolves outside the
// workspace is rejected without being opened.
func (w *WorkspaceSource) resolve(key string) (string, error) {
	root, err := filepath.EvalSymlinks(w.rootDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace root for source file %q: %v", key, err)
	}
	cur := root
	for _, comp := range strings.Split(filepath.ToSlash(key), "/") {
		if comp == "" || comp == "." {
			continue
		}
		next := filepath.Join(cur, comp)
		info, err := os.Lstat(next)
		if err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("source file %q is not found in the workspace", key)
			}
			return "", fmt.Errorf("failed to access workspace source file %q: %v", key, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			real, err := filepath.EvalSymlinks(next)
			if err != nil {
				if os.IsNotExist(err) {
					return "", fmt.Errorf("source file %q is not found in the workspace", key)
				}
				return "", fmt.Errorf("failed to resolve workspace source file %q: %v", key, err)
			}
			cur = real
		} else {
			cur = next
		}
		if !isWithinDir(root, cur) {
			return "", fmt.Errorf("source file %q resolves outside the workspace and is not read", key)
		}
	}
	return cur, nil
}

func isWithinDir(dir, target string) bool {
	rel, err := filepath.Rel(dir, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
