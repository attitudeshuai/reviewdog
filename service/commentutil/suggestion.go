package commentutil

import (
	"errors"
	"fmt"

	"github.com/reviewdog/reviewdog/filter"
	"github.com/reviewdog/reviewdog/proto/rdf"
)

// BuildSuggestionText applies a suggestion's replacement text to the source
// lines it covers.
//
// Only the prefix of the start line and the suffix of the end line are kept;
// every line in between is replaced wholesale by the suggestion text.
// Columns are 1-based UTF-8 byte offsets: a column past a line end is
// clamped to the line end and a column in the middle of a multi-byte rune
// expands outward to a rune boundary, so the resulting text is always valid
// UTF-8. A missing start column means the start of the start line; a missing
// end position defaults to the start point; an end line without a column
// means the end of that line (no end-line content is retained).
func BuildSuggestionText(sourceLines map[int]string, s *rdf.Suggestion) (string, error) {
	if len(sourceLines) == 0 {
		return "", errors.New("source lines are not available")
	}

	startPos := s.GetRange().GetStart()
	startLine := int(startPos.GetLine())
	startLineContent, err := getSourceLine(sourceLines, startLine)
	if err != nil {
		return "", err
	}

	endLine := startLine
	endLineExplicit := false
	if endPos := s.GetRange().GetEnd(); endPos != nil && endPos.GetLine() != 0 {
		endLine = int(endPos.GetLine())
		endLineExplicit = true
	}
	endLineContent, err := getSourceLine(sourceLines, endLine)
	if err != nil {
		return "", err
	}
	if endLine < startLine {
		return "", fmt.Errorf("suggestion range end line (L=%d) precedes its start line (L=%d)", endLine, startLine)
	}

	startOff := filter.StartByteOffset(startLineContent, int(startPos.GetColumn()))

	var endOff int
	if endLineExplicit {
		// An explicit end line without a column reaches the end of that line.
		endOff = filter.EndByteOffset(endLineContent, int(s.GetRange().GetEnd().GetColumn()))
	} else {
		// The end position is omitted or stays on the start line. It defaults
		// to the start point unless its column is given explicitly.
		endColumn := int(startPos.GetColumn())
		if endPos := s.GetRange().GetEnd(); endPos != nil && endPos.GetColumn() != 0 {
			endColumn = int(endPos.GetColumn())
		}
		endOff = filter.EndByteOffset(endLineContent, endColumn)
	}

	if endLine == startLine && endOff < startOff {
		return "", fmt.Errorf("suggestion range end column precedes its start column on line %d", startLine)
	}

	return startLineContent[:startOff] + s.GetText() + endLineContent[endOff:], nil
}

func getSourceLine(sourceLines map[int]string, line int) (string, error) {
	lineContent, ok := sourceLines[line]
	if !ok {
		return "", fmt.Errorf("source line (L=%d) is not available for this suggestion", line)
	}
	return lineContent, nil
}
